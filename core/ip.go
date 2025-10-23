package core

import (
	"fmt"
	"net"
)

type IpAddress struct {
	Ip     *net.IPAddr
	Port   int
	IpPort string
}

func (ip *IpAddress) String() string {
	return fmt.Sprintf("%s:%d", ip.Ip.IP, ip.Port)
}
