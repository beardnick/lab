//go:build linux

package main

import (
	"github.com/stretchr/testify/assert"
	"log"
	"net"
	"network/netstack/tcp"
	"network/netstack/tuntap"
	"testing"
	"time"
)

//func TestStartup(t *testing.T) {
//	dev, err := tuntap.NewTun("mytun")
//	assert.Nil(t, err)
//	err = dev.StartUp("192.168.1.1/24")
//	assert.Nil(t, err)
//	for {
//		fmt.Println("read next")
//		p, err := dev.ReadTcpPacket()
//		assert.Nil(t, err)
//		fmt.Println("read packet")
//		if p.IpPack != nil {
//			fmt.Printf("ip %s -> %s\n", p.IpPack.SrcIP, p.IpPack.DstIP)
//		}
//		if p.TcpPack != nil {
//			fmt.Printf("tcp %s -> %s\n%s\n", p.TcpPack.SelfPort, p.TcpPack.PeerPort, string(p.TcpPack.Payload))
//		}
//	}
//}

func TestTcp(t *testing.T) {
	dev, err := tuntap.NewTun("mytun")
	assert.Nil(t, err)
	err = dev.StartUp("192.168.1.1/24")
	assert.Nil(t, err)

	s := tcp.NewSocket()
	err = s.Bind("192.168.1.2", 8080)
	assert.Nil(t, err)
	s.Listen()
	go func() {
		for {
			clientConn, e := net.Dial("tcp", "192.168.1.2:8080")
			if e != nil {
				log.Println("dial err", e)
				continue
			}
			_, e = clientConn.Write([]byte("hello world"))
			if e != nil {
				log.Println("dial write err", e)
				continue
			}
			time.Sleep(time.Second)
			clientConn.Close()
			break
		}
	}()
	conn, err := s.Accept()
	assert.Nil(t, err)
	data, err := s.Rcvd(conn)
	assert.Nil(t, err)
	assert.Equal(t, "hello world", string(data))
}
