package main

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
)

func runClient() {
	conn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", *host, *port))
	if err != nil {
		fmt.Println("connect to server failed", err)
		return
	}
	defer conn.Close()
	prompt := "mydb> "
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print(prompt)
		//fmt.Println("reading...")
		text, err := reader.ReadString('\n')
		if err != nil {
			if err == io.EOF {
				return
			}
			fmt.Println("read failed", err)
			continue
		}
		text = text[:len(text)-1]
		//fmt.Println("read", text)
		if strings.TrimSpace(text) == "exit" {
			fmt.Println("bye")
			return
		}
		_, err = conn.Write([]byte(text))
		if err != nil {
			fmt.Println("send cmd failed", err)
			continue
		}
		buf := make([]byte, 1024)
		n, err := conn.Read(buf)
		if err != nil {
			fmt.Println("read resp failed", err)
			continue
		}
		fmt.Println(string(buf[:n]))
	}
}
