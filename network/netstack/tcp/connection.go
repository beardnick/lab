package tcp

import (
	"fmt"
	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"gotcp/tuntap"
	"log"
	"math/rand"
	"net"
)

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
	switch s {
	case CLOSED:
		return "CLOSED"
	case LISTEN:
		return "LISTEN"
	case SYN_RCVD:
		return "SYN_RCVD"
	case SYN_SENT:
		return "SYN_SENT"
	case ESTAB:
		return "ESTAB"
	case FIN_WAIT_1:
		return "FIN_WAIT_1"
	case CLOSE_WAIT:
		return "CLOSE_WAIT"
	case CLOSING:
		return "CLOSING"
	case FINWAIT_2:
	case TIME_WAIT:
	case LAST_ACK:
		return "LAST_ACK"
	}
	return "UNKNOWN"
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

func New(ip *layers.IPv4, tcp *layers.TCP, nic, wind int) Connection {
	conn := Connection{
		Nic:     nic,
		State:   LISTEN,
		SrcIp:   ip.DstIP,
		DstIp:   ip.SrcIP,
		SrcPort: tcp.DstPort,
		DstPort: tcp.SrcPort,
		window:  make([]byte, wind),
	}
	return conn
}

func (c Connection) IsTarget(ip *layers.IPv4, tcp *layers.TCP) bool {
	return ip.DstIP.Equal(c.SrcIp) &&
		ip.SrcIP.Equal(c.DstIp) &&
		tcp.SrcPort == c.DstPort &&
		tcp.DstPort == c.SrcPort
}

func (c Connection) String() string {
	return fmt.Sprintf("%s:%s -> %s:%s state %s nxt %d nic %d",
		c.SrcIp, c.SrcPort,
		c.DstIp, c.DstPort,
		c.State,
		c.Nxt,
		c.Nic)
}

func Send(conn int, buf []byte) (n int, err error) {
	defer func() {
		if e := recover(); e != nil {
			err = ConnectionClosedErr{}
		}
	}()
	connection := connections[conn]
	ipLay := layers.IPv4{
		Version:  4,
		TTL:      64,
		Protocol: layers.IPProtocolTCP,
		SrcIP:    connection.SrcIp,
		DstIP:    connection.DstIp,
	}
	//fmt.Println("send conn.seq:", connection.Seq)
	tcpLay := layers.TCP{
		BaseLayer: layers.BaseLayer{
			Payload: buf,
		},
		SrcPort: connection.SrcPort,
		DstPort: connection.DstPort,
		Seq:     connection.Seq,
		// note: PSH and ACK must send together
		PSH:    true,
		ACK:    true,
		Ack:    connection.Nxt,
		Window: connection.Window(),
	}
	err = WritePacketWithBuf(connection.Nic, &ipLay, &tcpLay, buf)
	return
}

func Rcvd(conn int) (buf []byte, err error) {
	defer func() {
		if e := recover(); e != nil {
			err = ConnectionClosedErr{}
		}
	}()
	connection := connections[conn]
	ip, tcp, err := ReadPacket(connection.Nic)
	if _, ok := err.(NotValidTcpErr); !ok && err != nil {
		return
	}
	for !connection.IsTarget(ip, tcp) {
		ip, tcp, err = ReadPacket(connection.Nic)
		if _, ok := err.(NotValidTcpErr); !ok && err != nil {
			return
		}
	}
	if tcp.PSH {
		buf = tcp.Payload
		err = TcpPSHACK(ip, tcp, connection)
		return
	}
	if tcp.FIN {
		err = TcpFIN(ip, tcp, connection)
		if err != nil {
			return
		}
	}
	return
}

