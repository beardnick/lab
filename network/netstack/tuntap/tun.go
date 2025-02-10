package tuntap

import (
	"net"
	"os/exec"
	"syscall"
)

type Tun struct {
	name string
	fd   int
	ip   net.IP
	cidr net.IPNet
}

func NewTun(name, cidr string) (*Tun, error) {
	fd, err := createTun(name)
	if err != nil {
		return nil, err
	}
	ip, ipNet, err := net.ParseCIDR(cidr)
	if err != nil {
		return nil, err
	}
	tun := &Tun{name: name, fd: fd, ip: ip, cidr: *ipNet}

	_, err = exec.Command("ip", "addr", "add", cidr, "dev", name).CombinedOutput()
	if err != nil {
		return nil, err
	}

	_, err = exec.Command("ip", "link", "set", name, "up").CombinedOutput()
	if err != nil {
		return nil, err
	}
	return tun, nil
}

func (t *Tun) Read(b []byte) (n int, err error) {
	return syscall.Read(t.fd, b)
}

func (t *Tun) Write(b []byte) (n int, err error) {
	return syscall.Write(t.fd, b)
}

func (t *Tun) Close() error {
	return syscall.Close(t.fd)
}

func (t *Tun) IP() net.IP {
	return t.ip
}

func (t *Tun) CIDR() net.IPNet {
	return t.cidr
}
