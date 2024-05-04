package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
)

type Options struct {
	Addr string
}

func main() {
	opt := Options{}
	flag.StringVar(&opt.Addr, "l", ":9090", "listen address")
	flag.Parse()
	l, err := net.Listen("tcp", opt.Addr)
	if err != nil {
		log.Fatalln(err)
	}
	defer l.Close()
	loop(l)
}

func loop(l net.Listener) {
	for {
		conn, err := l.Accept()
		if err != nil {
			log.Println("accept err", err)
			continue
		}
		go handleConn(conn)
	}
}

func handleConn(conn net.Conn) {
	defer conn.Close()
	fmt.Println("debuginfo:")
	fmt.Println(map[string]any{
		"localAddress":   conn.LocalAddr().String(),
		"remomteAddress": conn.RemoteAddr().String(),
	})

	io.Copy(os.Stdout, conn)
}
