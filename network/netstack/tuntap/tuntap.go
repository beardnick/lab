//go:build linux
// +build linux

package tuntap

import (
	"fmt"
	"os/exec"
	"syscall"
	"unsafe"
)

type TunTapDevice string

const (
	TunDevice TunTapDevice = "tun"
	TapDevice TunTapDevice = "tap"
)

type TunTap struct {
	Name       string
	Addr       string
	Nic        int
	flags      int
	DeviceType TunTapDevice
}

// [create tun tap](https://www.kernel.org/doc/html/latest/networking/tuntap.html?highlight=tuntap)
func CreateTunTap(name string, flags int) (device TunTap, err error) {
	// syscall.IFF_NO_PI only raw package
	//flags := syscall.IFF_TUN | syscall.IFF_NO_PI
	//flags := syscall.IFF_TAP | syscall.IFF_NO_PI
	fd, err := syscall.Open("/dev/net/tun", syscall.O_RDWR, 0)
	if err != nil {
		return
	}
	var ifr struct {
		name  [16]byte // device name
		flags uint16   // flag
		_     [22]byte //padding
	}
	copy(ifr.name[:], name)
	ifr.flags = uint16(flags)
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), syscall.TUNSETIFF, uintptr(unsafe.Pointer(&ifr)))
	if errno != 0 {
		syscall.Close(fd)
		err = errno
		return
	}
	var deviceType TunTapDevice
	if flags&syscall.IFF_TUN != 0 {
		deviceType = TunDevice
	}
	if flags&syscall.IFF_TAP != 0 {
		deviceType = TapDevice
	}
	device = TunTap{
		Name:       name,
		Nic:        fd,
		flags:      flags,
		DeviceType: deviceType,
	}
	return

}

func NewTun(name string) (device TunTap, err error) {
	flags := syscall.IFF_TUN | syscall.IFF_NO_PI
	return CreateTunTap(name, flags)
}

func NewTap(name string) (device TunTap, err error) {
	flags := syscall.IFF_TAP | syscall.IFF_NO_PI
	return CreateTunTap(name, flags)
}

// # startup tun/tap device
// ip link set up dev $name
func SetUpLink(name string) error {
	out, err := exec.Command("ip", "link", "set", name, "up").CombinedOutput()
	if err != nil {
		err = fmt.Errorf("%v:%v", err, string(out))
		return err
	}
	return nil
}

// # set tun/tap device ip
// ip addr add 192.168.0.1/24 dev $name
func SetIp(name, ip string) error {
	out, err := exec.Command("ip", "addr", "add", ip, "dev", name).CombinedOutput()
	if err != nil {
		err = fmt.Errorf("%v:%v", err, string(out))
		return err
	}
	return nil
}

func SetRoute(name, addr string) error {
	out, err := exec.Command("ip", "route", "add", addr, "dev", name).CombinedOutput()
	if err != nil {
		err = fmt.Errorf("%v:%v", err, string(out))
		return err
	}
	return nil
}

// # show tun/tap device info
// ip addr show $name
func IpShow(name string) (string, error) {
	out, err := exec.Command("ip", "addr", "show", name).CombinedOutput()
	if err != nil {
		err = fmt.Errorf("%v:%v", err, string(out))
		return "", err
	}
	return string(out), nil
}
