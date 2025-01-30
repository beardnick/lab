package tuntap

import (
	"os/exec"
	"syscall"
)

type Tun struct {
	name string
	fd   int
}

func NewTun(name, cidr string) (*Tun, error) {
	fd, err := createTun(name)
	if err != nil {
		return nil, err
	}
	tun := &Tun{name: name, fd: fd}

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
