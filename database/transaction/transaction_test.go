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

func CreateClient(address string) (c *Client, err error) {
	conn, err := net.Dial("tcp", address)
	if err != nil {
		return
	}
	c = &Client{conn: conn}
	return
}

func (c *Client) Set(key, value string) (err error) {
	_, err = c.conn.Write([]byte(fmt.Sprintf("set %s %s\n", key, value)))
	return
}

func (c *Client) Begin() (err error) {
	_, err = c.conn.Write([]byte("begin\n"))
	return
}

func (c *Client) Commit() (err error) {
	_, err = c.conn.Write([]byte("commit\n"))
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

func Test_trans(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	server := &Server{
		address: ":19090",
		ctx:     ctx,
	}
	server.Start()

	time.Sleep(time.Second)

	client1, err := CreateClient("127.0.0.1:19090")
	assert.Nil(t, err)

	client2, err := CreateClient("127.0.0.1:19090")
	assert.Nil(t, err)

	assert.Nil(t, client1.Set("hello", "world"))
	value, err := client1.Get("hello")
	assert.Nil(t, err)
	assert.Equal(t, "world", value)

	assert.Nil(t, client1.Begin())
	assert.Nil(t, client1.Set("hello", "world1"))
	value, err = client1.Get("hello")
	assert.Nil(t, err)
	assert.Equal(t, "world1", value)
	value, err = client2.Get("hello")
	assert.Nil(t, err)
	assert.Equal(t, "world", value)
	assert.Nil(t, client1.Commit())
	time.Sleep(time.Millisecond * 10)
	value, err = client2.Get("hello")
	assert.Nil(t, err)
	assert.Equal(t, "world1", value)
	cancel()
}
