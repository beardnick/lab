package tcp

import (
	"errors"
	"fmt"
	"log"
	"math/rand"
	"net"
	"network/netstack/tuntap"
	"sync"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
)

type Socket struct {
	Nic         *Nic
	Host        string
	Port        uint16
	connections []*Connection
	ReadC       chan Packet
}

func NewSocket(nic *Nic) Socket {
	return Socket{
		Nic:   nic,
		ReadC: make(chan Packet),
	}
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

type Nic struct {
	nic     int
	sockets map[uint16]*Socket
	sync.RWMutex
}

type Packet struct {
	IpPack  *layers.IPv4
	TcpPack *layers.TCP
}

func NewNic(nic int) *Nic {
	return &Nic{
		nic:     nic,
		sockets: map[uint16]*Socket{},
		RWMutex: sync.RWMutex{},
	}
}

func (n *Nic) Up() {
	go func() {
		for {
			pack, err := ReadPacket(n.nic)
			if err != nil {
				//log.Println(err)
				continue
			}
			n.RLock()
			s := n.sockets[uint16(pack.TcpPack.DstPort)]
			n.RUnlock()
			if s == nil {
				// TODO:  22-10-30 //
				log.Println("send connection refuse")
				continue
			}
			go func() { s.ReadC <- pack }()
		}
	}()
}

func (n *Nic) RegisterListener(s *Socket) (err error) {
	n.Lock()
	defer n.Unlock()
	oldSock := n.sockets[s.Port]
	if oldSock != nil {
		err = PortAlreadyInUsed
		return
	}
	n.sockets[s.Port] = s
	return
}

func (n *Nic) NewIpV4() layers.IPv4 {
	return layers.IPv4{
		BaseLayer: layers.BaseLayer{
			Contents: []byte{},
			Payload:  []byte{},
		},
		Version:    0,
		IHL:        0,
		TOS:        0,
		Length:     0,
		Id:         0,
		Flags:      0,
		FragOffset: 0,
		TTL:        0,
		Protocol:   0,
		Checksum:   0,
		SrcIP:      []byte{},
		DstIP:      []byte{},
		Options:    []layers.IPv4Option{},
		Padding:    []byte{},
	}
}

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

func NewConnection(pack Packet, wind int) Connection {
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
			err = ConnectionClosedErr
		}
	}()
	connection := connections[conn]
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
			err = ConnectionClosedErr
		}
	}()
	connection := connections[conn]
	pack, err := s.ReadPacket()
	if errors.Is(err, NotValidTcpErr) {
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

func (s *Socket) ReadPacket() (pack Packet, err error) {
	pack = <-s.ReadC
	return
}

func (s *Socket) TcpFIN(ip *layers.IPv4, tcp *layers.TCP, connection *Connection) (err error) {
	defer func() {
		if e := recover(); e != nil {
			err = ConnectionClosedErr
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
		connections = append(connections[:connection.Nic], connections[connection.Nic+1:]...)
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

var (
	connections = []*Connection{}
)

func Close(conn int) (err error) {
	return
}

func (s *Socket) Listen(host string, port uint16) {
	s.Host = host
	s.Port = port
	s.Nic.RegisterListener(s)
}

func (s *Socket) Accept() (conn int, err error) {
	var connection Connection
	for {
		var pack Packet
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
				connections = append(connections, &connection)
				conn = len(connections) - 1
				fmt.Println("handshake succeed")
				return
			}
		default:
			log.Println("not expect packet, ignore")
		}
	}
}

func (s *Socket) SendSyn(pack Packet) (conn Connection, err error) {

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
	_, err = tuntap.Write(s.Nic.nic, buffer.Bytes())
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
	_, err = tuntap.Write(s.Nic.nic, buffer.Bytes())
	return
}

func ReadPacket(fd int) (pack Packet, err error) {
	buf := make([]byte, 1024)
	n, err := tuntap.Read(fd, buf)
	if err != nil {
		log.Fatal(err)
	}
	p := gopacket.NewPacket(
		buf[:n],
		layers.LayerTypeIPv4,
		gopacket.Default,
	)
	return UnWrapTcp(p)
}

func UnWrapTcp(packet gopacket.Packet) (pack Packet, err error) {
	ipLayer := packet.Layer(layers.LayerTypeIPv4)
	if ipLayer == nil {
		err = ConnectionClosedErr
		return
	}
	pack.IpPack, _ = ipLayer.(*layers.IPv4)
	tcpLayer := packet.Layer(layers.LayerTypeTCP)
	if tcpLayer == nil {
		err = NotValidTcpErr
		return
	}
	pack.TcpPack, _ = tcpLayer.(*layers.TCP)
	return
}

type Err struct {
	Code int
	Msg  string
}

func (e Err) Error() string {
	return e.Msg
}

func (e Err) Is(err error) bool {
	if newErr, ok := err.(Err); ok {
		return newErr.Code == e.Code
	}
	return false
}

var (
	ConnectionClosedErr = Err{Code: 1, Msg: "connection closed"}
	NotValidTcpErr      = Err{Code: 2, Msg: "not valid tcp packet"}
	PortAlreadyInUsed   = Err{Code: 3, Msg: "port already in used"}
)
