package tuntap

import (
	"github.com/samber/lo"
	"github.com/stretchr/testify/assert"
	"testing"
)

// IFF_TUN   - TUN device (no Ethernet headers)
// IFF_TAP   - TAP device
// IFF_NO_PI - Do not provide packet information
func TestNewTun(t *testing.T) {
	device, err := NewTun("test_tun")
	assert.Nil(t, err, lo.IfF(err != nil, func() string { return err.Error() }))
	assert.Equal(t, TunDevice, device.DeviceType)
	assert.NotZero(t, device.Nic)
	assert.Equal(t, device.Name, "test_tun")
}

func TestNewTap(t *testing.T) {
	device, err := NewTap("test_tap")
	assert.Nil(t, err, lo.IfF(err != nil, func() string { return err.Error() }))
	assert.Equal(t, TapDevice, device.DeviceType)
	assert.NotZero(t, device.Nic)
	assert.Equal(t, device.Name, "test_tap")
}
