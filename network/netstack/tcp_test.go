//go:build linux

package main

import (
	"bufio"
	"github.com/stretchr/testify/assert"
	"log"
	"net"
	"network/netstack/tcp"
	"network/netstack/tuntap"
	"sync"
	"testing"
)

func TestTcp(t *testing.T) {
	dev, err := tuntap.NewTun("mytun")
	assert.Nil(t, err)
	err = dev.StartUp("192.168.1.1/24")
	assert.Nil(t, err)

	s := tcp.NewSocket()
	err = s.Bind("192.168.1.2", 8080)
	assert.Nil(t, err)
	s.Listen()

	wg := sync.WaitGroup{}
	wg.Add(2)
	go func() {
		var (
			clientConn net.Conn
			e          error
		)
		for {
			clientConn, e = net.Dial("tcp", "192.168.1.2:8080")
			if e != nil {
				log.Println("dial err", e)
				continue
			}
			break
		}
		_, e = clientConn.Write([]byte("hello world"))
		assert.Nil(t, e)

		// use Read will return an empty string immediately
		r := bufio.NewReader(clientConn)
		assert.Nil(t, e)
		str, e := r.ReadString('\n')
		assert.Nil(t, e)
		assert.Equal(t, "nihao\n", str)

		clientConn.Close()
		//_, e = clientConn.Read(buf)
		//assert.Equal(t, io.EOF, e)
		wg.Done()
	}()
	go func() {
		conn, err := s.Accept()
		assert.Nil(t, err)
		data, err := s.Rcvd(conn)
		assert.Nil(t, err)
		assert.Equal(t, "hello world", string(data))
		s.Send(conn, []byte("nihao\n"))
		wg.Done()
	}()
	wg.Wait()
}
