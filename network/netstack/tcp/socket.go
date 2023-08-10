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
	WindowSize       uint16
	Ttl              uint8
	LimitOpenFiles   int
	SendDataInterval time.Duration
}

type Socket struct {
	Dev         tuntap.INicDevice
	Host        string
	Port        uint16
	estabConn   map[layers.TCPPort]*Connection
	synQueue    map[layers.TCPPort]*Connection
	acceptQueue []*Connection
	sockFds     map[int]Fd
	Opt         SocketOptions
	sync.RWMutex
}

func NewSocket(opt SocketOptions) Socket {
	return Socket{
		estabConn: make(map[layers.TCPPort]*Connection),
		synQueue:  make(map[layers.TCPPort]*Connection),
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
	dstAcked uint32
	srcAcked uint32
	windSize uint16
	recvWin  []byte
	sendWin  []byte
	sockFd   Fd
	fd       int
	sentSeq  uint32
	sync.Mutex
}

type Fd struct {
	In        chan []byte
	Out       chan []byte
	OutFinish chan struct{}
	Close     chan struct{}
}

func NewFd() Fd {
	return Fd{
		In:        make(chan []byte),
		Out:       make(chan []byte),
		OutFinish: make(chan struct{}),
		Close:     make(chan struct{}),
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
	return pack.TcpPack.Seq == c.dstAcked+1
}

func (s *Socket) Send(conn int, buf []byte) (n int, err error) {
	s.RLock()
	defer s.RUnlock()
	s.sockFds[conn].Out <- buf
	<-s.sockFds[conn].OutFinish
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

func (c *Connection) handleTcpFin(ip *layers.IPv4, tcp *layers.TCP) (err error) {
	ipLay := c.ipPack()

	c.Lock()
	defer c.Unlock()
	c.caculatePeerNext(ip, tcp)
	c.caculateSelfNext(ip, tcp)

	if len(c.sendWin) == 0 {
		tcpLay := c.tcpPack(TcpFlags{FIN: true, ACK: true})
		err = c.Socket.WritePacket(&ipLay, &tcpLay, nil)
		if err != nil {
			return
		}
		c.State = StateLastAck
		return
	}
	tcpLay := c.tcpPack(TcpFlags{ACK: true})
	err = c.Socket.WritePacket(&ipLay, &tcpLay, nil)
	if err != nil {
		return
	}
	c.State = StateCloseWait
	return
}

func (c *Connection) sendSimpleDataAck(ip *layers.IPv4, tcp *layers.TCP) (err error) {
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
		c.dstAcked = tcp.Seq
		return
	}
	// #LAST payload may be split
	if len(tcp.Payload) > 0 {
		c.dstAcked = c.dstAcked + uint32(len(tcp.Payload))
	}
}

func (c *Connection) caculateSelfNext(ip *layers.IPv4, tcp *layers.TCP) {
	if tcp.SYN {
		c.srcAcked = uint32(rand.Int()) - 1
		return
	}
	if tcp.ACK {
		newAcked := tcp.Ack - 1
		if newAcked > c.srcAcked && newAcked <= c.sentSeq {
			// really ack this connections data
			// |acked+1|acked+2|...|new_acked          |new_acked+1|
			// |   0   |   1   |...|new_acked-(acked+1)|new_acked-acked|
			// |       sent                            |unsent    |
			// new_acked = tcp.Ack - 1
			c.sendWin = c.sendWin[newAcked-c.srcAcked:]
			c.srcAcked = newAcked
		}
		return
	}
}

func (c *Connection) waitAllDataSent() {
	t := time.NewTicker(time.Millisecond)
	for {
		select {
		case <-t.C:
			c.Lock()
			// #NOTE
			// 注意这里的时序性问题
			// 一定是使用sendWin来判断是否数据传输完成
			// 如果使用acked sentSeq来判断，是有一定滞后性的
			// 有可能sendWin已经有数据了，但是没有发送出去，那么acked sentSeq就是相等的
			// 会误判断数据已经传输完成了
			if len(c.sendWin) == 0 {
				c.Unlock()
				// all data has been sent
				return
			}
			c.Unlock()
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
	c.Lock()
	c.State = StateFinWait1
	c.Unlock()
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

func (s *Socket) tcpStateMachine(conn *Connection, pack tuntap.Packet) (err error) {
	// todo 校验重复包
	switch conn.State {
	case StateClosed, StateListen:
		if pack.TcpPack.SYN {
			err = s.handleSyn(conn, pack)
		}
	case StateSynRcvd:
		err = s.handleSynAck(conn, pack)
	case StateEstab:
		err = conn.handleEstabPack(pack)
	case StateFinWait1:
		err = conn.handleFinWait1(pack)
	case StateFinWait2:
		err = conn.handleFinWait2(pack)
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
	conn.sentSeq = tcpLay.Seq
	s.synQueue[pack.TcpPack.SrcPort] = conn
	// mark syn as a special data logically
	// todo not a good implementation rewrite this
	conn.sendWin = append(conn.sendWin, 'S')
	return
}

func (s *Socket) handleSynAck(conn *Connection, pack tuntap.Packet) (err error) {
	if !conn.validateSeq(pack) {
		return
	}
	if !pack.TcpPack.ACK && pack.TcpPack.Ack != conn.sentSeq {
		// not ack my syn packet
		return
	}
	conn.caculateSelfNext(pack.IpPack, pack.TcpPack)
	conn.State = StateEstab
	conn.recvWin = make([]byte, 0, s.Opt.WindowSize)
	conn.sendWin = make([]byte, 0, s.Opt.WindowSize)

	s.Lock()
	fd, sockFd := s.applyNewSockFd()
	if fd < 0 {
		err = errors.New("no fds")
		return
	}
	conn.fd = fd
	conn.sockFd = sockFd
	s.estabConn[conn.DstPort] = conn
	delete(s.synQueue, conn.DstPort)
	s.acceptQueue = append(s.acceptQueue, conn)
	s.Unlock()

	conn.startSendDataLoop()
	return
}

func (c *Connection) recvData(pack tuntap.Packet) (err error) {
	c.Lock()
	defer c.Unlock()
	payload := pack.TcpPack.Payload
	if len(payload)+len(c.recvWin) > int(c.Socket.Opt.WindowSize) {
		payload = payload[:(len(payload)+len(c.recvWin))-int(c.Socket.Opt.WindowSize)]
	}
	c.recvWin = append(c.recvWin, payload...)
	// push data to application layer when PSH is set
	if pack.TcpPack.PSH {
		c.sockFd.In <- c.recvWin
		c.recvWin = make([]byte, 0, c.Socket.Opt.WindowSize)
	}

	err = c.sendSimpleDataAck(pack.IpPack, pack.TcpPack)
	return
}

func (c *Connection) handleEstabPack(pack tuntap.Packet) (err error) {
	if !pack.TcpPack.ACK {
		infra.Logger.Warn().Msgf("invalid packet,ACK should be true when estabed")
		return
	}
	if !c.validateSeq(pack) {
		return
	}
	if pack.TcpPack.FIN {
		err = c.handleTcpFin(pack.IpPack, pack.TcpPack)
		return
	}
	if len(pack.TcpPack.Payload) == 0 {
		c.caculateSelfNext(pack.IpPack, pack.TcpPack)
		return
	}

	err = c.recvData(pack)
	return
}

func (c *Connection) handleFinWait1(pack tuntap.Packet) (err error) {
	if !c.validateSeq(pack) {
		return
	}
	if !pack.TcpPack.ACK || pack.TcpPack.Ack-1 != c.sentSeq {
		return
	}
	c.Lock()
	defer c.Unlock()
	if !pack.TcpPack.FIN {
		c.State = StateFinWait2
		return
	}
	// todo close wait handle
	c.State = StateCloseWait
	delete(c.Socket.estabConn, c.DstPort)
	err = c.sendSimpleDataAck(pack.IpPack, pack.TcpPack)
	return
}

func (c *Connection) handleFinWait2(pack tuntap.Packet) (err error) {
	if !c.validateSeq(pack) {
		return
	}
	if pack.TcpPack.FIN {
		c.Lock()
		defer c.Unlock()
		c.State = StateCloseWait
		delete(c.Socket.estabConn, c.DstPort)
		err = c.sendSimpleDataAck(pack.IpPack, pack.TcpPack)
		return
	}
	if !pack.TcpPack.ACK {
		return
	}
	if len(pack.TcpPack.Payload) == 0 {
		return
	}
	err = c.recvData(pack)
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
	for i := 0; i < s.Opt.LimitOpenFiles; i++ {
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
			// #NOTE
			// 注意这里的时序问题
			// out中的数据读出来了但是不能说明数据读取成功了
			// 必须要数据装到sendWin中才算成功，才能释放掉阻塞的socket.Send操作
			case data := <-c.sockFd.Out:
				c.Lock()
				// todo send win full
				c.sendWin = append(c.sendWin, data...)
				c.Unlock()
				c.sockFd.OutFinish <- struct{}{} // indicate send data finished
			case <-c.sockFd.Close:
				c.Close()
				return
			}
		}
	}()
	go func() {
		t := time.NewTicker(c.Socket.Opt.SendDataInterval)
		for {
			select {
			case <-t.C:
				c.Lock()
				if len(c.sendWin) > 0 {
					// |acked+1|acked+2|...|sent          |sent+1|
					// |   0   |   1   |...|sent-(acked+1)|sent-acked|
					// |       sent                       |unsent    |
					_, err := c.Send(c.sendWin[c.sentSeq-c.srcAcked:])
					if err != nil {
						infra.Logger.Err(err)
					}
				}
				c.Unlock()
			}
		}
	}()
}

func (c *Connection) Send(buf []byte) (n int, err error) {
	infra.Logger.Debug().Msgf("send data %v", string(buf))
	ipLay := c.ipPack()

	tcpLay := c.tcpPack(TcpFlags{
		PSH: true,
		ACK: true,
	})

	err = c.Socket.WritePacket(&ipLay, &tcpLay, buf)
	if err != nil {
		return
	}
	c.sentSeq = c.sentSeq + uint32(len(buf))
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
		Seq:        c.srcAcked + 1,
		Ack:        c.dstAcked + 1,
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
