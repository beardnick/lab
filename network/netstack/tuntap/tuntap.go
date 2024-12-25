package tuntap

import (
	"syscall"
	"unsafe"
)

func createTap(name string) (fd int, err error) {
	return createTunTap(name, syscall.IFF_TAP|syscall.IFF_NO_PI)
}

func createTun(name string) (fd int, err error) {
	return createTunTap(name, syscall.IFF_TUN|syscall.IFF_NO_PI)
}

// [create tun tap](https://www.kernel.org/doc/html/latest/networking/tuntap.html?highlight=tuntap)
// syscall.IFF_NO_PI only raw package
// flags := syscall.IFF_TUN | syscall.IFF_NO_PI
// flags := syscall.IFF_TAP | syscall.IFF_NO_PI
func createTunTap(name string, flags int) (fd int, err error) {
	fd, err = syscall.Open("/dev/net/tun", syscall.O_RDWR, 0)
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
	return
}
