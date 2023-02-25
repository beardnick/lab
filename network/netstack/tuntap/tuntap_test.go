package tuntap

import (
	"syscall"
	"testing"

	"github.com/stretchr/testify/assert"
)

// IFF_TUN   - TUN device (no Ethernet headers)
// IFF_TAP   - TAP device
// IFF_NO_PI - Do not provide packet information
func TestNewTun(t *testing.T) {
	tuntap, err := NewTunTap("raw_pkg_tun", syscall.IFF_TUN|syscall.IFF_NO_PI)
	assert.Nil(t, err)
	assert.NotZero(t, tuntap)
}
