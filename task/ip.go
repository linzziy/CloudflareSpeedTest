package task

import (
	"bufio"
	"encoding/csv"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/XIU2/CloudflareSpeedTest/core"
	"github.com/gookit/goutil/strutil"
)

const defaultInputFile = "ip.txt"

var (
	// TestAll test all ip
	TestAll = false
	// IPFile is the filename of IP Rangs
	IPFile = defaultInputFile
	IPText string
)

func InitRandSeed() {
	rand.Seed(time.Now().UnixNano())
}

// getFieldIndex 从表头获取字段索引
func getFieldIndex(header []string, fieldName string) int {
	for i, h := range header {
		if strings.TrimSpace(h) == fieldName {
			return i
		}
	}
	return -1
}

// getField 从行中获取指定索引的值
func getField(row []string, idx int) string {
	if idx >= 0 && idx < len(row) {
		return strings.TrimSpace(row[idx])
	}
	return ""
}

func isIPv4(ip string) bool {
	return strings.Contains(ip, ".")
}

func randIPEndWith(num byte) byte {
	if num == 0 { // 对于 /32 这种单独的 IP
		return byte(0)
	}
	return byte(rand.Intn(int(num)))
}

type IPRanges struct {
	ips     []*core.IpAddress
	mask    string
	firstIP net.IP
	ipNet   *net.IPNet
}

func newIPRanges() *IPRanges {
	return &IPRanges{
		ips: make([]*core.IpAddress, 0),
	}
}

// 如果是单独 IP 则加上子网掩码，反之则获取子网掩码(r.mask)
func (r *IPRanges) fixIP(ip string) string {
	// 如果不含有 '/' 则代表不是 IP 段，而是一个单独的 IP，因此需要加上 /32 /128 子网掩码
	if i := strings.IndexByte(ip, '/'); i < 0 {
		if isIPv4(ip) {
			r.mask = "/32"
		} else {
			r.mask = "/128"
		}
		ip += r.mask
	} else {
		r.mask = ip[i:]
	}
	return ip
}

// 解析 IP 段，获得 IP、IP 范围、子网掩码
func (r *IPRanges) parseCIDR(ip string) {
	var err error
	if r.firstIP, r.ipNet, err = net.ParseCIDR(r.fixIP(ip)); err != nil {
		log.Fatalln("ParseCIDR err", err)
	}
}

func (r *IPRanges) appendIPv4(d byte, port int) {
	r.appendIP(net.IPv4(r.firstIP[12], r.firstIP[13], r.firstIP[14], d), port)
}

func (r *IPRanges) appendIP(ip net.IP, port int) {
	ipPort := fmt.Sprintf("%s:%d", ip, port)
	for _, e := range r.ips { //如果IP和端口环境，去除重复的
		if e.IpPort == ipPort {
			return
		}
	}
	r.ips = append(r.ips, &core.IpAddress{
		Ip:     &net.IPAddr{IP: ip},
		Port:   port,
		IpPort: ipPort,
	})
}

// 返回第四段 ip 的最小值及可用数目
func (r *IPRanges) getIPRange() (minIP, hosts byte) {
	minIP = r.firstIP[15] & r.ipNet.Mask[3] // IP 第四段最小值

	// 根据子网掩码获取主机数量
	m := net.IPv4Mask(255, 255, 255, 255)
	for i, v := range r.ipNet.Mask {
		m[i] ^= v
	}
	total, _ := strconv.ParseInt(m.String(), 16, 32) // 总可用 IP 数
	if total > 255 {                                 // 矫正 第四段 可用 IP 数
		hosts = 255
		return
	}
	hosts = byte(total)
	return
}

func (r *IPRanges) chooseIPv4(port int) {
	if r.mask == "/32" { // 单个 IP 则无需随机，直接加入自身即可
		r.appendIP(r.firstIP, port)
	} else {
		minIP, hosts := r.getIPRange()    // 返回第四段 IP 的最小值及可用数目
		for r.ipNet.Contains(r.firstIP) { // 只要该 IP 没有超出 IP 网段范围，就继续循环随机
			if TestAll { // 如果是测速全部 IP
				for i := 0; i <= int(hosts); i++ { // 遍历 IP 最后一段最小值到最大值
					r.appendIPv4(byte(i)+minIP, port)
				}
			} else { // 随机 IP 的最后一段 0.0.0.X
				r.appendIPv4(minIP+randIPEndWith(hosts), port)
			}
			r.firstIP[14]++ // 0.0.(X+1).X
			if r.firstIP[14] == 0 {
				r.firstIP[13]++ // 0.(X+1).X.X
				if r.firstIP[13] == 0 {
					r.firstIP[12]++ // (X+1).X.X.X
				}
			}
		}
	}
}

