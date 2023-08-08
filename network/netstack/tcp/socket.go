package tcp

import (
	"math/rand"
	"net"
	"network/netstack/tuntap"
	"sync"
	"time"

	"github.com/pkg/errors"

	"network/infra"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
)

type Socket struct {
	Dev         tuntap.INicDevice
	Host        string
	Port        uint16
	WindowSize  uint16
	portConn    map[layers.TCPPort]*Connection
	SynQueue    map[layers.TCPPort]*Connection
	AcceptQueue []*Connection
	sockFds     map[int]Stream
	fdLimit     int
	sync.RWMutex
}

func NewSocket() Socket {
	return Socket{
		portConn:   make(map[layers.TCPPort]*Connection),
		SynQueue:   make(map[layers.TCPPort]*Connection),
		WindowSize: 1024,
		fdLimit:    1024,
		sockFds:    make(map[int]Stream),
	}
}

type TcpState int

const (
	StateClosed TcpState = iota
	StateListen
	StateSynRcvd
	StateSynSent
	StateEstab
	StateFinWait1
	StateCloseWait
	StateClosing
	StateFinWait2
	StateTimeWait
	StateLastAck
)

func (s TcpState) String() string {
	return [...]string{
		"StateClosed",
		"StateListen",
		"StateSynRcvd",
		"StateSynSent",
		"StateEstab",
		"StateFinWait1",
		"StateCloseWait",
		"StateClosing",
		"StateFinWait2",
		"StateTimeWait",
		"StateLastAck",
	}[s]
}

type Connection struct {
	Socket   *Socket
	State    TcpState
	SelfIp   net.IP
	PeerIp   net.IP
	SelfPort layers.TCPPort
	PeerPort layers.TCPPort
	PeerNxt  uint32
	SelfNxt  uint32
	windSize uint16
	recvWin  []byte
	sendWin  []byte
	sendPtr  int
	recvPtr  int
	sockFd   Stream
	fd       int
}

type Stream struct {
	In    chan []byte
	Out   chan []byte
	Close chan struct{}
}

func NewStream() Stream {
	return Stream{
		In:    make(chan []byte),
		Out:   make(chan []byte),
		Close: make(chan struct{}),
	}
}

func (c Connection) Window() uint16 {
	return c.windSize
}

