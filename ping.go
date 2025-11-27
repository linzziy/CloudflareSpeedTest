package main

import (
	"bufio"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

func main() {
	host := "baidu.com" // 可以替换为你想测试的主机
	count := 1          // ping 次数

	cmd := exec.Command("ping", "-c", strconv.Itoa(count), host)
	output, err := cmd.Output()
	if err != nil {
		fmt.Printf("Error executing ping: %v\n", err)
		return
	}

	// 解析 ping 输出，提取每个包的 RTT 时间（单位：ms）
	rttRegex := regexp.MustCompile(`time=([\d\.]+) ms`)
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	var rtts []float64

	for scanner.Scan() {
		line := scanner.Text()
		match := rttRegex.FindStringSubmatch(line)
		if len(match) == 2 {
			rtt, err := strconv.ParseFloat(match[1], 64)
			if err == nil {
				rtts = append(rtts, rtt)
			}
		}
	}

	if len(rtts) == 0 {
		fmt.Println("No RTT data found.")
		return
	}

	// 计算平均延迟
	var sum float64
	for _, rtt := range rtts {
		sum += rtt
	}
	average := sum / float64(len(rtts))

	fmt.Printf("Ping results for %s:\n", host)
	for i, rtt := range rtts {
		fmt.Printf("Ping %d: %.2f ms\n", i+1, rtt)
	}
	fmt.Printf("Average latency: %.2f ms\n", average)
}
