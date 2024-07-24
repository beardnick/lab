package main

import (
	"encoding/hex"
	"fmt"
	"log"
	"os/exec"
	"syscall"
	"testing"
	"unsafe"
)

func Test_tun(t *testing.T) {
	args := struct {
		cidr string
		name string
	}{
		cidr: "11.0.0.1/24",
		name: "testtun1",
	}
	fd, err := CreateTunTap(args.name, syscall.IFF_TUN|syscall.IFF_NO_PI)
	if err != nil {
		log.Fatalln(err)
	}

	out, err := exec.Command("ip", "addr", "add", args.cidr, "dev", args.name).CombinedOutput()
	if err != nil {
		log.Fatalln(err)
	}
	fmt.Println(out)

	out, err = exec.Command("ip", "link", "set", args.name, "up").CombinedOutput()
	if err != nil {
		log.Fatalln(err)
	}
	fmt.Println(out)
	buf := make([]byte, 1024)
	for {
		n, err := syscall.Read(fd, buf)
		if err != nil {
			log.Fatalln(err)
		}
		fmt.Println(hex.Dump(buf[:n]))
	}
}

// [create tun tap](https://www.kernel.org/doc/html/latest/networking/tuntap.html?highlight=tuntap)
func CreateTunTap(name string, flags int) (fd int, err error) {
	// syscall.IFF_NO_PI only raw package
	//flags := syscall.IFF_TUN | syscall.IFF_NO_PI
	//flags := syscall.IFF_TAP | syscall.IFF_NO_PI
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
