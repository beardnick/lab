package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"netstack/socket"
	"netstack/tuntap"
	"os"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

func main() {
	logFile, err := os.OpenFile("netstack.log", os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0666)
	if err != nil {
		panic(err)
	}
	defer logFile.Close()
	log.SetOutput(logFile)

	tun, err := tuntap.NewTun("testtun", "10.0.0.1/24")
	if err != nil {
		panic(err)
	}
	socket.SetupDefaultNetwork(context.Background(), tun, socket.NetworkOptions{Debug: true})
	serverFd, err := socket.TcpSocket()
	if err != nil {
		panic(err)
	}
	err = socket.Bind(serverFd, "10.0.0.2:8080")
	if err != nil {
		panic(err)
	}
	err = socket.Listen(serverFd, 10)
	if err != nil {
		panic(err)
	}
	fmt.Printf("server start success %d\n", serverFd)
	for {
		fmt.Println("accepting...")
		connFd, err := socket.Accept(serverFd)
		if err != nil {
			log.Println(err)
			continue
		}
		handleConn(connFd)
	}
}

func handleConn(connFd int) {
	fmt.Printf("connected with:%d\n", connFd)
	wg := sync.WaitGroup{}
	wg.Add(2)
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		defer wg.Done()
		for {
			data, err := socket.Read(connFd)
			if err != nil {
				if err == io.EOF {
					fmt.Printf("connection %d closed\n", connFd)
					cancel()
					return
				}
				log.Println(err)
				continue
			}
			fmt.Println("recv:", string(data))
		}
	}()

	go func() {
		defer wg.Done()
		for {
			select {
			case <-ctx.Done():
				return
			default:
			}
			input, err := selectStdin(time.Millisecond * 100)
			if err != nil {
				log.Println(err)
				continue
			}
			if len(input) == 0 {
				continue
			}
			err = socket.Send(connFd, []byte(input))
			if err != nil {
				log.Println(err)
				continue
			}
			if input == "exit" {
				socket.Close(connFd)
				break
			}
		}
	}()
	wg.Wait()
}

func selectStdin(timeout time.Duration) (string, error) {
	fds := []unix.PollFd{
		{
			Fd:     int32(os.Stdin.Fd()),
			Events: unix.POLLIN,
		},
	}

	n, err := unix.Poll(fds, int(timeout.Milliseconds()))
	if err != nil {
		return "", fmt.Errorf("poll error: %v", err)
	}

	if n == 0 {
		return "", nil // timeout
	}

	if fds[0].Revents&unix.POLLIN != 0 {
		var input [1024]byte
		n, err := unix.Read(int(os.Stdin.Fd()), input[:])
		if err != nil {
			return "", fmt.Errorf("read error: %v", err)
		}
		return string(input[:n-1]), nil // trim newline
	}

	return "", nil
}
