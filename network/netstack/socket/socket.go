package socket

import (
	"fmt"
	"io"
	"log"
	"math/rand"
	"net"
	"netstack/tcpip"
	"sync"
)

type ListenSocket struct {
	sync.Mutex
	network        *Network
	Fd             int
	ip             net.IP
	port           int
	listening      bool
	acceptQueue    chan *ConnectSocket
	synQueue       sync.Map
	connectSockets sync.Map
	readCh         chan []byte
	writeCh        chan *tcpip.IPPack
}

func NewListenSocket(network *Network, fd int) *ListenSocket {
	return &ListenSocket{
		network:        network,
		Fd:             fd,
		synQueue:       sync.Map{},
		connectSockets: sync.Map{},
		acceptQueue:    make(chan *ConnectSocket, network.opt.Backlog),
		readCh:         make(chan []byte),
		writeCh:        make(chan *tcpip.IPPack),
	}
}

func (s *ListenSocket) getConnectSocket(key string) (cs *ConnectSocket, ok bool) {
	value, ok := s.connectSockets.Load(key)
	if !ok {
		return nil, false
	}
	cs = value.(*ConnectSocket)
	return cs, true
}

func (s *ListenSocket) Listen(backlog int) (err error) {
	s.acceptQueue = make(chan *ConnectSocket, backlog)
	s.listening = true
	go s.runloop()
	return nil
}

func (s *ListenSocket) Accept() (cfd int, err error) {
	cs := <-s.acceptQueue
	return cs.fd, nil
}

func (s *ListenSocket) runloop() {
	for {
		select {
		case data := <-s.writeCh:
			tcpPack := data.Payload.(*tcpip.TcpPack)
			s.handle(data, tcpPack)
		}
	}
}

func (s *ListenSocket) handle(ipPack *tcpip.IPPack, tcpPack *tcpip.TcpPack) {
	var sock *ConnectSocket
	s.Lock()
	defer s.Unlock()
	sock, ok := s.getConnectSocket(
		fmt.Sprintf(
			"%s:%d->%s:%d",
			ipPack.SrcIP,
			tcpPack.SrcPort,
			ipPack.DstIP,
			tcpPack.DstPort,
		))
	if !ok {
		sock = NewConnectSocket(
			s,
			ipPack.DstIP,
			tcpPack.DstPort,
			ipPack.SrcIP,
			tcpPack.SrcPort,
		)
	}
	resp, err := s.handleState(sock, tcpPack)
	if err != nil {
		log.Println(err)
		return
	}
	if resp == nil {
		return
	}
	data, err := resp.Encode()
	if err != nil {
		log.Println(err)
		return
	}
	s.network.writeCh <- data
}

func (s *ListenSocket) handleState(sock *ConnectSocket, tcpPack *tcpip.TcpPack) (resp *tcpip.IPPack, err error) {
	sock.Lock()
	defer sock.Unlock()
	if !sock.checkSeqAck(tcpPack) {
		return nil, fmt.Errorf(
			"seq %d or ack %d invalid recvNext %d sendUnack %d sendNext %d",
			tcpPack.SequenceNumber,
			tcpPack.AckNumber,
			sock.recvNext,
			sock.sendUnack,
			sock.sendNext,
		)
	}
	switch sock.State {
	case tcpip.TcpStateClosed:
		if tcpPack.Flags&uint8(tcpip.TcpSYN) != 0 {
			resp, err = s.handleSyn(sock, tcpPack)
		}
	case tcpip.TcpStateSynReceived:
		if tcpPack.Flags&uint8(tcpip.TcpACK) != 0 {
			resp, err = s.handleFirstAck(sock, tcpPack)
		}
	case tcpip.TcpStateEstablished:
		if tcpPack.Flags&uint8(tcpip.TcpFIN) != 0 {
			resp, err = s.handleFin(sock, tcpPack)
			return
		}
		resp, err = s.handleData(sock, tcpPack)
	case tcpip.TcpStateLastAck:
		if tcpPack.Flags&uint8(tcpip.TcpACK) != 0 {
			s.handleLastAck(sock)
			return nil, nil
		}
	case tcpip.TcpStateCloseWait:
	default:
		return nil, fmt.Errorf("invalid state %d", sock.State)
	}
	return resp, err
}

