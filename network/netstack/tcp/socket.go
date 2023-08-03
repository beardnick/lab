package tcp

import (
	"fmt"
	"log"
	"math/rand"
	"net"
	"network/netstack/tuntap"
	"sync"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
)

type Socket struct {
	connections []*Connection
	Dev         tuntap.INicDevice
	Host        string
	Port        uint16
	ReadC       chan tuntap.Packet
	WindowSize  uint16
	portConn    map[layers.TCPPort]*Connection
	AcceptQueue []*Connection
	sockFds     []chan []byte // todo connection close后fd如何分配
	sync.RWMutex
}

func NewSocket() Socket {
	return Socket{
		WindowSize: 1024,
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
	Socket   *Socket
	Nic      int
	State    TcpState
	SelfIp   net.IP
	PeerIp   net.IP
	SelfPort layers.TCPPort
	PeerPort layers.TCPPort
	PeerNxt  uint32
	SelfNxt  uint32
	Seq      uint32
	windSize uint16
	window   []byte
	sockFd   chan []byte
	fd       int
}

func (c Connection) Window() uint16 {
	return c.windSize
}

func NewConnection(wind uint16) *Connection {
	return &Connection{
		State:    LISTEN,
		windSize: wind,
	}
}

func (c Connection) IsTarget(ipPack *layers.IPv4, tcpPack *layers.TCP) bool {
	return ipPack.DstIP.Equal(c.SelfIp) &&
		ipPack.SrcIP.Equal(c.PeerIp) &&
		tcpPack.SrcPort == c.PeerPort &&
		tcpPack.DstPort == c.SelfPort
}

func (c Connection) String() string {
	return fmt.Sprintf("%s:%s -> %s:%s state %s nxt %d nic %d",
		c.SelfIp, c.SelfPort,
		c.PeerIp, c.PeerPort,
		c.State,
		c.PeerNxt,
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
		SrcIP:    connection.PeerIp,
		DstIP:    connection.SelfIp,
	}
	//fmt.Println("send conn.seq:", connection.Seq)
	tcpLay := layers.TCP{
		BaseLayer: layers.BaseLayer{
			Payload: buf,
		},
		SrcPort: connection.PeerPort,
		DstPort: connection.SelfPort,
		Seq:     connection.Seq,
		// note: PSH and ACK must send together
		PSH:    true,
		ACK:    true,
		Ack:    connection.PeerNxt,
		Window: connection.Window(),
	}
	err = s.WritePacketWithBuf(&ipLay, &tcpLay, buf)
	return
}

func (s *Socket) Rcvd(conn int) (buf []byte, err error) {
	s.RLock()
	defer s.RUnlock()
	buf = <-s.sockFds[conn]
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
		SrcIP:    connection.SelfIp,
		DstIP:    connection.PeerIp,
	}
	tcpLay := layers.TCP{
		SrcPort: connection.SelfPort,
		DstPort: connection.PeerPort,
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
	conn.PeerNxt = tcpLay.Ack
	return s.WritePacket(&ipLay, &tcpLay)
}

func (c *Connection) sendAck(ip *layers.IPv4, tcp *layers.TCP) (err error) {
	ipLay := layers.IPv4{
		BaseLayer:  layers.BaseLayer{},      // todo
		Version:    4,                       // ipv4
		IHL:        0,                       // let gopacket compute this
		TOS:        0,                       // type of service
		Length:     0,                       // let gopacket compute this
		Id:         0,                       // todo unique id
		Flags:      layers.IPv4DontFragment, // do not fragment in ip layer, tcp layer will do better
		FragOffset: 0,                       // let gopacket compute this
		TTL:        64,                      // the default ttl is stored in /proc/sys/net/ipv4/ip_default_ttl
		Protocol:   layers.IPProtocolTCP,    // protocol over ip layer
		Checksum:   0,                       // let gopacket compute this
		SrcIP:      c.SelfIp,                // self ip
		DstIP:      c.PeerIp,                // dst ip
		Options:    nil,                     // options, ignore now
		Padding:    nil,                     // ignore now
	}

	c.PeerNxt = c.PeerNxt + uint32(len(tcp.Payload))

	tcpLay := layers.TCP{
		BaseLayer:  layers.BaseLayer{},
		SrcPort:    c.SelfPort, //
		DstPort:    c.PeerPort, //
		Seq:        0,          //
		Ack:        c.PeerNxt,  //
		DataOffset: 0,
		FIN:        false,
		SYN:        false,
		RST:        false,
		PSH:        false,
		ACK:        true,
		URG:        false,
		ECE:        false,
		CWR:        false,
		NS:         false,
		Window:     uint16(c.Window()),
		Checksum:   0, // let go packet caculate it
		Urgent:     0,
		Options:    nil,
		Padding:    nil,
	}

	return c.Socket.WritePacket(&ipLay, &tcpLay)
}

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
	go func() {
		for {
			pack, err := s.ReadPacket()
			if err != nil {
				log.Println("err:", err)
				continue
			}
			connection := s.getConnection(pack)
			if connection == nil {
				connection = NewConnection(s.WindowSize)
			}
			err = s.tcpStateMachine(connection, pack)
			if err != nil {
				return
			}
			if connection.State == ESTAB {
				s.Lock()
				fd, sockFd := s.applyNewSockFd()
				connection.fd = fd
				connection.sockFd = sockFd
				s.portConn[connection.PeerPort] = connection
				s.AcceptQueue = append(s.AcceptQueue, connection)
				s.Unlock()
				break
			}
		}
	}()
	return
}

