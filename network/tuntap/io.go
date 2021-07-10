package tuntap

import "syscall"

func Read(fd int, buf []byte) (n int, err error) {
	return syscall.Read(fd, buf)
}

func Write(fd int, buf []byte) (n int, err error) {
	return syscall.Write(fd, buf)
}

func Close(fd int) (err error) {
	return syscall.Close(fd)
}
