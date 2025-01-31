package socket

import (
	"fmt"
	"net"
	"netstack/tcpip"
)

type PacketBuilder struct {
	ip  *tcpip.IPPack
	tcp *tcpip.TcpPack
	err error
}

func NewPacketBuilder(opt NetworkOptions) *PacketBuilder {
	builder := &PacketBuilder{}

	builder.tcp = &tcpip.TcpPack{
		TcpHeader: &tcpip.TcpHeader{
			WindowSize: opt.WindowSize,
		},
	}
	builder.ip = &tcpip.IPPack{
		IPHeader: &tcpip.IPHeader{
			Version:    4,
			Flags:      2,
			TimeToLive: 64,
			Protocol:   uint8(tcpip.ProtocolTCP),
		},
		Payload: builder.tcp,
	}

	return builder
}

func (b *PacketBuilder) SetAddr(addr SocketAddr) *PacketBuilder {
	srcIP := net.ParseIP(addr.SrcIP).To4()
	dstIP := net.ParseIP(addr.DstIP).To4()
	if srcIP == nil || dstIP == nil {
		b.err = fmt.Errorf("invalid IPv4 address: %s, %s", addr.SrcIP, addr.DstIP)
		return b
	}
	b.ip.IPHeader.SrcIP = srcIP
	b.ip.IPHeader.DstIP = dstIP
	b.tcp.PseudoHeader = &tcpip.PseudoHeader{
		SrcIP: b.ip.IPHeader.SrcIP,
		DstIP: b.ip.IPHeader.DstIP,
	}
	b.tcp.TcpHeader.SrcPort = addr.SrcPort
	b.tcp.TcpHeader.DstPort = addr.DstPort
	return b
}

func (b *PacketBuilder) SetSeq(seq uint32) *PacketBuilder {
	b.tcp.SequenceNumber = seq
	return b
}

func (b *PacketBuilder) SetAck(ack uint32) *PacketBuilder {
	b.tcp.AckNumber = ack
	return b
}

func (b *PacketBuilder) SetFlags(flags tcpip.TcpFlag) *PacketBuilder {
	b.tcp.Flags = uint8(flags)
	return b
}
func (b *PacketBuilder) SetPayload(payload tcpip.NetworkPacket) *PacketBuilder {
	b.tcp.Payload = payload
	return b
}

func (b *PacketBuilder) Build() (*tcpip.IPPack, *tcpip.TcpPack, error) {
	if b.err != nil {
		return nil, nil, b.err
	}
	return b.ip, b.tcp, nil
}
