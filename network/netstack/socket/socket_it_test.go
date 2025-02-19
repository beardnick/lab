package socket

import (
	"context"
	"io"
	"net"
	"netstack/tuntap"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func EnsureNetwork() (err error) {
	if NetworkInitialized() {
		return nil
	}
	tun, err := tuntap.NewTun("testtun", "10.0.0.1/24")
	if err != nil {
		return err
	}
	SetupDefaultNetwork(context.Background(), tun, NetworkOptions{Debug: true})
	return nil
}

type tcpHandler func(connFd int)

func NewServer(hostAddr string, handler tcpHandler) (server *Server, err error) {
	if err := EnsureNetwork(); err != nil {
		return nil, err
	}
	serverFd, err := Socket()
	if err != nil {
		return nil, err
	}
	err = Bind(serverFd, hostAddr)
	if err != nil {
		return nil, err
	}
	err = Listen(serverFd, 10)
	if err != nil {
		return nil, err
	}
	return &Server{serverFd: serverFd, hostAddr: hostAddr, handler: handler}, nil
}

type Server struct {
	serverFd int
	hostAddr string
	handler  tcpHandler
	cancel   context.CancelFunc
}

func (s *Server) Start() {
	ctx, cancel := context.WithCancel(context.Background())
	go s.acceptloop(ctx)
	s.cancel = cancel
}

func (s *Server) Stop() {
	s.cancel()
	Close(s.serverFd)
}

func (s *Server) acceptloop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		connFd, err := AcceptWithTimeout(s.serverFd, time.Millisecond*100)
		if err != nil {
			panic(err)
		}
		if connFd == 0 {
			continue
		}
		if s.handler != nil {
			go s.handler(connFd)
		}
	}
}

func TestConnectionsSendRead(t *testing.T) {
	hostAddr := "10.0.0.2:8080"
	connectionNum := 10

	wg := sync.WaitGroup{}
	wg.Add(connectionNum)
	handler := func(connFd int) {
		defer wg.Done()
		for {
			data, err := Read(connFd)
			if err == io.EOF {
				break
			}
			assert.NoError(t, err)
			assert.NoError(t, Send(connFd, data))
		}
		assert.NoError(t, Close(connFd))
	}
	server, err := NewServer(hostAddr, handler)
	if err != nil {
		t.Fatal(err)
	}
	server.Start()
	defer server.Stop()

	clientWg := sync.WaitGroup{}
	clientWg.Add(connectionNum)
	for i := 0; i < connectionNum; i++ {
		go func(i int) {
			defer clientWg.Done()
			conn, err := net.Dial("tcp", hostAddr)
			assert.NoError(t, err)
			defer conn.Close()
			_, err = conn.Write([]byte("hello" + strconv.Itoa(i)))
			assert.NoError(t, err)

			data := make([]byte, 1024)
			n, err := conn.Read(data)
			assert.NoError(t, err)
			assert.Equal(t, "hello"+strconv.Itoa(i), string(data[:n]))
		}(i)
	}
	clientWg.Wait()
	wg.Wait()
}

func TestActiveConnection(t *testing.T) {
	serverAddr := "10.0.0.1:18087"
	assert.NoError(t, EnsureNetwork())
	wg := sync.WaitGroup{}
	wg.Add(1)
	l, err := net.Listen("tcp", serverAddr)
	assert.NoError(t, err)
	go func() {
		defer wg.Done()
		conn, err := l.Accept()
		assert.NoError(t, err)
		for {
			data := make([]byte, 1024)
			n, err := conn.Read(data)
			if err == io.EOF {
				break
			}
			assert.NoError(t, err)
			_, err = conn.Write(data[:n])
			assert.NoError(t, err)
		}
		conn.Close()
		l.Close()
	}()

	cfd, err := Socket()
	assert.NoError(t, err)

	assert.NoError(t, Connect(cfd, serverAddr))
	assert.NoError(t, Send(cfd, []byte("hello")))
	data, err := Read(cfd)
	assert.NoError(t, err)
	assert.Equal(t, "hello", string(data))
	assert.NoError(t, Close(cfd))
	wg.Wait()
}
