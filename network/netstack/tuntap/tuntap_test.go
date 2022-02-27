package tuntap

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestNewTun(t *testing.T) {
	assert := assert.New(t)
	dev := "mytun"
	tun, err := NewTun(dev)
	assert.Nil(err)
	err = SetIp(dev, "192.168.0.1/24")
	assert.Nil(err)
	err = SetUpLink(dev)
	assert.Nil(err)
	info, err := IpShow(dev)
	assert.Nil(err)
	assert.Greater(tun, -1)
	assert.NotEmpty(info)
	fmt.Println(info)
}
