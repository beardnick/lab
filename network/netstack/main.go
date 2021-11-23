// +build linux

package main

import (
	"fmt"
	"gotcp/tcp"
	"gotcp/tuntap"
	"log"
)

func main() {
	dev := "mytun"
	//net, err := tuntap.NewTap(dev)
	net, err := tuntap.NewTun(dev)
	if err != nil {
		log.Fatal(err)
	}
	err = tuntap.SetIp(dev, "192.168.1.1/24")
	if err != nil {
		log.Fatal(err)
	}
	err = tuntap.SetUpLink(dev)
	if err != nil {
		log.Fatal(err)
	}
	log.Println("net:", net)
	conn, err := tcp.Accept(net)
	if err != nil {
		return
	}
	fmt.Println("conn:", conn)
	for {
		buf, err := tcp.Rcvd(conn)
		if _, ok := err.(tcp.ConnectionClosedErr); ok {
			break
		}
		if err != nil {
			fmt.Println("read err:", err)
			continue
		}
		fmt.Printf("read:'%s'\n", string(buf))
		if len(buf) == 0 {
			continue
		}
		_, err = tcp.Send(conn, buf)
		if err != nil {
			fmt.Println("send err:", err)
		}
		if string(buf) == "byte" {
			break
		}
	}
	fmt.Println("connection closed")
}
