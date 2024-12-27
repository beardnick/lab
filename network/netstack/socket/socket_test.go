package socket

import (
	"context"
	"net"
	"netstack/tuntap"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSocket(t *testing.T) {
	wg := sync.WaitGroup{}
	readyCh := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		<-readyCh
		conn, err := net.Dial("tcp", "10.0.0.2:8080")
		assert.Nil(t, err)
		assert.NotNil(t, conn)
		conn.Write([]byte("hello"))
	}()

	tun, err := tuntap.NewTun("testtun", "10.0.0.1/24")
	assert.Nil(t, err)
	SetupDefaultNetwork(context.Background(), tun, NetworkOptions{})
	fd, err := TcpSocket()
	assert.Nil(t, err)
	err = Bind(fd, "10.0.0.2:8080")
	assert.Nil(t, err)
	err = Listen(fd, 10)
	assert.Nil(t, err)
	readyCh <- struct{}{}
	wg.Wait()
	// conn, err := Accept(fd)
	// assert.Nil(t, err)
	// assert.NotZero(t, conn)
}