func (s *ListenSocket) handleSyn(sock *ConnectSocket, tcpPack *tcpip.TcpPack) (resp *tcpip.IPPack, err error) {
	sock.State = tcpip.TcpStateSynReceived
	sock.recvNext = tcpPack.SequenceNumber + 1
	s.synQueue.Store(tcpPack.DstPort, sock)
	s.connectSockets.Store(
		fmt.Sprintf("%s:%d->%s:%d",
			sock.remoteIP,
			sock.remotePort,
			sock.localIP,
			sock.localPort,
		),
		sock,
	)

	var seq uint32
	if s.network.opt.Seq == 0 {
		seq = uint32(rand.Int())
	} else {
		seq = s.network.opt.Seq
	}

	tcpResp := &tcpip.TcpPack{
		PseudoHeader: &tcpip.PseudoHeader{
			SrcIP: sock.remoteIP,
			DstIP: sock.localIP,
		},
		TcpHeader: &tcpip.TcpHeader{
			SrcPort:        sock.localPort,
			DstPort:        sock.remotePort,
			SequenceNumber: seq,
			AckNumber:      tcpPack.SequenceNumber + 1,
			Flags:          uint8(tcpip.TcpSYN | tcpip.TcpACK),
			WindowSize:     s.network.opt.WindowSize,
		},
	}

	ipResp := &tcpip.IPPack{
		IPHeader: &tcpip.IPHeader{
			Version:    4,
			SrcIP:      sock.localIP,
			DstIP:      sock.remoteIP,
			Flags:      2,
			TimeToLive: 64,
			Protocol:   uint8(tcpip.ProtocolTCP),
		},
		Payload: tcpResp,
	}

	sock.sendUnack = tcpResp.SequenceNumber
	sock.sendNext = tcpResp.SequenceNumber + 1

	return ipResp, nil
}

func (s *ListenSocket) handleFirstAck(sock *ConnectSocket, tcpPack *tcpip.TcpPack) (resp *tcpip.IPPack, err error) {
	sock.State = tcpip.TcpStateEstablished
	sock.sendUnack = tcpPack.AckNumber
	fd := s.network.addFile(sock)
	sock.fd = fd
	s.synQueue.Delete(sock.remotePort)
	select {
	case s.acceptQueue <- sock:
	default:
		return nil, fmt.Errorf("accept queue is full, drop connection")
	}
	return nil, nil
}

func (s *ListenSocket) handleData(sock *ConnectSocket, tcpPack *tcpip.TcpPack) (resp *tcpip.IPPack, err error) {
	if tcpPack.Flags&uint8(tcpip.TcpACK) != 0 {
		sock.sendUnack = tcpPack.AckNumber
	}
	if tcpPack.Payload == nil {
		return nil, nil
	}
	data, err := tcpPack.Payload.Encode()
	if err != nil {
		return nil, fmt.Errorf("encode tcp payload failed %w", err)
	}
	if len(data) == 0 {
		return nil, nil
	}
	sock.recvNext = sock.recvNext + uint32(len(data))

	select {
	case sock.readCh <- data:
	default:
		return nil, fmt.Errorf("the reader queue is full, drop the data")
	}

	tcpResp := &tcpip.TcpPack{
		PseudoHeader: &tcpip.PseudoHeader{
			SrcIP: sock.remoteIP,
			DstIP: sock.localIP,
		},
		TcpHeader: &tcpip.TcpHeader{
			SrcPort:        sock.localPort,
			DstPort:        sock.remotePort,
			SequenceNumber: sock.sendNext,
			AckNumber:      sock.recvNext,
			Flags:          uint8(tcpip.TcpACK),
			WindowSize:     s.network.opt.WindowSize,
		},
	}

	ipResp := &tcpip.IPPack{
		IPHeader: &tcpip.IPHeader{
			Version:    4,
			SrcIP:      sock.localIP,
			DstIP:      sock.remoteIP,
			Flags:      2,
			TimeToLive: 64,
			Protocol:   uint8(tcpip.ProtocolTCP),
		},
		Payload: tcpResp,
	}

	return ipResp, nil
}