func (s *Socket) Accept() (conn int, err error) {
	t := time.Tick(time.Millisecond)
	for {
		select {
		case <-t:
			s.Lock()
			if len(s.AcceptQueue) > 0 {
				conn = s.AcceptQueue[0].fd
				s.AcceptQueue = s.AcceptQueue[1:]
				s.Unlock()
				return
			}
			s.Unlock()
		}
	}
}

func (s *Socket) tcpStateMachine(connection *Connection, pack tuntap.Packet) (err error) {
	switch connection.State {
	case CLOSED, LISTEN:
		if pack.TcpPack.SYN {
			err = s.SendSyn(connection, pack)
			if err != nil {
				return
			}
		}
	case SYN_RCVD:
		if pack.TcpPack.ACK && connection.PeerNxt == pack.TcpPack.Ack {
			connection.State = ESTAB
			connection.window = make([]byte, 0, s.WindowSize)
			connection.Seq = 0
			s.connections = append(s.connections, connection)
			return
		}
	case ESTAB:
		if pack.TcpPack.FIN {
			err = s.TcpFIN(pack.IpPack, pack.TcpPack, connection)
			if err != nil {
				return
			}
		}
		err = connection.sendAck(pack.IpPack, pack.TcpPack)
		if err != nil {
			return
		}
		payload := pack.TcpPack.Payload
		if len(payload) == 0 {
			return
		}
		connection.window = append(connection.window, payload...)
		// push data to application when PSH is set
		if pack.TcpPack.PSH {
			connection.sockFd <- connection.window
			connection.window = make([]byte, 0, s.WindowSize)
		}
	default:
		log.Println("not expect packet, ignore")
	}
	return
}

func (s *Socket) SendSyn(conn *Connection, pack tuntap.Packet) (err error) {

	conn.SelfIp = pack.IpPack.SrcIP
	conn.PeerIp = pack.IpPack.DstIP
	conn.SelfPort = pack.TcpPack.SrcPort
	conn.PeerPort = pack.TcpPack.DstPort

	ipPack, tcpPack := pack.IpPack, pack.TcpPack

	ipLay := *ipPack
	ipLay.SrcIP = conn.PeerIp
	ipLay.DstIP = conn.SelfIp

	tcpLay := *tcpPack

	tcpLay.SrcPort = conn.PeerPort
	tcpLay.DstPort = conn.SelfPort

	// <SEQ=random><ACK=lastSeq + 1><CTL=SYN,ACK>
	tcpLay.Seq = uint32(rand.Int())
	tcpLay.Ack = tcpPack.Seq + 1
	tcpLay.SYN = true
	tcpLay.ACK = true

	tcpLay.Window = uint16(conn.Window())
	conn.PeerNxt = tcpLay.Seq + 1

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
	err = s.Dev.Write(buffer.Bytes())
	return
}

func (s *Socket) getConnection(pack tuntap.Packet) (connection *Connection) {
	connection = s.portConn[pack.TcpPack.SrcPort]
	return
}

func (s *Socket) applyNewSockFd() (fdId int, newFd chan []byte) {
	newFd = make(chan []byte)
	s.sockFds = append(s.sockFds, newFd)
	fdId = len(s.sockFds) - 1
	return
}
