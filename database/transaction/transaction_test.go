package main

import (
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type Client struct {
	conn net.Conn
}

func CreateClient(address string, deadLine time.Duration) (c *Client, err error) {
	var conn net.Conn
	timeout := time.After(deadLine)
	for {
		select {
		case <-timeout:
			return
		default:
			conn, err = net.Dial("tcp", address)
			if err != nil {
				time.Sleep(time.Millisecond * 10)
				continue
			}
			c = &Client{conn: conn}
			return
		}
	}
}

func (c *Client) Set(key, value string) (err error) {
	_, err = c.conn.Write([]byte(fmt.Sprintf("set %s %s\n", key, value)))
	if err != nil {
		return
	}
	buf := make([]byte, 1024)
	_, err = c.conn.Read(buf)
	return
}

func (c *Client) Begin() (err error) {
	_, err = c.conn.Write([]byte("begin\n"))
	if err != nil {
		return
	}
	buf := make([]byte, 1024)
	_, err = c.conn.Read(buf)
	return
}

func (c *Client) Commit() (err error) {
	_, err = c.conn.Write([]byte("commit\n"))
	if err != nil {
		return
	}
	buf := make([]byte, 1024)
	_, err = c.conn.Read(buf)
	return
}

func (c *Client) Rollback() (err error) {
	_, err = c.conn.Write([]byte("rollback\n"))
	if err != nil {
		return
	}
	buf := make([]byte, 1024)
	_, err = c.conn.Read(buf)
	return
}

func (c *Client) Get(key string) (value string, err error) {
	_, err = c.conn.Write([]byte(fmt.Sprintf("get %s\n", key)))
	if err != nil {
		return
	}
	buf := make([]byte, 1024)
	n, err := c.conn.Read(buf)
	if err != nil {
		return
	}
	value = strings.TrimSpace(string(buf[:n]))
	return
}

func createServer() (s *Server, err error) {
	port, err := GetFreePort()
	if err != nil {
		return
	}
	s = &Server{
		address: fmt.Sprintf("127.0.0.1:%d", port),
	}
	return
}

// GetFreePort asks the kernel for a free open port that is ready to use.
func GetFreePort() (port int, err error) {
	var a *net.TCPAddr
	if a, err = net.ResolveTCPAddr("tcp", "localhost:0"); err == nil {
		var l *net.TCPListener
		if l, err = net.ListenTCP("tcp", a); err == nil {
			defer l.Close()
			return l.Addr().(*net.TCPAddr).Port, nil
		}
	}
	return
}

func Test_TransGetSet(t *testing.T) {
	server, err := createServer()
	assert.Nil(t, err)
	ctx, stop := context.WithCancel(context.Background())
	server.Start(ctx)

	client, err := CreateClient(server.address, time.Second)
	assert.Nil(t, err)

	// haven't set hello, should get ""
	value, err := client.Get("hello")
	assert.Nil(t, err)
	assert.Equal(t, "ok", value)

	// set hello, should get hello value
	assert.Nil(t, client.Set("hello", "world"))
	value, err = client.Get("hello")
	assert.Nil(t, err)
	assert.Equal(t, "world", value)

	stop()
}

func Test_TransCommit(t *testing.T) {
	server, err := createServer()
	assert.Nil(t, err)
	ctx, stop := context.WithCancel(context.Background())
	server.Start(ctx)

	client1, err := CreateClient(server.address, time.Second)
	assert.Nil(t, err)
	client2, err := CreateClient(server.address, time.Second)
	assert.Nil(t, err)

	// set hello world, should get world
	assert.Nil(t, client1.Set("hello", "world"))
	value, err := client1.Get("hello")
	assert.Nil(t, err)
	assert.Equal(t, "world", value)

	// set hello world1 in transaction, should get world1 in transaction
	assert.Nil(t, client1.Begin())
	assert.Nil(t, client1.Set("hello", "world1"))
	value, err = client1.Get("hello")
	assert.Nil(t, err)
	assert.Equal(t, "world1", value)

	// transaction haven't been committed
	// get hello from another transaction, should get world
	value, err = client2.Get("hello")
	assert.Nil(t, err)
	assert.Equal(t, "world", value)

	// transaction have been committed
	// should get world1
	assert.Nil(t, client1.Commit())
	value, err = client1.Get("hello")
	assert.Nil(t, err)
	assert.Equal(t, "world1", value)
	value, err = client2.Get("hello")
	assert.Nil(t, err)
	assert.Equal(t, "world1", value)

	stop()
}

func Test_TransShouldSeeTransSetValue(t *testing.T) {
	server, err := createServer()
	assert.Nil(t, err)
	ctx, stop := context.WithCancel(context.Background())
	server.Start(ctx)
	defer stop()

	client1, err := CreateClient(server.address, time.Second)
	assert.Nil(t, err)
	client2, err := CreateClient(server.address, time.Second)
	assert.Nil(t, err)

	client1.Begin()
	client2.Begin()

	err = client1.Set("hello", "1")
	assert.Nil(t, err)
	err = client2.Set("hello", "2")
	assert.Nil(t, err)

	v, err := client1.Get("hello")
	assert.Nil(t, err)
	assert.Equal(t, "1", v)

	v, err = client2.Get("hello")
	assert.Nil(t, err)
	assert.Equal(t, "2", v)

}

func Test_TransRollback(t *testing.T) {
	server, err := createServer()
	assert.Nil(t, err)
	ctx, stop := context.WithCancel(context.Background())
	server.Start(ctx)

	client1, err := CreateClient(server.address, time.Second)
	assert.Nil(t, err)
	client2, err := CreateClient(server.address, time.Second)
	assert.Nil(t, err)

	// set hello world, should get world
	assert.Nil(t, client1.Set("hello", "world"))
	value, err := client1.Get("hello")
	assert.Nil(t, err)
	assert.Equal(t, "world", value)

	// set hello world1 in transaction, should get world1 in transaction
	assert.Nil(t, client1.Begin())
	assert.Nil(t, client1.Set("hello", "world1"))
	value, err = client1.Get("hello")
	assert.Nil(t, err)
	assert.Equal(t, "world1", value)

	// transaction haven't been committed
	// get hello from another transaction, should get world
	value, err = client2.Get("hello")
	assert.Nil(t, err)
	assert.Equal(t, "world", value)

	// transaction have been rollbacked
	// should get world
	assert.Nil(t, client1.Rollback())
	value, err = client1.Get("hello")
	assert.Nil(t, err)
	assert.Equal(t, "world", value)
	value, err = client2.Get("hello")
	assert.Nil(t, err)
	assert.Equal(t, "world", value)

	stop()
}
