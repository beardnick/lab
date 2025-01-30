package socket

import (
	"fmt"
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

type ListenSocket struct {
	sync.Mutex
	network        *Network
	Fd             int
	acceptQueue    chan *ListenSocket
	synQueue       sync.Map
	connectSockets sync.Map
	readCh         chan []byte
	writeCh        chan *tcpip.IPPack

	listener   *ListenSocket
	State      tcpip.TcpState
	recvNext   uint32
	sendNext   uint32
	sendUnack  uint32
	sendBuffer []byte
	remoteIP   net.IP
	localIP    net.IP
	remotePort uint16
	localPort  uint16
}

func NewListenSocket(network *Network) *ListenSocket {
	return &ListenSocket{
		network:        network,
		synQueue:       sync.Map{},
		connectSockets: sync.Map{},
		acceptQueue:    make(chan *ListenSocket, network.opt.Backlog),
		readCh:         make(chan []byte),
		writeCh:        make(chan *tcpip.IPPack),
		State:          tcpip.TcpStateListen,
	}
}

func NewConnectSocket(
	listenSocket *ListenSocket,
	localIP net.IP,
	localPort uint16,
	remoteIP net.IP,
	remotePort uint16,
) *ListenSocket {
	return &ListenSocket{
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

func (s *ListenSocket) Listen(backlog int) (err error) {
	s.acceptQueue = make(chan *ListenSocket, backlog)
	go s.runloop()
	return nil
}

func (s *ListenSocket) Accept() (cfd int, err error) {
	cs := <-s.acceptQueue
	return cs.Fd, nil
}

func (s *ListenSocket) runloop() {
	for data := range s.writeCh {
		tcpPack := data.Payload.(*tcpip.TcpPack)
		s.handle(data, tcpPack)
	}
}

func (s *ListenSocket) handle(ipPack *tcpip.IPPack, tcpPack *tcpip.TcpPack) {
	s.Lock()
	defer s.Unlock()
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

func (s *ListenSocket) handleState(ipPack *tcpip.IPPack, tcpPack *tcpip.TcpPack) (resp *tcpip.IPPack, err error) {
	switch s.State {
	case tcpip.TcpStateListen:
		s.handleNewPacket(ipPack, tcpPack)
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
				resp, err = s.handleFin(tcpPack)
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

func (s *ListenSocket) handleNewPacket(ipPack *tcpip.IPPack, tcpPack *tcpip.TcpPack) {
	sock := NewConnectSocket(
		s,
		ipPack.DstIP,
		tcpPack.DstPort,
		ipPack.SrcIP,
		tcpPack.SrcPort,
	)
	go sock.runloop()
	sock.handle(ipPack, tcpPack)
}

func (s *ListenSocket) handleSyn(tcpPack *tcpip.TcpPack) (resp *tcpip.IPPack, err error) {
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

	s.network.addFile(s)
	s.network.registerSocket(s.Fd, SocketAddr{
		SrcIP:   s.remoteIP.String(),
		SrcPort: s.remotePort,
		DstIP:   s.localIP.String(),
		DstPort: s.localPort,
	})

	return ipResp, nil
}

func (s *ListenSocket) handleFirstAck(tcpPack *tcpip.TcpPack) (resp *tcpip.IPPack, err error) {
	s.State = tcpip.TcpStateEstablished
	s.sendUnack = tcpPack.AckNumber
	s.synQueue.Delete(s.remotePort)
	select {
	case s.listener.acceptQueue <- s:
	default:
		return nil, fmt.Errorf("accept queue is full, drop connection")
	}
	return nil, nil
}

func (s *ListenSocket) handleData(tcpPack *tcpip.TcpPack) (resp *tcpip.IPPack, err error) {
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

func (s *ListenSocket) handleFin(tcpPack *tcpip.TcpPack) (resp *tcpip.IPPack, err error) {
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

func (s *ListenSocket) handleLastAck() {
	s.State = tcpip.TcpStateClosed
	s.connectSockets.Delete(fmt.Sprintf(
		"%s:%d->%s:%d",
		s.remoteIP,
		s.remotePort,
		s.localIP,
		s.localPort,
	))
}

func (s *ListenSocket) handleFinWait1(
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

func (s *ListenSocket) handleFinWait2Fin(tcpPack *tcpip.TcpPack) (resp *tcpip.IPPack, err error) {
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
	s.connectSockets.Delete(fmt.Sprintf(
		"%s:%d->%s:%d",
		s.remoteIP,
		s.remotePort,
		s.localIP,
		s.localPort,
	))

	return ipResp, nil
}