func TcpFIN(ip *layers.IPv4, tcp *layers.TCP, connection *Connection) (err error) {
	defer func() {
		if e := recover(); e != nil {
			err = ConnectionClosedErr{}
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
	err = WritePacket(connection.Nic, &ipLay, &tcpLay)
	if err != nil {
		return
	}
	tcpLay.FIN = true
	err = WritePacket(connection.Nic, &ipLay, &tcpLay)
	ipData, tcpData, err := ReadPacket(connection.Nic)
	if err != nil {
		return
	}
	for !connection.IsTarget(ipData, tcpData) {
		ipData, tcpData, err = ReadPacket(connection.Nic)
		if err != nil {
			return
		}
	}
	if tcpData.ACK && tcpData.Ack == tcpLay.Seq+1 {
		fmt.Println("disconnect succeed")
		connections = append(connections[:connection.Nic], connections[connection.Nic+1:]...)
	}
	return
}

func TcpPSHACK(ip *layers.IPv4, tcp *layers.TCP, conn *Connection) (err error) {

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
	return WritePacket(conn.Nic, &ipLay, &tcpLay)
}

var (
	connections = []*Connection{}
)

func Close(conn int) (err error) {
	return
}

func Accept(fd int) (conn int, err error) {
	var connection Connection
	for {
		//fmt.Println(connection)
		ip, tcp, err := ReadPacket(fd)
		if err != nil {
			log.Println("err:", err)
			continue
		}
		if connection.State != CLOSED {
			if !connection.IsTarget(ip, tcp) {
				log.Println("not target ip port")
				continue
			}
		}
		if tcp.SYN {
			connection, err = SendSyn(fd, ip, tcp)
			if err != nil {
				fmt.Println("err:", err)
				continue
			}
			continue
		}
		if tcp.ACK && connection.State == SYN_RCVD && connection.Nxt == tcp.Ack {
			connection.State = ESTAB
			connection.Seq = 0
			connections = append(connections, &connection)
			conn = len(connections) - 1
			fmt.Println("handshake succeed")
			break
		}
	}
	return
}

func SendSyn(fd int, ip *layers.IPv4, tcp *layers.TCP) (conn Connection, err error) {

	conn = New(ip, tcp, fd, 1024)

	ipLay := *ip
	ipLay.SrcIP = ip.DstIP
	ipLay.DstIP = ip.SrcIP

	tcpLay := *tcp
	tcpLay.SrcPort = tcp.DstPort
	tcpLay.DstPort = tcp.SrcPort
	tcpLay.SYN = true
	tcpLay.ACK = true
	tcpLay.Ack = tcp.Seq + 1
	tcpLay.Seq = uint32(rand.Int())
	tcpLay.Window = uint16(conn.Window())

	conn.Nxt = tcpLay.Seq + 1

	err = WritePacket(fd, &ipLay, &tcpLay)
	if err != nil {
		return
	}
	conn.State = SYN_RCVD
	return
}

func WritePacketWithBuf(fd int, ip *layers.IPv4, tcp *layers.TCP, buf []byte) (err error) {
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
	_, err = tuntap.Write(fd, buffer.Bytes())
	return
}

func WritePacket(fd int, ip *layers.IPv4, tcp *layers.TCP) (err error) {
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
	_, err = tuntap.Write(fd, buffer.Bytes())
	return
}

func ReadPacket(fd int) (ip *layers.IPv4, tcp *layers.TCP, err error) {
	buf := make([]byte, 1024)
	n, err := tuntap.Read(fd, buf)
	if err != nil {
		log.Fatal(err)
	}
	pack := gopacket.NewPacket(
		buf[:n],
		layers.LayerTypeIPv4,
		gopacket.Default,
	)
	return UnWrapTcp(pack)
}

func UnWrapTcp(packet gopacket.Packet) (ip *layers.IPv4, tcp *layers.TCP, err error) {
	ipLayer := packet.Layer(layers.LayerTypeIPv4)
	if ipLayer == nil {
		err = ConnectionClosedErr{}
		return
	}
	ip, _ = ipLayer.(*layers.IPv4)
	tcpLayer := packet.Layer(layers.LayerTypeTCP)
	if tcpLayer == nil {
		err = NotValidTcpErr{}
		return
	}
	tcp, _ = tcpLayer.(*layers.TCP)
	return
}

type ConnectionClosedErr struct {
}

func (c ConnectionClosedErr) Error() string {
	return "connection closed"
}

type NotValidTcpErr struct {
}

func (e NotValidTcpErr) Error() string {
	return "not valid tcp packet"
}
