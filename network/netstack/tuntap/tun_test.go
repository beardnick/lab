package tuntap

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestTunRead(t *testing.T) {
	name := "tun0"
	cidr := "11.0.0.1/24"

	tun, err := NewTun(name, cidr)
	assert.Nil(t, err)

	b := make([]byte, 1024)
	n, err := tun.Read(b)
	assert.Nil(t, err)
	assert.NotEqual(t, n, 0)

	// you can view the packets with this
	// fmt.Println(hex.Dump(b[:n]))
}
