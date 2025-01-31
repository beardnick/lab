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

type SocketAddr struct {
	SrcIP   string
	SrcPort uint16
	DstIP   string
	DstPort uint16
}

type Socket struct {
	sync.Mutex
	fd int

	localIP    net.IP
	remoteIP   net.IP
	localPort  uint16
	remotePort uint16

	network     *Network
	acceptQueue chan *Socket
	synQueue    sync.Map
	readCh      chan []byte
	writeCh     chan *tcpip.IPPack

	listener   *Socket
	recvNext   uint32
	sendNext   uint32
	sendUnack  uint32
	sendBuffer []byte

	State tcpip.TcpState
}

func NewListenSocket(network *Network) *Socket {
	return &Socket{
		network:     network,
		synQueue:    sync.Map{},
		acceptQueue: make(chan *Socket, network.opt.Backlog),
		readCh:      make(chan []byte),
		writeCh:     make(chan *tcpip.IPPack),
		State:       tcpip.TcpStateListen,
	}
}

func NewConnectSocket(
	listenSocket *Socket,
	localIP net.IP,
	localPort uint16,
	remoteIP net.IP,
	remotePort uint16,
) *Socket {
	return &Socket{
		network:    listenSocket.network,
		listener:   listenSocket,
		localIP:    localIP,
		localPort:  localPort,
		remoteIP:   remoteIP,
		remotePort: remotePort,
		State:      tcpip.TcpStateClosed,
		readCh:     make(chan []byte, 1024),
		writeCh:    make(chan *tcpip.IPPack),
		sendBuffer: make([]byte, 1024),
	}
}

func (s *Socket) Listen(backlog int) (err error) {
	s.acceptQueue = make(chan *Socket, backlog)
	go s.runloop()
	return nil
}

func (s *Socket) Accept() (cfd int, err error) {
	cs := <-s.acceptQueue
	cs.Lock()
	defer cs.Unlock()
	return cs.fd, nil
}

