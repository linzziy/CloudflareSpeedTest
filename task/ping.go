package task

import (
	"context"
	"fmt"
	"net"
	"os/exec"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/XIU2/CloudflareSpeedTest/core"

	"github.com/XIU2/CloudflareSpeedTest/utils"
)

const (
	tcpConnectTimeout = time.Second * 1
	maxRoutine        = 1000
	defaultRoutines   = 200
	defaultPort       = 443
	defaultPingTimes  = 4
)

var (
	Routines       = defaultRoutines
	TCPPort   int  = defaultPort
	PingTimes int  = defaultPingTimes
	RealPing  bool = true
)

type Ping struct {
	wg      *sync.WaitGroup
	m       *sync.Mutex
	ips     []*core.IpAddress
	csv     utils.PingDelaySet
	control chan bool
	bar     *utils.Bar
}

func checkPingDefault() {
	if Routines <= 0 {
		Routines = defaultRoutines
	}
	//if TCPPort <= 0 || TCPPort >= 65535 {
	//	TCPPort = defaultPort
	//}
	if PingTimes <= 0 {
		PingTimes = defaultPingTimes
	}
}

func NewPing() *Ping {
	checkPingDefault()
	ips := loadIPRanges()
	return &Ping{
		wg:      &sync.WaitGroup{},
		m:       &sync.Mutex{},
		ips:     ips,
		csv:     make(utils.PingDelaySet, 0),
		control: make(chan bool, Routines),
		bar:     utils.NewBar(len(ips), "可用:", ""),
	}
}

func (p *Ping) Run() utils.PingDelaySet {
	if len(p.ips) == 0 {
		return p.csv
	}
	var mode string
	var portStr string
	if Httping {
		mode = "HTTP"
		portStr = "~"
	} else if RealPing {
		mode = "ICMP"
		portStr = ""
	} else {
		mode = "TCP"
		portStr = strconv.Itoa(TCPPort)
	}
	utils.Cyan.Printf("开始延迟测速（模式：%s, 端口：%s, 范围：%v ~ %v ms, 丢包：%.2f)\n", mode, portStr, utils.InputMinDelay.Milliseconds(), utils.InputMaxDelay.Milliseconds(), utils.InputMaxLossRate)
	for _, ip := range p.ips {
		p.wg.Add(1)
		p.control <- false
		go p.start(ip)
	}
	p.wg.Wait()
	p.bar.Done()
	sort.Sort(p.csv)
	for _, csv := range p.csv {
		utils.Green.Println(csv.PingData)
	}
	return p.csv
}

func (p *Ping) start(ip *core.IpAddress) {
	defer p.wg.Done()
	p.tcpingHandler(ip)
	<-p.control
}

// bool connectionSucceed float32 time
func (p *Ping) tcping(ip *core.IpAddress) (bool, time.Duration) {
	startTime := time.Now()
	var fullAddress string
	if isIPv4(ip.Ip.String()) {
		fullAddress = fmt.Sprintf("%s:%d", ip.Ip.IP, ip.Port)
	} else {
		fullAddress = fmt.Sprintf("[%s]:%d", ip.Ip.IP, ip.Port)
	}
	conn, err := net.DialTimeout("tcp", fullAddress, tcpConnectTimeout)
	if err != nil {
		return false, 0
	}
	defer conn.Close()
	duration := time.Since(startTime)
	return true, duration
}

func (p *Ping) icmpping(ip *core.IpAddress) (bool, time.Duration) {
	ipStr := ip.Ip.String()
	//pingCmd := "ping"
	//if !isIPv4(ipStr) {
	//	pingCmd = "ping6"
	//}
	////cmd := exec.Command(pingCmd, "-c", "1", "-W", "1", ipStr)
	////output, err := cmd.Output()
	//cmd := exec.Command(pingCmd, "-c", strconv.Itoa(1), "-W", strconv.Itoa(1), ipStr)
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()

	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		// Windows: -n 1 -w 1000 → 1个包，最多等1000毫秒
		cmd = exec.CommandContext(ctx, "ping", "-n", "1", "-w", "1000", ipStr)
	} else if runtime.GOOS == "darwin" { // macOS
		// macOS: -c 1 -w 1  (最稳)  + -t 1 (双保险)
		cmd = exec.CommandContext(ctx, "ping", "-c", "1", "-t", "1", ipStr)
	} else { // Linux + 其他类 Unix (包括 OpenWrt)
		if isIPv4(ipStr) {
			cmd = exec.CommandContext(ctx, "ping", "-c", "1", "-W", "1", ipStr)
		} else {
			cmd = exec.CommandContext(ctx, "ping6", "-c", "1", "-W", "1", ipStr)
		}
	}
	output, err := cmd.Output()
	if err != nil {
		return false, 0
	}
	rtt := parsePingOutput(string(output))
	if rtt == 0 {
		return false, 0
	}
	return true, time.Duration(rtt * float64(time.Millisecond))
}

func parsePingOutput(output string) float64 {
	rttRegex := regexp.MustCompile(`time=([\d\.]+) ms`)
	if rttRegex.MatchString(output) {
		match := rttRegex.FindStringSubmatch(output)
		if len(match) == 2 {
			rtt, err := strconv.ParseFloat(match[1], 64)
			if err == nil {
				return rtt
			}
		}
	}
	return 0
}

// pingReceived pingTotalTime
func (p *Ping) checkConnection(ip *core.IpAddress) (recv int, totalDelay time.Duration, colo string) {
	if Httping {
		recv, totalDelay, colo = p.httping(ip)
		return
	}
	recv = 0
	totalDelay = 0
	colo = "" // ICMP/TCP 不获取 colo
	if RealPing {
		PingTimes = 1
	}
	for i := 0; i < PingTimes; i++ {
		var ok bool
		var delay time.Duration
		if RealPing {
			ok, delay = p.icmpping(ip)
		} else {
			ok, delay = p.tcping(ip)
		}
		if ok {
			recv++
			totalDelay += delay
		}
	}
	return
}

func (p *Ping) appendIPData(data *utils.PingData) {
	p.m.Lock()
	defer p.m.Unlock()
	p.csv = append(p.csv, utils.CloudflareIPData{
		PingData: data,
	})
}

// handle tcping
func (p *Ping) tcpingHandler(ip *core.IpAddress) {
	recv, totalDlay, colo := p.checkConnection(ip)
	nowAble := len(p.csv)
	if recv != 0 {
		nowAble++
	}
	p.bar.Grow(1, strconv.Itoa(nowAble))
	if recv == 0 {
		return
	}
	data := &utils.PingData{
		IP:       ip,
		Sended:   PingTimes,
		Received: recv,
		Delay:    totalDlay / time.Duration(recv),
		Colo:     colo,
	}
	p.appendIPData(data)
}