func (s *ListenSocket) handleFin(sock *ConnectSocket, tcpPack *tcpip.TcpPack) (resp *tcpip.IPPack, err error) {
	sock.recvNext += 1
	sock.State = tcpip.TcpStateCloseWait
	tcpResp := &tcpip.TcpPack{
		PseudoHeader: &tcpip.PseudoHeader{
			SrcIP: sock.remoteIP,
			DstIP: sock.localIP,
		},
		TcpHeader: &tcpip.TcpHeader{
			SrcPort:        sock.localPort,
			DstPort:        sock.remotePort,
			SequenceNumber: sock.sendNext,
			AckNumber:      sock.recvNext,
			Flags:          uint8(tcpip.TcpACK),
			WindowSize:     s.network.opt.WindowSize,
		},
	}

	ipResp := &tcpip.IPPack{
		IPHeader: &tcpip.IPHeader{
			Version:    4,
			SrcIP:      sock.localIP,
			DstIP:      sock.remoteIP,
			Flags:      2,
			TimeToLive: 64,
			Protocol:   uint8(tcpip.ProtocolTCP),
		},
		Payload: tcpResp,
	}
	close(sock.readCh)

	return ipResp, nil
}

func (s *ListenSocket) handleLastAck(sock *ConnectSocket) {
	sock.State = tcpip.TcpStateClosed
	s.connectSockets.Delete(fmt.Sprintf(
		"%s:%d->%s:%d",
		sock.remoteIP,
		sock.remotePort,
		sock.localIP,
		sock.localPort,
	))
}

func (s *ListenSocket) closeConnectSocket(sock *ConnectSocket) (ipResp *tcpip.IPPack, err error) {
	sock.Lock()
	defer sock.Unlock()
	sock.State = tcpip.TcpStateLastAck

	tcpResp := &tcpip.TcpPack{
		PseudoHeader: &tcpip.PseudoHeader{
			SrcIP: sock.remoteIP,
			DstIP: sock.localIP,
		},
		TcpHeader: &tcpip.TcpHeader{
			SrcPort:        sock.localPort,
			DstPort:        sock.remotePort,
			SequenceNumber: sock.sendNext,
			AckNumber:      sock.recvNext,
			Flags:          uint8(tcpip.TcpFIN | tcpip.TcpACK),
			WindowSize:     s.network.opt.WindowSize,
		},
	}

	ipResp = &tcpip.IPPack{
		IPHeader: &tcpip.IPHeader{
			Version:    4,
			SrcIP:      sock.localIP,
			DstIP:      sock.remoteIP,
			Flags:      2,
			TimeToLive: 64,
			Protocol:   uint8(tcpip.ProtocolTCP),
		},
		Payload: tcpResp,
	}

	sock.sendUnack = tcpResp.SequenceNumber
	sock.sendNext = tcpResp.SequenceNumber + 1

	return ipResp, nil
}

func (s *ListenSocket) Read() (data []byte, err error) {
	return nil, nil
}

func (s *ListenSocket) Write(data []byte) (n int, err error) {
	return 0, nil
}

func (s *ListenSocket) Close() error {
	panic("not implemented")
}

type ConnectSocket struct {
	sync.Mutex
	listenSocket *ListenSocket
	remoteIP     net.IP
	localIP      net.IP
	remotePort   uint16
	localPort    uint16
	fd           int
	State        tcpip.TcpState
	readCh       chan []byte

	recvNext  uint32
	sendNext  uint32
	sendUnack uint32

	sendBuffer []byte
}

func NewConnectSocket(
	listenSocket *ListenSocket,
	localIP net.IP,
	localPort uint16,
	remoteIP net.IP,
	remotePort uint16,
) *ConnectSocket {
	return &ConnectSocket{
		listenSocket: listenSocket,
		localIP:      localIP,
		localPort:    localPort,
		remoteIP:     remoteIP,
		remotePort:   remotePort,
		State:        tcpip.TcpStateClosed,
		readCh:       make(chan []byte, 1024),
		sendBuffer:   make([]byte, 1024),
	}
}

