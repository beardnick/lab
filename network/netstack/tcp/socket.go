package tcp

import (
	"math/rand"
	"net"
	"network/netstack/tuntap"
	"sync"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"network/infra"
)

type Socket struct {
	connections []*Connection
	Dev         tuntap.INicDevice
	Host        string
	Port        uint16
	ReadC       chan tuntap.Packet
	WindowSize  uint16
	portConn    map[layers.TCPPort]*Connection
	SynQueue    map[layers.TCPPort]*Connection
	AcceptQueue []*Connection
	sockFds     []chan []byte // todo connection close后fd如何分配
	sync.RWMutex
}

func NewSocket() Socket {
	return Socket{
		portConn:   make(map[layers.TCPPort]*Connection),
		SynQueue:   make(map[layers.TCPPort]*Connection),
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
	windSize uint16
	window   []byte
	sockFd   chan []byte
	fd       int
}

func (c Connection) Window() uint16 {
	return c.windSize
}

func NewConnection(socket *Socket, wind uint16) *Connection {
	return &Connection{
		State:    LISTEN,
		windSize: wind,
		Socket:   socket,
	}
}

func (c Connection) IsTarget(ipPack *layers.IPv4, tcpPack *layers.TCP) bool {
	return ipPack.DstIP.Equal(c.SelfIp) &&
		ipPack.SrcIP.Equal(c.PeerIp) &&
		tcpPack.SrcPort == c.PeerPort &&
		tcpPack.DstPort == c.SelfPort
}

//func (s *Socket) Send(conn int, buf []byte) (n int, err error) {
//	defer func() {
//		if e := recover(); e != nil {
//			err = tuntap.ConnectionClosedErr
//		}
//	}()
//	connection := s.connections[conn]
//	ipLay := layers.IPv4{
//		Version:  4,
//		TTL:      64,
//		Protocol: layers.IPProtocolTCP,
//		SrcIP:    connection.PeerIp,
//		DstIP:    connection.SelfIp,
//	}
//	//fmt.Println("send conn.seq:", connection.Seq)
//	tcpLay := layers.TCP{
//		BaseLayer: layers.BaseLayer{
//			Payload: buf,
//		},
//		SrcPort: connection.PeerPort,
//		DstPort: connection.SelfPort,
//		Seq:     connection.Seq,
//		// note: PSH and ACK must send together
//		PSH:    true,
//		ACK:    true,
//		Ack:    connection.PeerNxt,
//		Window: connection.Window(),
//	}
//	err = s.WritePacketWithBuf(&ipLay, &tcpLay, buf)
//	return
//}

func (s *Socket) Rcvd(conn int) (buf []byte, err error) {
	s.RLock()
	defer s.RUnlock()
	buf = <-s.sockFds[conn]
	return
}

func (s *Socket) ReadPacket() (pack tuntap.Packet, err error) {
	pack, err = s.Dev.ReadTcpPacket(s.Port)
	if err != nil {
		return
	}
	ip, tcp := pack.IpPack, pack.TcpPack
	infra.Logger.Debug().Msgf("read packet %s:%s -> %s:%s syn:%v ack:%v fin:%v psh:%v seq:%v ack:%v",
		ip.SrcIP, tcp.SrcPort, ip.DstIP, tcp.DstPort, tcp.SYN, tcp.ACK, tcp.FIN, tcp.PSH, tcp.Seq, tcp.Ack)
	return
}

func (c *Connection) tcpFIN(ip *layers.IPv4, tcp *layers.TCP) (err error) {
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

	c.caculatePeerNext(ip, tcp)
	c.caculateSelfNext(ip, tcp)

	tcpLay := layers.TCP{
		BaseLayer:  layers.BaseLayer{},
		SrcPort:    c.SelfPort, //
		DstPort:    c.PeerPort, //
		Seq:        c.SelfNxt,
		Ack:        c.PeerNxt, //
		DataOffset: 0,
		FIN:        true,
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

	err = c.Socket.WritePacket(&ipLay, &tcpLay)
	if err != nil {
		return
	}
	return
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

	c.caculateSelfNext(ip, tcp)
	c.caculatePeerNext(ip, tcp)

	tcpLay := layers.TCP{
		BaseLayer:  layers.BaseLayer{},
		SrcPort:    c.SelfPort, //
		DstPort:    c.PeerPort, //
		Seq:        c.SelfNxt,  //
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

func (c *Connection) caculatePeerNext(ip *layers.IPv4, tcp *layers.TCP) {
	if tcp.SYN || tcp.FIN {
		c.PeerNxt = tcp.Seq + 1
		return
	}
	if len(tcp.Payload) > 0 {
		c.PeerNxt = c.PeerNxt + uint32(len(tcp.Payload))
	}
}

func (c *Connection) caculateSelfNext(ip *layers.IPv4, tcp *layers.TCP) {
	if tcp.SYN {
		c.SelfNxt = uint32(rand.Int())
		return
	}
	if tcp.ACK {
		// todo verify this ack
		c.SelfNxt = tcp.Ack
		return
	}
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
				infra.Logger.Error().AnErr("read packet", err)
				continue
			}
			connection := s.getConnection(pack)
			err = s.tcpStateMachine(connection, pack)
			if err != nil {
				infra.Logger.Error().AnErr("handle packet", err)
				continue
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
	// todo 校验重复包
	switch connection.State {
	case CLOSED, LISTEN:
		if pack.TcpPack.SYN {
			err = s.sendSyn(connection, pack)
			if err != nil {
				return
			}
			s.SynQueue[pack.TcpPack.SrcPort] = connection
		}
	case SYN_RCVD:
		if pack.TcpPack.ACK && connection.PeerNxt == pack.TcpPack.Seq {
			s.Lock()
			connection.State = ESTAB
			connection.window = make([]byte, 0, s.WindowSize)
			s.connections = append(s.connections, connection)
			fd, sockFd := s.applyNewSockFd()
			connection.fd = fd
			connection.sockFd = sockFd
			s.portConn[connection.PeerPort] = connection
			s.AcceptQueue = append(s.AcceptQueue, connection)
			s.Unlock()
			return
		}
	case ESTAB:
		if pack.TcpPack.FIN {
			err = connection.tcpFIN(pack.IpPack, pack.TcpPack)
			if err != nil {
				return
			}
			return
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
		infra.Logger.Debug().Msg("not expect packet, ignore")
	}
	return
}

func (s *Socket) sendSyn(conn *Connection, pack tuntap.Packet) (err error) {

	conn.SelfIp = pack.IpPack.DstIP
	conn.PeerIp = pack.IpPack.SrcIP
	conn.SelfPort = pack.TcpPack.DstPort
	conn.PeerPort = pack.TcpPack.SrcPort

	conn.caculatePeerNext(pack.IpPack, pack.TcpPack)
	conn.caculateSelfNext(pack.IpPack, pack.TcpPack)

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
		SrcIP:      conn.SelfIp,             // self ip
		DstIP:      conn.PeerIp,             // dst ip
		Options:    nil,                     // options, ignore now
		Padding:    nil,                     // ignore now
	}

	tcpLay := layers.TCP{
		BaseLayer:  layers.BaseLayer{},
		SrcPort:    conn.SelfPort, //
		DstPort:    conn.PeerPort, //
		Seq:        conn.SelfNxt,  //
		Ack:        conn.PeerNxt,  //
		DataOffset: 0,
		FIN:        false,
		SYN:        true,
		RST:        false,
		PSH:        false,
		ACK:        true,
		URG:        false,
		ECE:        false,
		CWR:        false,
		NS:         false,
		Window:     uint16(conn.Window()),
		Checksum:   0, // let go packet caculate it
		Urgent:     0,
		Options:    nil,
		Padding:    nil,
	}

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
	infra.Logger.Debug().Msgf("write packet %s:%s -> %s:%s syn:%v ack:%v fin:%v psh:%v seq:%v ack:%v",
		ip.SrcIP, tcp.SrcPort, ip.DstIP, tcp.DstPort, tcp.SYN, tcp.ACK, tcp.FIN, tcp.PSH, tcp.Seq, tcp.Ack)
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
	ok := false
	connection, ok = s.portConn[pack.TcpPack.SrcPort]
	if ok {
		return
	}
	connection, ok = s.SynQueue[pack.TcpPack.SrcPort]
	if ok {
		return
	}
	connection = NewConnection(s, s.WindowSize)
	return
}

func (s *Socket) applyNewSockFd() (fdId int, newFd chan []byte) {
	newFd = make(chan []byte)
	s.sockFds = append(s.sockFds, newFd)
	fdId = len(s.sockFds) - 1
	return
}
