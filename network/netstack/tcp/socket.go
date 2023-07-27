package tcp

import (
	"errors"
	"fmt"
	"log"
	"math/rand"
	"net"
	"network/netstack/tuntap"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
)

type Socket struct {
	connections []*Connection
	//Nic         *Nic
	Dev   *tuntap.TunTap
	Host  string
	Port  uint16
	ReadC chan tuntap.Packet
}

func NewSocket() Socket {
	return Socket{}
}

type TcpState int

const (
	CLOSED TcpState = iota
	LISTEN
	SYN_RCVD
	SYN_SENT
	ESTAB
	FIN_WAIT_1
	CLOSE_WAIT
	CLOSING
	FINWAIT_2
	TIME_WAIT
	LAST_ACK
)

func (s TcpState) String() string {
	return [...]string{
		"CLOSED",
		"LISTEN",
		"SYN_RCVD",
		"SYN_SENT",
		"ESTAB",
		"FIN_WAIT_1",
		"CLOSE_WAIT",
		"CLOSING",
		"FINWAIT_2",
		"TIME_WAIT",
		"LAST_ACK",
	}[s]
}

type Connection struct {
	Nic     int
	State   TcpState
	SrcIp   net.IP
	DstIp   net.IP
	SrcPort layers.TCPPort
	DstPort layers.TCPPort
	Nxt     uint32
	Seq     uint32
	window  []byte
}

func (c Connection) Window() uint16 {
	return uint16(len(c.window))
}

func NewConnection(pack tuntap.Packet, wind int) Connection {
	conn := Connection{
		State:   LISTEN,
		SrcIp:   pack.IpPack.SrcIP,
		DstIp:   pack.IpPack.DstIP,
		SrcPort: pack.TcpPack.SrcPort,
		DstPort: pack.TcpPack.DstPort,
		window:  make([]byte, wind),
	}
	return conn
}

func (c Connection) IsTarget(ipPack *layers.IPv4, tcpPack *layers.TCP) bool {
	return ipPack.DstIP.Equal(c.SrcIp) &&
		ipPack.SrcIP.Equal(c.DstIp) &&
		tcpPack.SrcPort == c.DstPort &&
		tcpPack.DstPort == c.SrcPort
}

func (c Connection) String() string {
	return fmt.Sprintf("%s:%s -> %s:%s state %s nxt %d nic %d",
		c.SrcIp, c.SrcPort,
		c.DstIp, c.DstPort,
		c.State,
		c.Nxt,
		c.Nic)
}

func (s *Socket) Send(conn int, buf []byte) (n int, err error) {
	defer func() {
		if e := recover(); e != nil {
			err = tuntap.ConnectionClosedErr
		}
	}()
	connection := s.connections[conn]
	ipLay := layers.IPv4{
		Version:  4,
		TTL:      64,
		Protocol: layers.IPProtocolTCP,
		SrcIP:    connection.DstIp,
		DstIP:    connection.SrcIp,
	}
	//fmt.Println("send conn.seq:", connection.Seq)
	tcpLay := layers.TCP{
		BaseLayer: layers.BaseLayer{
			Payload: buf,
		},
		SrcPort: connection.DstPort,
		DstPort: connection.SrcPort,
		Seq:     connection.Seq,
		// note: PSH and ACK must send together
		PSH:    true,
		ACK:    true,
		Ack:    connection.Nxt,
		Window: connection.Window(),
	}
	err = s.WritePacketWithBuf(&ipLay, &tcpLay, buf)
	return
}

func (s *Socket) Rcvd(conn int) (buf []byte, err error) {
	defer func() {
		if e := recover(); e != nil {
			err = tuntap.ConnectionClosedErr
		}
	}()
	connection := s.connections[conn]
	pack, err := s.ReadPacket()
	if errors.Is(err, tuntap.NotValidTcpErr) {
		err = nil
		return
	}
	if err != nil {
		return
	}
	tcpPack, ipPack := pack.TcpPack, pack.IpPack
	if tcpPack.PSH {
		buf = tcpPack.Payload
		err = s.TcpPSHACK(ipPack, tcpPack, connection)
		return
	}
	if tcpPack.FIN {
		err = s.TcpFIN(ipPack, tcpPack, connection)
		if err != nil {
			return
		}
	}
	return
}

func (s *Socket) ReadPacket() (pack tuntap.Packet, err error) {
	return s.Dev.ReadTcpPacket(s.Port)
}

func (s *Socket) TcpFIN(ip *layers.IPv4, tcp *layers.TCP, connection *Connection) (err error) {
	defer func() {
		if e := recover(); e != nil {
			err = tuntap.ConnectionClosedErr
		}
	}()
	ipLay := layers.IPv4{
		Version:  4,
		TTL:      64,
		Protocol: layers.IPProtocolTCP,
		SrcIP:    connection.SrcIp,
		DstIP:    connection.DstIp,
	}
	//fmt.Println("send conn.seq:", connection.Seq)
	tcpLay := layers.TCP{
		SrcPort: connection.SrcPort,
		DstPort: connection.DstPort,
		Seq:     tcp.Ack,
		// note: PSH and ACK must send together
		ACK:    true,
		Ack:    tcp.Seq + 1,
		Window: connection.Window(),
	}
	err = s.WritePacket(&ipLay, &tcpLay)
	if err != nil {
		return
	}
	tcpLay.FIN = true
	err = s.WritePacket(&ipLay, &tcpLay)
	pack, err := s.ReadPacket()
	if err != nil {
		return
	}
	tcpData := pack.TcpPack
	if tcpData.ACK && tcpData.Ack == tcpLay.Seq+1 {
		fmt.Println("disconnect succeed")
		s.connections = append(s.connections[:connection.Nic], s.connections[connection.Nic+1:]...)
	}
	return
}

