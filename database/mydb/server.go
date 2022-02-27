package main

import (
	"fmt"
	"io"
	"net"
	"strings"
)

func runServer() {
	l, err := net.Listen("tcp", fmt.Sprintf("%s:%d", *host, *port))
	if err != nil {
		panic(err)
	}
	defer l.Close()
	fmt.Printf("listen on %s:%d\n", *host, *port)
	for {
		conn, err := l.Accept()
		if err != nil {
			fmt.Println("connect failed", err)
			continue
		}
		go handle(conn)
	}
}

func handle(conn net.Conn) {
	fmt.Println("connect succeed")
	defer conn.Close()
	for {
		buf := make([]byte, 1024) // buf size cannot be zero
		fmt.Println("reading...")
		n, err := conn.Read(buf)
		if err != nil {
			if err == io.EOF {
				fmt.Println("client disconnect")
				return
			}
			fmt.Println("read err", err)
			continue
		}
		cmd, args := parseCmd(string(buf[:n])) // :n is needed
		switch cmd {
		case "get":
			if len(args) < 1 {
				conn.Write([]byte("get key"))
				continue
			}
			if v, ok := storage[args[0]]; !ok {
				conn.Write([]byte("nil"))
			} else {
				conn.Write([]byte(v))
			}
		case "set":
			if len(args) < 2 {
				conn.Write([]byte("set key value"))
				continue
			}
			storage[args[0]] = args[1]
			conn.Write([]byte(args[1]))
		case "exit":
			conn.Write([]byte("bye"))
			return
		default:
			conn.Write([]byte(fmt.Sprintf("unknown cmd %v\n", cmd)))

		}
		fmt.Println("handle cmd succeed")
	}
}

const (
	Cmd = "cmd"
	Arg = "arg"
)

var storage = make(map[string]string, 100)

func parseCmd(text string) (cmd string, args []string) {
	text = strings.TrimSpace(text)
	tmpArgs := strings.SplitN(text, " ", 3)
	fmt.Println(strings.Join(args, "->"))
	if len(tmpArgs) == 0 {
		return
	}
	cmd = tmpArgs[0]
	args = tmpArgs[1:]
	for i := 0; i < len(args); i++ {
		args[i] = strings.TrimSpace(args[i])
	}
	return
}