func (s *Socket) Read() (data []byte, err error) {
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

func (s *Socket) Write(data []byte) (n int, err error) {
	return s.send(data)
}

func (s *Socket) runloop() {
	for data := range s.writeCh {
		tcpPack := data.Payload.(*tcpip.TcpPack)
		s.handle(data, tcpPack)
	}
}

func (s *Socket) handle(ipPack *tcpip.IPPack, tcpPack *tcpip.TcpPack) {
	s.Lock()
	defer s.Unlock()
	if s.network.opt.Debug {
		log.Printf(
			"handle %s:%d => %s:%d %s",
			ipPack.SrcIP,
			tcpPack.SrcPort,
			ipPack.DstIP,
			tcpPack.DstPort,
			s.State.String(),
		)
	}
	resp, err := s.handleState(ipPack, tcpPack)
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

func (s *Socket) handleState(ipPack *tcpip.IPPack, tcpPack *tcpip.TcpPack) (resp *tcpip.IPPack, err error) {
	switch s.State {
	case tcpip.TcpStateListen:
		s.handleNewSocket(ipPack, tcpPack)
	default:
		if !s.checkSeqAck(tcpPack) {
			return nil, fmt.Errorf(
				"seq %d or ack %d invalid recvNext %d sendUnack %d sendNext %d",
				tcpPack.SequenceNumber,
				tcpPack.AckNumber,
				s.recvNext,
				s.sendUnack,
				s.sendNext,
			)
		}
		switch s.State {
		case tcpip.TcpStateClosed:
			if tcpPack.Flags&uint8(tcpip.TcpSYN) != 0 {
				resp, err = s.handleSyn(tcpPack)
			}
		case tcpip.TcpStateSynReceived:
			if tcpPack.Flags&uint8(tcpip.TcpACK) != 0 {
				resp, err = s.handleFirstAck(tcpPack)
			}
		case tcpip.TcpStateEstablished:
			if tcpPack.Flags&uint8(tcpip.TcpFIN) != 0 {
				resp, err = s.handleFin()
				return
			}
			resp, err = s.handleData(tcpPack)
		case tcpip.TcpStateLastAck:
			if tcpPack.Flags&uint8(tcpip.TcpACK) != 0 {
				s.handleLastAck()
				return nil, nil
			}
		case tcpip.TcpStateCloseWait:
		case tcpip.TcpStateFinWait1:
			resp, err = s.handleFinWait1(tcpPack)
		case tcpip.TcpStateFinWait2:
			resp, err = s.handleFinWait2Fin(tcpPack)
		default:
			return nil, fmt.Errorf("invalid state %d", s.State)
		}
	}
	return resp, err
}

func (s *Socket) handleNewSocket(ipPack *tcpip.IPPack, tcpPack *tcpip.TcpPack) {
	value, ok := s.synQueue.Load(tcpPack.DstPort)
	var sock *Socket
	if ok {
		sock = value.(*Socket)
	} else {
		sock = NewConnectSocket(
			s,
			ipPack.DstIP,
			tcpPack.DstPort,
			ipPack.SrcIP,
			tcpPack.SrcPort,
		)
	}
	sock.handle(ipPack, tcpPack)
}

func (s *Socket) handleSyn(tcpPack *tcpip.TcpPack) (resp *tcpip.IPPack, err error) {
	s.State = tcpip.TcpStateSynReceived
	s.recvNext = tcpPack.SequenceNumber + 1
	s.listener.synQueue.Store(tcpPack.DstPort, s)

	var seq uint32
	if s.network.opt.Seq == 0 {
		seq = uint32(rand.Int())
	} else {
		seq = s.network.opt.Seq
	}

	tcpResp := &tcpip.TcpPack{
		PseudoHeader: &tcpip.PseudoHeader{
			SrcIP: s.remoteIP,
			DstIP: s.localIP,
		},
		TcpHeader: &tcpip.TcpHeader{
			SrcPort:        s.localPort,
			DstPort:        s.remotePort,
			SequenceNumber: seq,
			AckNumber:      tcpPack.SequenceNumber + 1,
			Flags:          uint8(tcpip.TcpSYN | tcpip.TcpACK),
			WindowSize:     s.network.opt.WindowSize,
		},
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

	s.sendUnack = tcpResp.SequenceNumber
	s.sendNext = tcpResp.SequenceNumber + 1

	return ipResp, nil
}

func (s *Socket) handleFirstAck(tcpPack *tcpip.TcpPack) (resp *tcpip.IPPack, err error) {
	s.State = tcpip.TcpStateEstablished
	s.sendUnack = tcpPack.AckNumber
	s.synQueue.Delete(s.remotePort)
	select {
	case s.listener.acceptQueue <- s:
	default:
		return nil, fmt.Errorf("accept queue is full, drop connection")
	}

	s.network.addSocket(s)
	s.network.bindSocket(SocketAddr{
		SrcIP:   s.remoteIP.String(),
		SrcPort: s.remotePort,
		DstIP:   s.localIP.String(),
		DstPort: s.localPort,
	}, s.fd)
	go s.runloop()
	return nil, nil
}

func (s *Socket) handleData(tcpPack *tcpip.TcpPack) (resp *tcpip.IPPack, err error) {
	if tcpPack.Flags&uint8(tcpip.TcpACK) != 0 {
		s.sendUnack = tcpPack.AckNumber
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
	s.recvNext = s.recvNext + uint32(len(data))

	select {
	case s.readCh <- data:
	default:
		return nil, fmt.Errorf("the reader queue is full, drop the data")
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
			WindowSize:     s.network.opt.WindowSize,
		},
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

	return ipResp, nil
}

func (s *Socket) handleFin() (resp *tcpip.IPPack, err error) {
	s.recvNext += 1
	s.State = tcpip.TcpStateCloseWait
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
			WindowSize:     s.network.opt.WindowSize,
		},
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
	close(s.readCh)

	return ipResp, nil
}

func (s *Socket) handleLastAck() {
	s.State = tcpip.TcpStateClosed
	s.network.removeSocket(s.fd)
	s.network.unbindSocket(SocketAddr{
		SrcIP:   s.remoteIP.String(),
		SrcPort: s.remotePort,
		DstIP:   s.localIP.String(),
		DstPort: s.localPort,
	})
}

func (s *Socket) handleFinWait1(
	tcpPack *tcpip.TcpPack,
) (resp *tcpip.IPPack, err error) {
	if tcpPack.Flags&uint8(tcpip.TcpACK) != 0 {
		return nil, fmt.Errorf("invalid packet, ack flag isn't set %s", tcpip.InspectFlags(tcpPack.Flags))
	}
	if tcpPack.AckNumber >= s.sendNext-1 {
		s.State = tcpip.TcpStateFinWait2
	}
	return s.handleFinWait2Fin(tcpPack)
}

func (s *Socket) handleFinWait2Fin(tcpPack *tcpip.TcpPack) (resp *tcpip.IPPack, err error) {
	if tcpPack.Flags&uint8(tcpip.TcpFIN) == 0 {
		return s.handleData(tcpPack)
	}

	s.sendUnack = tcpPack.AckNumber
	data, err := tcpPack.Payload.Encode()
	if err != nil {
		return nil, fmt.Errorf("encode tcp payload failed %w", err)
	}
	if len(data) == 0 {
		return nil, nil
	}
	// +1 for FIN
	s.recvNext = s.recvNext + uint32(len(data)) + 1

	select {
	case s.readCh <- data:
	default:
		return nil, fmt.Errorf("the reader queue is full, drop the data")
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
			WindowSize:     s.network.opt.WindowSize,
		},
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

	s.State = tcpip.TcpStateClosed
	s.network.removeSocket(s.fd)
	s.network.unbindSocket(SocketAddr{
		SrcIP:   s.remoteIP.String(),
		SrcPort: s.remotePort,
		DstIP:   s.localIP.String(),
		DstPort: s.localPort,
	})
	return ipResp, nil
}

func (s *Socket) send(data []byte) (n int, err error) {
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
	s.network.writeCh <- respData
	return send, nil
}

func (s *Socket) Close() error {
	var (
		ipResp *tcpip.IPPack
		err    error
	)
	s.Lock()
	defer s.Unlock()
	if s.State == tcpip.TcpStateCloseWait {
		ipResp = s.passiveCloseSocket()
	} else if s.State == tcpip.TcpStateEstablished {
		ipResp = s.activeCloseSocket()
	} else {
		return fmt.Errorf("wrong state %s", s.State.String())
	}

	data, err := ipResp.Encode()
	if err != nil {
		return err
	}

	s.network.writeCh <- data

	return nil
}

func (s *Socket) passiveCloseSocket() (ipResp *tcpip.IPPack) {
	s.State = tcpip.TcpStateLastAck

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
			Flags:          uint8(tcpip.TcpFIN | tcpip.TcpACK),
			WindowSize:     s.network.opt.WindowSize,
		},
	}

	ipResp = &tcpip.IPPack{
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

	s.sendUnack = tcpResp.SequenceNumber
	s.sendNext = tcpResp.SequenceNumber + 1

	return ipResp
}

func (s *Socket) activeCloseSocket() (ipResp *tcpip.IPPack) {
	s.State = tcpip.TcpStateFinWait1

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
			Flags:          uint8(tcpip.TcpFIN | tcpip.TcpACK),
			WindowSize:     s.network.opt.WindowSize,
		},
	}

	ipResp = &tcpip.IPPack{
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

	s.sendUnack = tcpResp.SequenceNumber
	s.sendNext = tcpResp.SequenceNumber + 1

	return ipResp
}

func (s *Socket) handleSend(data []byte) (send int, resp *tcpip.IPPack, err error) {
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
			WindowSize:     s.network.opt.WindowSize,
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

func (s *Socket) checkSeqAck(tcpPack *tcpip.TcpPack) (valid bool) {
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

func (s *Socket) cacheSendData(data []byte) int {
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

func (s *Socket) sendBufferRemain() int {
	// tail - 1 - head + 1
	tail := int(s.sendNext) % len(s.sendBuffer)
	head := int(s.sendUnack) % len(s.sendBuffer)
	if tail >= head {
		return len(s.sendBuffer) - (tail - head)
	}
	return head - tail
}
