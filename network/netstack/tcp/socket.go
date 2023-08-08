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

type SocketOptions struct {
	WindowSize uint16
	Ttl        uint8
}

type Socket struct {
	Dev         tuntap.INicDevice
	Host        string
	Port        uint16
	estabConn   map[layers.TCPPort]*Connection
	synQueue    map[layers.TCPPort]*Connection
	acceptQueue []*Connection
	sockFds     map[int]Fd
	fdLimit     int
	Opt         SocketOptions
	sync.RWMutex
}

func NewSocket(opt SocketOptions) Socket {
	return Socket{
		estabConn: make(map[layers.TCPPort]*Connection),
		synQueue:  make(map[layers.TCPPort]*Connection),
		fdLimit:   1024,
		sockFds:   make(map[int]Fd),
		Opt:       opt,
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
	SrcIp    net.IP
	DstIp    net.IP
	SrcPort  layers.TCPPort
	DstPort  layers.TCPPort
	DstNxt   uint32
	SrcNxt   uint32
	windSize uint16
	recvWin  []byte
	sendWin  []byte
	sendPtr  int
	recvPtr  int
	sockFd   Fd
	fd       int
}

type Fd struct {
	In    chan []byte
	Out   chan []byte
	Close chan struct{}
}

func NewFd() Fd {
	return Fd{
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

func (s *Socket) cleanConnection(conn *Connection) {
	delete(s.estabConn, conn.DstPort)
}

func (c *Connection) validateSeq(pack tuntap.Packet) bool {
	return pack.TcpPack.Seq == c.DstNxt
}

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
	ipLay := c.ipPack()

	c.caculatePeerNext(ip, tcp)
	c.caculateSelfNext(ip, tcp)

	tcpLay := c.tcpPack(TcpFlags{FIN: true, ACK: true})

	err = c.Socket.WritePacket(&ipLay, &tcpLay, nil)
	if err != nil {
		return
	}
	return
}

func (c *Connection) sendAck(ip *layers.IPv4, tcp *layers.TCP) (err error) {
	ipLay := c.ipPack()

	c.caculateSelfNext(ip, tcp)
	c.caculatePeerNext(ip, tcp)

	tcpLay := c.tcpPack(TcpFlags{
		ACK: true,
	})

	return c.Socket.WritePacket(&ipLay, &tcpLay, nil)
}

func (c *Connection) caculatePeerNext(ip *layers.IPv4, tcp *layers.TCP) {
	if tcp.SYN || tcp.FIN {
		c.DstNxt = tcp.Seq + 1
		return
	}
	if len(tcp.Payload) > 0 {
		c.DstNxt = c.DstNxt + uint32(len(tcp.Payload))
	}
}

func (c *Connection) caculateSelfNext(ip *layers.IPv4, tcp *layers.TCP) {
	if tcp.SYN {
		c.SrcNxt = uint32(rand.Int())
		return
	}
	if tcp.ACK {
		// todo verify this ack
		c.SrcNxt = tcp.Ack
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

	ipLay := c.ipPack()

	tcpLay := c.tcpPack(TcpFlags{
		FIN: true,
		ACK: true,
	})

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
			if len(s.acceptQueue) > 0 {
				conn = s.acceptQueue[0].fd
				s.acceptQueue = s.acceptQueue[1:]
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
			err = s.handleSyn(connection, pack)
			if err != nil {
				return
			}
			s.synQueue[pack.TcpPack.SrcPort] = connection
		}
	case StateSynRcvd:
		if pack.TcpPack.ACK && connection.DstNxt == pack.TcpPack.Seq {
			s.Lock()
			connection.State = StateEstab
			connection.recvWin = make([]byte, 0, s.Opt.WindowSize)
			connection.sendWin = make([]byte, 0, s.Opt.WindowSize)
			fd, sockFd := s.applyNewSockFd()
			if fd < 0 {
				err = errors.New("no fds")
				return
			}
			connection.fd = fd
			connection.sockFd = sockFd
			s.estabConn[connection.DstPort] = connection
			delete(s.synQueue, connection.DstPort)
			s.acceptQueue = append(s.acceptQueue, connection)
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
			connection.recvWin = make([]byte, 0, s.Opt.WindowSize)
		}
	case StateFinWait1:
		if pack.TcpPack.ACK && connection.DstNxt == pack.TcpPack.Seq {
			if !pack.TcpPack.FIN {
				connection.State = StateFinWait2
			} else {
				connection.State = StateCloseWait
				delete(s.estabConn, connection.DstPort)
				connection.sendAck(pack.IpPack, pack.TcpPack)
			}
		}
	case StateFinWait2:
		if pack.TcpPack.FIN {
			connection.State = StateCloseWait
			delete(s.estabConn, connection.DstPort)
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

func (s *Socket) handleSyn(conn *Connection, pack tuntap.Packet) (err error) {
	conn.SrcIp = pack.IpPack.DstIP
	conn.DstIp = pack.IpPack.SrcIP
	conn.SrcPort = pack.TcpPack.DstPort
	conn.DstPort = pack.TcpPack.SrcPort

	conn.caculatePeerNext(pack.IpPack, pack.TcpPack)
	conn.caculateSelfNext(pack.IpPack, pack.TcpPack)

	ipLay := conn.ipPack()

	tcpLay := conn.tcpPack(TcpFlags{
		SYN: true,
		ACK: true,
	})

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
	connection, ok = s.estabConn[pack.TcpPack.SrcPort]
	if ok {
		return
	}
	connection, ok = s.synQueue[pack.TcpPack.SrcPort]
	if ok {
		return
	}
	connection = NewConnection(s, s.Opt.WindowSize)
	return
}

func (s *Socket) applyNewSockFd() (fdId int, newFd Fd) {
	// todo better fd allocating algorithm
	for i := 0; i < s.fdLimit; i++ {
		if _, ok := s.sockFds[i]; ok {
			continue
		}
		newFd = NewFd()
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
	ipLay := c.ipPack()

	tcpLay := c.tcpPack(TcpFlags{
		PSH: true,
		ACK: true,
	})

	err = c.Socket.WritePacket(&ipLay, &tcpLay, buf)
	return
}

func (c *Connection) ipPack() layers.IPv4 {
	return layers.IPv4{
		BaseLayer:  layers.BaseLayer{},      // todo
		Version:    4,                       // ipv4
		IHL:        0,                       // let gopacket compute this
		TOS:        0,                       // type of service
		Length:     0,                       // let gopacket compute this
		Id:         0,                       // todo unique id
		Flags:      layers.IPv4DontFragment, // do not fragment in ip layer, tcp layer will do better
		FragOffset: 0,                       // let gopacket compute this
		TTL:        c.Socket.Opt.Ttl,        // the default ttl is stored in /proc/sys/net/ipv4/ip_default_ttl
		Protocol:   layers.IPProtocolTCP,    // protocol over ip layer
		Checksum:   0,                       // let gopacket compute this
		SrcIP:      c.SrcIp,                 // self ip
		DstIP:      c.DstIp,                 // dst ip
		Options:    nil,                     // options, ignore now
		Padding:    nil,                     // ignore now
	}

}

type TcpFlags struct {
	FIN, SYN, RST, PSH, ACK, URG, ECE, CWR, NS bool
}

func (c *Connection) tcpPack(flags TcpFlags) layers.TCP {
	return layers.TCP{
		SrcPort:    c.SrcPort,
		DstPort:    c.DstPort,
		Seq:        c.SrcNxt,
		Ack:        c.DstNxt,
		DataOffset: 0,
		FIN:        flags.FIN,
		SYN:        flags.SYN,
		RST:        flags.RST,
		PSH:        flags.PSH,
		ACK:        flags.ACK,
		URG:        flags.URG,
		ECE:        flags.ECE,
		CWR:        flags.CWR,
		NS:         flags.NS,
		Window:     uint16(c.Window()),
		Checksum:   0, // let go packet caculate it
		Urgent:     0,
		Options:    nil,
		Padding:    nil,
	}
}
