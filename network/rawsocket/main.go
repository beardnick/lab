package main

import (
	"encoding/binary"
	"fmt"
	"log"
	"os"
	"syscall"

	"golang.org/x/net/ipv4"
)

type TcpHeader struct {
	SourcePort uint16
	DestPort   uint16
	SeqNum     uint32
	AckNum     uint32
	DataOffset byte
	Flags      byte
	UrgFlag    bool
	AckFlag    bool
	PshFlag    bool
	RstFlag    bool
	SynFlag    bool
	FinFlag    bool
	WindowSize uint16
	Checksum   uint16
	UrgentPtr  uint16
}

func parseTcpHeaer(buf []byte) TcpHeader {
	flags := buf[13]
	return TcpHeader{
		SourcePort: binary.BigEndian.Uint16(buf[0:2]),
		DestPort:   binary.BigEndian.Uint16(buf[2:4]),
		SeqNum:     binary.BigEndian.Uint32(buf[4:8]),
		AckNum:     binary.BigEndian.Uint32(buf[8:12]),
		DataOffset: (buf[12] >> 4) * 4,
		Flags:      buf[13],
		UrgFlag:    flags&0x20 != 0,
		AckFlag:    flags&0x10 != 0,
		PshFlag:    flags&0x08 != 0,
		RstFlag:    flags&0x04 != 0,
		SynFlag:    flags&0x02 != 0,
		FinFlag:    flags&0x01 != 0,
		WindowSize: binary.BigEndian.Uint16(buf[14:16]),
		Checksum:   binary.BigEndian.Uint16(buf[16:18]),
		UrgentPtr:  binary.BigEndian.Uint16(buf[18:20]),
	}
}

func main() {
	fd, err := syscall.Socket(syscall.AF_INET, syscall.SOCK_RAW, syscall.IPPROTO_TCP)
	if err != nil {
		log.Fatal(err)
	}
	f := os.NewFile(uintptr(fd), fmt.Sprintf("fd %d", fd))
	for {
		buf := make([]byte, 1500)
		f.Read(buf)
		ip4header, _ := ipv4.ParseHeader(buf[:20])
		tcpHeader := parseTcpHeaer(buf[20:40])
		if tcpHeader.DestPort == 9090 {
			fmt.Printf("%v:%v -> %v:%v\n", ip4header.Src, tcpHeader.SourcePort, ip4header.Dst, tcpHeader.DestPort)
		}
	}
}