func (s *Socket) TcpPSHACK(ip *layers.IPv4, tcp *layers.TCP, conn *Connection) (err error) {

	ipLay := *ip
	ipLay.SrcIP = ip.DstIP
	ipLay.DstIP = ip.SrcIP

	tcpLay := *tcp
	tcpLay.SrcPort = tcp.DstPort
	tcpLay.DstPort = tcp.SrcPort
	tcpLay.PSH = false
	tcpLay.ACK = true
	tcpLay.Ack = tcp.Seq + uint32(len(tcp.Payload))
	tcpLay.Window = uint16(conn.Window())
	tcpLay.Payload = []byte{}
	// PSH + ACK
	if tcp.ACK {
		// note: tcp.Ack is what client expect seq next
		conn.Seq = tcp.Ack
	}
	// note: seq is random, seq real value maybe not equal to tcpdump output value
	// tcpdump calculate seq as relative seq
	tcpLay.Seq = conn.Seq
	conn.Nxt = tcpLay.Ack
	return s.WritePacket(&ipLay, &tcpLay)
}

var ()

func Close(conn int) (err error) {
	return
}

func (s *Socket) Bind(host string, port uint16) (err error) {
	s.Host = host
	s.Port = port
	dev, err := tuntap.Route(host)
	if err != nil {
		return
	}
	dev.Bind(port)
	s.Dev = dev
	return
}

func (s *Socket) Listen() {
}

func (s *Socket) Accept() (conn int, err error) {
	var connection Connection
	for {
		var pack tuntap.Packet
		pack, err = s.ReadPacket()
		if err != nil {
			log.Println("err:", err)
			continue
		}
		switch connection.State {
		case CLOSED:
			if pack.TcpPack.SYN {
				connection, err = s.SendSyn(pack)
				if err != nil {
					log.Println("err:", err)
					continue
				}
				continue
			}
		case SYN_RCVD:
			if pack.TcpPack.ACK && connection.Nxt == pack.TcpPack.Ack {
				connection.State = ESTAB
				connection.Seq = 0
				s.connections = append(s.connections, &connection)
				conn = len(s.connections) - 1
				fmt.Println("handshake succeed")
				return
			}
		default:
			log.Println("not expect packet, ignore")
		}
	}
}

func (s *Socket) SendSyn(pack tuntap.Packet) (conn Connection, err error) {

	conn = NewConnection(pack, 1024)
	ipPack, tcpPack := pack.IpPack, pack.TcpPack

	ipLay := *ipPack
	ipLay.SrcIP = conn.DstIp
	ipLay.DstIP = conn.SrcIp

	tcpLay := *tcpPack

	tcpLay.SrcPort = conn.DstPort
	tcpLay.DstPort = conn.SrcPort

	// <SEQ=random><ACK=lastSeq + 1><CTL=SYN,ACK>
	tcpLay.Seq = uint32(rand.Int())
	tcpLay.Ack = tcpPack.Seq + 1
	tcpLay.SYN = true
	tcpLay.ACK = true

	tcpLay.Window = uint16(conn.Window())
	conn.Nxt = tcpLay.Seq + 1

	err = s.WritePacket(&ipLay, &tcpLay)
	if err != nil {
		return
	}
	conn.State = SYN_RCVD
	return
}

func (s *Socket) WritePacketWithBuf(ip *layers.IPv4, tcp *layers.TCP, buf []byte) (err error) {
	// checksum needed
	err = tcp.SetNetworkLayerForChecksum(ip)
	if err != nil {
		return
	}
	buffer := gopacket.NewSerializeBuffer()
	err = gopacket.SerializeLayers(buffer, gopacket.SerializeOptions{
		FixLengths:       true,
		ComputeChecksums: true,
	},
		ip,
		tcp,
		gopacket.Payload(buf),
	)
	if err != nil {
	}
	// invalid argument if buffer is not valid ip packet
	err = s.Dev.Write(buffer.Bytes())
	return
}

func (s *Socket) WritePacket(ip *layers.IPv4, tcp *layers.TCP) (err error) {
	// checksum needed
	err = tcp.SetNetworkLayerForChecksum(ip)
	if err != nil {
		return
	}
	buffer := gopacket.NewSerializeBuffer()
	err = gopacket.SerializeLayers(buffer, gopacket.SerializeOptions{
		FixLengths:       true,
		ComputeChecksums: true,
	},
		ip,
		tcp,
	)
	if err != nil {
		return
	}
	//fmt.Println("write:", buffer.Bytes())
	// invalid argument if buffer is not valid ip packet
	err = s.Dev.Write(buffer.Bytes())
	return
}
