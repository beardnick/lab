package socket

import (
	"fmt"
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

func (s *ListenSocket) Read() (data []byte, err error) {
	return nil, nil
}

func (s *ListenSocket) Write(data []byte) (n int, err error) {
	return 0, nil
}

func (s *ListenSocket) Close() error {
	panic("not implemented")
}

func (s *ListenSocket) getConnectSocket(key string) (cs *ConnectSocket, ok bool) {
	value, ok := s.connectSockets.Load(key)
	if !ok {
		return nil, false
	}
	cs = value.(*ConnectSocket)
	return cs, true
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
	case tcpip.TcpStateFinWait1:
		resp, err = s.handleFinWait1(sock, tcpPack)
	case tcpip.TcpStateFinWait2:
		resp, err = s.handleFinWait2Fin(sock, tcpPack)
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

func (s *ListenSocket) handleFinWait1(
	sock *ConnectSocket,
	tcpPack *tcpip.TcpPack,
) (resp *tcpip.IPPack, err error) {
	if tcpPack.Flags&uint8(tcpip.TcpACK) != 0 {
		return nil, fmt.Errorf("invalid packet, ack flag isn't set %s", tcpip.InspectFlags(tcpPack.Flags))
	}
	if tcpPack.AckNumber >= sock.sendNext-1 {
		sock.State = tcpip.TcpStateFinWait2
	}
	return s.handleFinWait2Fin(sock, tcpPack)
}

func (s *ListenSocket) handleFinWait2Fin(sock *ConnectSocket, tcpPack *tcpip.TcpPack) (resp *tcpip.IPPack, err error) {
	if tcpPack.Flags&uint8(tcpip.TcpFIN) == 0 {
		return s.handleData(sock, tcpPack)
	}

	sock.sendUnack = tcpPack.AckNumber
	data, err := tcpPack.Payload.Encode()
	if err != nil {
		return nil, fmt.Errorf("encode tcp payload failed %w", err)
	}
	if len(data) == 0 {
		return nil, nil
	}
	// +1 for FIN
	sock.recvNext = sock.recvNext + uint32(len(data)) + 1

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

	sock.State = tcpip.TcpStateClosed
	s.connectSockets.Delete(fmt.Sprintf(
		"%s:%d->%s:%d",
		sock.remoteIP,
		sock.remotePort,
		sock.localIP,
		sock.localPort,
	))

	return ipResp, nil
}