func (s *ConnectSocket) Read() (data []byte, err error) {
	s.Lock()
	if s.State == tcpip.TcpStateCloseWait {
		return nil, io.EOF
	}
	s.Unlock()
	data, ok := <-s.readCh
	if !ok {
		return nil, io.EOF
	}
	return data, nil
}

func (s *ConnectSocket) Write(data []byte) (n int, err error) {
	return s.send(data)
}

func (s *ConnectSocket) send(data []byte) (n int, err error) {
	s.Lock()
	defer s.Unlock()
	send, resp, err := s.handleSend(data)
	if err != nil {
		return 0, err
	}
	if resp == nil {
		return 0, nil
	}
	respData, err := resp.Encode()
	if err != nil {
		return 0, err
	}
	s.listenSocket.network.writeCh <- respData
	return send, nil
}

func (s *ConnectSocket) handleSend(data []byte) (send int, resp *tcpip.IPPack, err error) {
	if s.State != tcpip.TcpStateEstablished {
		return 0, nil, fmt.Errorf("connection not established")
	}
	length := len(data)
	if length == 0 {
		return 0, nil, nil
	}

	send = s.cacheSendData(data)
	if send == 0 {
		return 0, nil, nil
	}

	tcpResp := &tcpip.TcpPack{
		PseudoHeader: &tcpip.PseudoHeader{
			SrcIP: s.remoteIP,
			DstIP: s.localIP,
		},
		TcpHeader: &tcpip.TcpHeader{
			SrcPort:        s.localPort,
			DstPort:        s.remotePort,
			SequenceNumber: s.sendNext,
			AckNumber:      s.recvNext,
			Flags:          uint8(tcpip.TcpACK),
			WindowSize:     s.listenSocket.network.opt.WindowSize,
		},
		Payload: tcpip.NewRawPack(data[:send]),
	}

	ipResp := &tcpip.IPPack{
		IPHeader: &tcpip.IPHeader{
			Version:    4,
			SrcIP:      s.localIP,
			DstIP:      s.remoteIP,
			Flags:      2,
			TimeToLive: 64,
			Protocol:   uint8(tcpip.ProtocolTCP),
		},
		Payload: tcpResp,
	}

	s.sendUnack = s.sendNext
	s.sendNext = s.sendNext + uint32(send)

	return send, ipResp, nil
}

func (s *ConnectSocket) Close() error {
	ipResp, err := s.listenSocket.closeConnectSocket(s)
	if err != nil {
		return err
	}

	data, err := ipResp.Encode()
	if err != nil {
		return err
	}

	s.listenSocket.network.writeCh <- data

	return nil
}

func (s *ConnectSocket) checkSeqAck(tcpPack *tcpip.TcpPack) (valid bool) {
	if s.State == tcpip.TcpStateClosed {
		return true
	}
	if tcpPack.SequenceNumber != s.recvNext {
		return false
	}
	if tcpPack.Flags&uint8(tcpip.TcpACK) == 0 {
		return true
	}
	if s.sendUnack == s.sendNext {
		return tcpPack.AckNumber == s.sendNext
	}
	return tcpPack.AckNumber >= s.sendUnack && tcpPack.AckNumber <= s.sendNext
}

func (s *ConnectSocket) cacheSendData(data []byte) int {
	send := 0
	remain := s.sendBufferRemain()
	if len(data) > remain {
		send = remain
	} else {
		send = len(data)
	}
	for i := 0; i < send; i++ {
		s.sendBuffer[(int(s.sendNext)+i)%len(s.sendBuffer)] = data[i]
	}
	return send
}

func (s *ConnectSocket) sendBufferRemain() int {
	// tail - 1 - head + 1
	tail := int(s.sendNext) % len(s.sendBuffer)
	head := int(s.sendUnack) % len(s.sendBuffer)
	if tail >= head {
		return len(s.sendBuffer) - (tail - head)
	}
	return head - tail
}