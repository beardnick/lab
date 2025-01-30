package main

import (
	"github.com/bwmarrin/snowflake"
	"net"
	"strconv"
	"strings"
)

func NewId() (id string, err error) {
	ip, err := clientIp()
	if err != nil {
		return
	}
	if ip == "" {
		return newId(1)
	}
	ips := strings.Split(ip, ".")
	nodeid, err := strconv.Atoi(ips[len(ips)-1])
	if err != nil {
		return
	}
	return newId(nodeid)
}

func newId(nodeid int) (id string, err error) {
	n, err := snowflake.NewNode(int64(nodeid))
	if err != nil {
		return
	}
	id = n.Generate().String()
	return
}

func clientIp() (ip string, err error) {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return
	}
	for _, a := range addrs {
		ipnet, ok := a.(*net.IPNet)
		if !ok || ipnet.IP.IsLoopback() {
			continue
		}
		if ipnet.IP.To4() != nil {
			ip = ipnet.IP.String()
			return
		}
	}
	return
}
