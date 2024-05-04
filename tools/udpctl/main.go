package main

import (
	"flag"
	"fmt"
	"net"
	"os"
)

type Options struct {
	Listen string
	Server string
	Msg    string
}

func main() {
	opt := Options{}
	flag.StringVar(&opt.Listen, "l", "", "udp listen address")
	flag.StringVar(&opt.Server, "s", ":9900", "udp server address")
	flag.StringVar(&opt.Msg, "m", ":9900", "send udp msg")
	flag.Parse()

	if opt.Listen != "" {
		// 创建一个 UDP 地址
		addr, err := net.ResolveUDPAddr("udp", opt.Listen)
		if err != nil {
			fmt.Println("Error resolving UDP address:", err)
			os.Exit(1)
		}
		conn, err := net.ListenUDP("udp", addr)
		if err != nil {
			fmt.Println("Error dialing UDP:", err)
			os.Exit(1)
		}
		defer conn.Close()

		for {
			// 接收数据
			buffer := make([]byte, 1024)
			n, _, err := conn.ReadFromUDP(buffer)
			if err != nil {
				fmt.Println("Error receiving data:", err)
				os.Exit(1)
			}

			fmt.Println("Received data:", string(buffer[:n]))

		}
	} else {

		addr, err := net.ResolveUDPAddr("udp", opt.Server)
		if err != nil {
			fmt.Println("Error resolving UDP address:", err)
			os.Exit(1)
		}
		// 创建一个 UDP 连接
		conn, err := net.DialUDP("udp", nil, addr)
		if err != nil {
			fmt.Println("Error dialing UDP:", err)
			os.Exit(1)
		}
		defer conn.Close()

		// 发送数据
		_, err = conn.Write([]byte(opt.Msg))
		if err != nil {
			fmt.Println("Error sending data:", err)
			os.Exit(1)
		}
	}

}