func (r *IPRanges) chooseIPv6(port int) {
	if r.mask == "/128" { // 单个 IP 则无需随机，直接加入自身即可
		r.appendIP(r.firstIP, port)
	} else {
		var tempIP uint8                  // 临时变量，用于记录前一位的值
		for r.ipNet.Contains(r.firstIP) { // 只要该 IP 没有超出 IP 网段范围，就继续循环随机
			r.firstIP[15] = randIPEndWith(255) // 随机 IP 的最后一段
			r.firstIP[14] = randIPEndWith(255) // 随机 IP 的最后一段

			targetIP := make([]byte, len(r.firstIP))
			copy(targetIP, r.firstIP)
			r.appendIP(targetIP, port) // 加入 IP 地址池

			for i := 13; i >= 0; i-- { // 从倒数第三位开始往前随机
				tempIP = r.firstIP[i]              // 保存前一位的值
				r.firstIP[i] += randIPEndWith(255) // 随机 0~255，加到当前位上
				if r.firstIP[i] >= tempIP {        // 如果当前位的值大于等于前一位的值，说明随机成功了，可以退出该循环
					break
				}
			}
		}
	}
}

func loadIPRanges() []*core.IpAddress {
	ranges := newIPRanges()
	if IPText != "" { // 从参数中获取 IP 段数据
		IPs := strings.Split(IPText, ",") // 以逗号分隔为数组并循环遍历
		for _, IP := range IPs {
			IP = strings.TrimSpace(IP) // 去除首尾的空白字符（空格、制表符、换行符等）
			if IP == "" {              // 跳过空的（即开头、结尾或连续多个 ,, 的情况）
				continue
			}
			port := TCPPort
			if strings.Contains(IP, ":") {
				m := strings.Split(IP, ":")
				IP = strings.TrimSpace(m[0])
				port = strutil.IntOr(m[1], TCPPort)
			}
			ranges.parseCIDR(IP) // 解析 IP 段，获得 IP、IP 范围、子网掩码
			if isIPv4(IP) {      // 生成要测速的所有 IPv4 / IPv6 地址（单个/随机/全部）
				ranges.chooseIPv4(port)
			} else {
				ranges.chooseIPv6(port)
			}
		}
	} else { // 从文件中获取 IP 段数据
		if IPFile == "" {
			IPFile = defaultInputFile
		}
		file, err := os.Open(IPFile)
		if err != nil {
			log.Fatal(err)
		}
		defer file.Close()
		if strutil.IsEndOf(IPFile, ".csv") {
			//在CSV中进行操作
			data, err := io.ReadAll(file)
			if err != nil {
				log.Fatal("读取文件失败:", err)
			}
			readerCSV := strings.NewReader(string(data))
			csvReader := csv.NewReader(readerCSV)
			csvReader.Comma = ','          // 默认逗号分隔
			csvReader.FieldsPerRecord = -1 // 允许变长记录

			records, err := csvReader.ReadAll()
			if err != nil {
				log.Fatal(err)
			}

			if len(records) < 2 {
				log.Fatal(err)
			}

			// 第一行是表头，提取关键列索引
			header := records[0]
			ipIdx := getFieldIndex(header, "ip")
			portIdx := getFieldIndex(header, "port")

			for i := 1; i < len(records); i++ {
				row := records[i]
				if len(row) < 2 { // 跳过空行
					continue
				}
				ip := getField(row, ipIdx)
				port := strutil.IntOr(getField(row, portIdx), TCPPort)

				ranges.parseCIDR(ip) // 解析 IP 段，获得 IP、IP 范围、子网掩码
				if isIPv4(ip) {      // 生成要测速的所有 IPv4 / IPv6 地址（单个/随机/全部）
					ranges.chooseIPv4(port)
				} else {
					ranges.chooseIPv6(port)
				}
			}

		} else {
			//在TXT中进行操作
			scanner := bufio.NewScanner(file)
			for scanner.Scan() { // 循环遍历文件每一行
				line := strings.TrimSpace(scanner.Text()) // 去除首尾的空白字符（空格、制表符、换行符等）
				if line == "" {                           // 跳过空行
					continue
				}
				port := TCPPort
				if strings.Contains(line, ":") {
					m := strings.Split(line, ":")
					line = strings.TrimSpace(m[0])
					port = strutil.IntOr(m[1], TCPPort)
				}
				ranges.parseCIDR(line) // 解析 IP 段，获得 IP、IP 范围、子网掩码
				if isIPv4(line) {      // 生成要测速的所有 IPv4 / IPv6 地址（单个/随机/全部）
					ranges.chooseIPv4(port)
				} else {
					ranges.chooseIPv6(port)
				}
			}
		}
	}
	return ranges.ips
}