func NewConnection(socket *Socket, wind uint16) *Connection {
	return &Connection{
		State:    StateListen,
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

// todo send should wait last data to be acked
func (s *Socket) Send(conn int, buf []byte) (n int, err error) {
	s.RLock()
	defer s.RUnlock()
	s.sockFds[conn].Out <- buf
	return
}

func (s *Socket) Rcvd(conn int) (buf []byte, err error) {
	s.RLock()
	defer s.RUnlock()
	buf = <-s.sockFds[conn].In
	return
}

func (s *Socket) ReadPacket() (pack tuntap.Packet, err error) {
	pack, err = s.Dev.ReadTcpPacket(s.Port)
	if err != nil {
		return
	}
	ip, tcp := pack.IpPack, pack.TcpPack
	infra.Logger.Debug().Msgf("read packet %s:%s -> %s:%s syn:%v ack:%v fin:%v psh:%v seq:%v ack:%v payload:%v",
		ip.SrcIP, tcp.SrcPort, ip.DstIP, tcp.DstPort, tcp.SYN, tcp.ACK, tcp.FIN, tcp.PSH, tcp.Seq, tcp.Ack, string(tcp.Payload))
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

	err = c.Socket.WritePacket(&ipLay, &tcpLay, nil)
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

	return c.Socket.WritePacket(&ipLay, &tcpLay, nil)
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

func (c *Connection) waitAllDataSent() {
	t := time.NewTicker(time.Millisecond)
	for {
		select {
		case <-t.C:
			if len(c.sendWin) == 0 {
				// all data has been sent
				return
			}
		}
	}
}

func (c *Connection) Close() (err error) {
	c.waitAllDataSent()
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

	tcpLay := layers.TCP{
		BaseLayer:  layers.BaseLayer{},
		SrcPort:    c.SelfPort, //
		DstPort:    c.PeerPort, //
		Seq:        c.SelfNxt,  //
		Ack:        c.PeerNxt,  //
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

	err = c.Socket.WritePacket(&ipLay, &tcpLay, nil)
	if err != nil {
		return
	}
	c.State = StateFinWait1
	c.sockFd.Close <- struct{}{}
	return
}

// todo close will wait until all data has been sent
func (s *Socket) Close(conn int) (err error) {
	s.RLock()
	defer s.RUnlock()
	s.releaseSockFd(conn)
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
				infra.Logger.Error().Err(err)
				continue
			}
			connection := s.getConnection(pack)
			err = s.tcpStateMachine(connection, pack)
			if err != nil {
				infra.Logger.Error().Err(err)
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
	case StateClosed, StateListen:
		if pack.TcpPack.SYN {
			err = s.sendSyn(connection, pack)
			if err != nil {
				return
			}
			s.SynQueue[pack.TcpPack.SrcPort] = connection
		}
	case StateSynRcvd:
		if pack.TcpPack.ACK && connection.PeerNxt == pack.TcpPack.Seq {
			s.Lock()
			connection.State = StateEstab
			connection.recvWin = make([]byte, 0, s.WindowSize)
			connection.sendWin = make([]byte, 0, s.WindowSize)
			fd, sockFd := s.applyNewSockFd()
			if fd < 0 {
				err = errors.New("no fds")
				return
			}
			connection.fd = fd
			connection.sockFd = sockFd
			s.portConn[connection.PeerPort] = connection
			delete(s.SynQueue, connection.PeerPort)
			s.AcceptQueue = append(s.AcceptQueue, connection)
			connection.startSendDataLoop()
			s.Unlock()
			return
		}
	case StateEstab:
		if pack.TcpPack.FIN {
			err = connection.tcpFIN(pack.IpPack, pack.TcpPack)
			if err != nil {
				return
			}
			connection.State = StateClosed
			return
		}
		if pack.TcpPack.ACK && !pack.TcpPack.PSH && len(pack.TcpPack.Payload) == 0 {
			// just normal ack
			// todo handle tcp split packet
			connection.caculateSelfNext(pack.IpPack, pack.TcpPack)
			connection.sendWin = []byte{}
		}
		err = connection.sendAck(pack.IpPack, pack.TcpPack)
		if err != nil {
			return
		}
		payload := pack.TcpPack.Payload
		if len(payload) == 0 {
			return
		}
		connection.recvWin = append(connection.recvWin, payload...)
		// push data to application when PSH is set
		if pack.TcpPack.PSH {
			connection.sockFd.In <- connection.recvWin
			connection.recvWin = make([]byte, 0, s.WindowSize)
		}
	case StateFinWait1:
		if pack.TcpPack.ACK && connection.PeerNxt == pack.TcpPack.Seq {
			if !pack.TcpPack.FIN {
				connection.State = StateFinWait2
			} else {
				connection.State = StateCloseWait
				delete(s.portConn, connection.PeerPort)
				connection.sendAck(pack.IpPack, pack.TcpPack)
			}
		}
	case StateFinWait2:
		if pack.TcpPack.FIN {
			connection.State = StateCloseWait
			delete(s.portConn, connection.PeerPort)
			// last ack
			connection.sendAck(pack.IpPack, pack.TcpPack)
		}
	//case :
	//case :
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

	err = s.WritePacket(&ipLay, &tcpLay, nil)
	if err != nil {
		return
	}
	conn.State = StateSynRcvd
	return
}

func (s *Socket) WritePacket(ip *layers.IPv4, tcp *layers.TCP, data []byte) (err error) {
	infra.Logger.Debug().Msgf("write packet %s:%s -> %s:%s syn:%v ack:%v fin:%v psh:%v seq:%v ack:%v payload:%v",
		ip.SrcIP, tcp.SrcPort, ip.DstIP, tcp.DstPort, tcp.SYN, tcp.ACK, tcp.FIN, tcp.PSH, tcp.Seq, tcp.Ack, string(data))
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
		gopacket.Payload(data),
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

func (s *Socket) applyNewSockFd() (fdId int, newFd Stream) {
	// todo better fd allocating algorithm
	for i := 0; i < s.fdLimit; i++ {
		if _, ok := s.sockFds[i]; ok {
			continue
		}
		newFd = NewStream()
		fdId = i
		s.sockFds[fdId] = newFd
		return
	}
	fdId = -1
	return
}

func (s *Socket) releaseSockFd(fdId int) {
	c := s.sockFds[fdId]
	c.Close <- struct{}{} // send close signal
	<-c.Close             // wait close complete
	close(c.In)
	close(c.Out)
	delete(s.sockFds, fdId)
}

func (c *Connection) startSendDataLoop() {
	go func() {
		for {
			select {
			case data := <-c.sockFd.Out:
				_, err := c.Send(data)
				if err != nil {
					infra.Logger.Error().Err(err)
				}
			case <-c.sockFd.Close:
				c.Close()
				return
			}
		}
	}()
}

func (c *Connection) Send(buf []byte) (n int, err error) {
	infra.Logger.Debug().Msg("send data")
	c.sendWin = append(c.sendWin, buf...)
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

	tcpLay := layers.TCP{
		SrcPort:    c.SelfPort, //
		DstPort:    c.PeerPort, //
		Seq:        c.SelfNxt,  //
		Ack:        c.PeerNxt,  //
		DataOffset: 0,
		FIN:        false,
		SYN:        false,
		RST:        false,
		PSH:        true,
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
	err = c.Socket.WritePacket(&ipLay, &tcpLay, buf)
	return
}
