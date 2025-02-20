package socket

import (
	"fmt"
	"io"
	"log"
	"math/rand"
	"netstack/tcpip"
	"sync"
	"time"
)

type SocketAddr struct {
	LocalIP    string
	RemoteIP   string
	LocalPort  uint16
	RemotePort uint16
}

type TcpSocket struct {
	sync.Mutex
	SocketAddr
	State tcpip.TcpState

	fd int

	network  *Network
	listener *TcpSocket

	acceptQueue chan *TcpSocket
	synQueue    sync.Map

	readCh  chan []byte
	writeCh chan *tcpip.IPPack

	recvNext   uint32
	sendNext   uint32
	sendUnack  uint32
	sendBuffer []byte
}

func NewSocket(network *Network) *TcpSocket {
	return &TcpSocket{
		network: network,
	}
}

func InitListenSocket(sock *TcpSocket) {
	sock.Lock()
	defer sock.Unlock()
	sock.synQueue = sync.Map{}
	sock.readCh = make(chan []byte)
	sock.writeCh = make(chan *tcpip.IPPack)
	sock.State = tcpip.TcpStateListen
}

func InitConnectSocket(
	sock *TcpSocket,
	listenSocket *TcpSocket,
	addr SocketAddr,
) {
	sock.Lock()
	defer sock.Unlock()
	sock.listener = listenSocket
	sock.SocketAddr = addr
	sock.readCh = make(chan []byte, 1024)
	sock.writeCh = make(chan *tcpip.IPPack)
	sock.sendBuffer = make([]byte, 1024)
	sock.State = tcpip.TcpStateClosed
}

func (s *TcpSocket) Listen(backlog uint) (err error) {
	s.acceptQueue = make(chan *TcpSocket, min(backlog, s.network.opt.SoMaxConn))
	go s.runloop()
	return nil
}

func (s *TcpSocket) Accept() (cfd int, addr SocketAddr, err error) {
	cs := <-s.acceptQueue
	cs.Lock()
	defer cs.Unlock()
	return cs.fd, cs.SocketAddr, nil
}

func (s *TcpSocket) AcceptWithTimeout(timeout time.Duration) (cfd int, err error) {
	select {
	case cs := <-s.acceptQueue:
		cs.Lock()
		defer cs.Unlock()
		return cs.fd, nil
	case <-time.After(timeout):
		return 0, nil
	}
}

func (s *TcpSocket) Connect() (err error) {
	return s.connect()
}

func (s *TcpSocket) Read() (data []byte, err error) {
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

func (s *TcpSocket) Write(data []byte) (n int, err error) {
	return s.send(data)
}

func (s *TcpSocket) runloop() {
	for data := range s.writeCh {
		tcpPack := data.Payload.(*tcpip.TcpPack)
		s.handle(data, tcpPack)
	}
}

func (s *TcpSocket) handle(ipPack *tcpip.IPPack, tcpPack *tcpip.TcpPack) {
	s.Lock()
	defer s.Unlock()
	if s.network.opt.Debug {
		log.Printf(
			"before handle %s:%d => %s:%d %s",
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
	log.Printf(
		"after handle %s:%d => %s:%d %s",
		ipPack.SrcIP,
		tcpPack.SrcPort,
		ipPack.DstIP,
		tcpPack.DstPort,
		s.State.String(),
	)
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

func (s *TcpSocket) handleState(ipPack *tcpip.IPPack, tcpPack *tcpip.TcpPack) (resp *tcpip.IPPack, err error) {
	switch s.State {
	case tcpip.TcpStateListen:
		s.handleNewSocket(ipPack, tcpPack)
	case tcpip.TcpStateSynSent:
		resp, err = s.handleSynResp(tcpPack)
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

func (s *TcpSocket) handleNewSocket(ipPack *tcpip.IPPack, tcpPack *tcpip.TcpPack) {
	value, ok := s.synQueue.Load(tcpPack.SrcPort)
	var sock *TcpSocket
	if ok {
		sock = value.(*TcpSocket)
	} else {
		sock = NewSocket(s.network)
		InitConnectSocket(
			sock,
			s,
			SocketAddr{
				LocalIP:    ipPack.DstIP.String(),
				LocalPort:  tcpPack.DstPort,
				RemoteIP:   ipPack.SrcIP.String(),
				RemotePort: tcpPack.SrcPort,
			},
		)
	}
	sock.handle(ipPack, tcpPack)
}

func (s *TcpSocket) handleSyn(tcpPack *tcpip.TcpPack) (resp *tcpip.IPPack, err error) {
	s.State = tcpip.TcpStateSynReceived
	s.recvNext = tcpPack.SequenceNumber + 1
	s.listener.synQueue.Store(tcpPack.SrcPort, s)

	var seq uint32
	if s.network.opt.Seq == 0 {
		seq = uint32(rand.Int())
	} else {
		seq = s.network.opt.Seq
	}

	s.sendUnack = seq
	s.sendNext = seq

	ipResp, _, err := NewPacketBuilder(s.network.opt).
		SetAddr(s.SocketAddr).
		SetSeq(s.sendNext).
		SetAck(s.recvNext).
		SetFlags(tcpip.TcpSYN | tcpip.TcpACK).
		Build()
	if err != nil {
		return nil, err
	}

	s.sendNext++

	return ipResp, nil
}

func (s *TcpSocket) handleSynResp(tcpPack *tcpip.TcpPack) (resp *tcpip.IPPack, err error) {
	if tcpPack.Flags&uint8(tcpip.TcpACK) == 0 || tcpPack.Flags&uint8(tcpip.TcpSYN) == 0 {
		// syn + ack expected
		// just drop the packet
		return nil,
			fmt.Errorf(
				"invalid packet, expected syn and ack, but get %s",
				tcpip.InspectFlags(tcpPack.Flags),
			)
	}
	if tcpPack.AckNumber != s.sendUnack+1 {
		return nil,
			fmt.Errorf(
				"invalid packet, expected ack %d, but get %d",
				s.sendUnack,
				tcpPack.AckNumber,
			)
	}

	s.State = tcpip.TcpStateEstablished
	s.recvNext = tcpPack.SequenceNumber + 1

	ipResp, _, err := NewPacketBuilder(s.network.opt).
		SetAddr(s.SocketAddr).
		SetSeq(s.sendNext).
		SetAck(s.recvNext).
		SetFlags(tcpip.TcpACK).
		Build()
	if err != nil {
		return nil, err
	}
	s.sendUnack++

	select {
	case s.listener.acceptQueue <- s:
	default:
		return nil, fmt.Errorf("accept queue is full, drop connection")
	}

	return ipResp, nil
}

func (s *TcpSocket) handleFirstAck(tcpPack *tcpip.TcpPack) (resp *tcpip.IPPack, err error) {
	s.State = tcpip.TcpStateEstablished
	s.sendUnack = tcpPack.AckNumber
	s.synQueue.Delete(s.RemotePort)
	select {
	case s.listener.acceptQueue <- s:
	default:
		return nil, fmt.Errorf("accept queue is full, drop connection")
	}

	s.network.addSocket(s)
	s.network.bindSocket(s.SocketAddr, s.fd)
	go s.runloop()
	return nil, nil
}

func (s *TcpSocket) handleData(tcpPack *tcpip.TcpPack) (resp *tcpip.IPPack, err error) {
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

	ipResp, _, err := NewPacketBuilder(s.network.opt).
		SetAddr(s.SocketAddr).
		SetSeq(s.sendNext).
		SetAck(s.recvNext).
		SetFlags(tcpip.TcpACK).
		Build()
	if err != nil {
		return nil, err
	}

	return ipResp, nil
}

func (s *TcpSocket) handleFin() (resp *tcpip.IPPack, err error) {
	s.recvNext += 1
	s.State = tcpip.TcpStateCloseWait
	ipResp, _, err := NewPacketBuilder(s.network.opt).
		SetAddr(s.SocketAddr).
		SetSeq(s.sendNext).
		SetAck(s.recvNext).
		SetFlags(tcpip.TcpACK).
		Build()
	if err != nil {
		return nil, err
	}

	close(s.readCh)

	return ipResp, nil
}

func (s *TcpSocket) handleLastAck() {
	s.State = tcpip.TcpStateClosed
	s.network.removeSocket(s.fd)
	s.network.unbindSocket(s.SocketAddr)
}

func (s *TcpSocket) handleFinWait1(
	tcpPack *tcpip.TcpPack,
) (resp *tcpip.IPPack, err error) {
	if tcpPack.Flags&uint8(tcpip.TcpACK) == 0 {
		return nil, fmt.Errorf("invalid packet, ack flag isn't set %s", tcpip.InspectFlags(tcpPack.Flags))
	}
	if tcpPack.AckNumber >= s.sendNext-1 {
		s.State = tcpip.TcpStateFinWait2
	}
	return s.handleFinWait2Fin(tcpPack)
}

func (s *TcpSocket) handleFinWait2Fin(tcpPack *tcpip.TcpPack) (resp *tcpip.IPPack, err error) {
	if tcpPack.Flags&uint8(tcpip.TcpFIN) == 0 {
		return s.handleData(tcpPack)
	}

	s.sendUnack = tcpPack.AckNumber
	data, err := tcpPack.Payload.Encode()
	if err != nil {
		return nil, fmt.Errorf("encode tcp payload failed %w", err)
	}

	// +1 for FIN
	s.recvNext = s.recvNext + uint32(len(data)) + 1

	if len(data) > 0 {
		select {
		case s.readCh <- data:
		default:
			return nil, fmt.Errorf("the reader queue is full, drop the data")
		}
	}

	ipResp, _, err := NewPacketBuilder(s.network.opt).
		SetAddr(s.SocketAddr).
		SetSeq(s.sendNext).
		SetAck(s.recvNext).
		SetFlags(tcpip.TcpACK).
		Build()
	if err != nil {
		return nil, err
	}

	s.State = tcpip.TcpStateClosed
	s.network.removeSocket(s.fd)
	s.network.unbindSocket(s.SocketAddr)
	close(s.readCh)
	return ipResp, nil
}

func (s *TcpSocket) send(data []byte) (n int, err error) {
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

func (s *TcpSocket) Close() error {
	var (
		ipResp *tcpip.IPPack
		err    error
	)
	s.Lock()
	defer s.Unlock()
	if s.State == tcpip.TcpStateCloseWait {
		ipResp, err = s.passiveCloseSocket()
	} else if s.State == tcpip.TcpStateEstablished {
		ipResp, err = s.activeCloseSocket()
	} else {
		return fmt.Errorf("wrong state %s", s.State.String())
	}
	if err != nil {
		return err
	}

	data, err := ipResp.Encode()
	if err != nil {
		return err
	}

	s.network.writeCh <- data

	return nil
}

func (s *TcpSocket) passiveCloseSocket() (ipResp *tcpip.IPPack, err error) {
	s.State = tcpip.TcpStateLastAck

	ipResp, tcpResp, err := NewPacketBuilder(s.network.opt).
		SetAddr(s.SocketAddr).
		SetSeq(s.sendNext).
		SetAck(s.recvNext).
		SetFlags(tcpip.TcpFIN | tcpip.TcpACK).
		Build()
	if err != nil {
		return nil, err
	}

	s.sendUnack = tcpResp.SequenceNumber
	s.sendNext = tcpResp.SequenceNumber + 1

	return ipResp, nil
}

func (s *TcpSocket) activeCloseSocket() (ipResp *tcpip.IPPack, err error) {
	s.State = tcpip.TcpStateFinWait1

	ipResp, tcpResp, err := NewPacketBuilder(s.network.opt).
		SetAddr(s.SocketAddr).
		SetSeq(s.sendNext).
		SetAck(s.recvNext).
		SetFlags(tcpip.TcpFIN | tcpip.TcpACK).
		Build()
	if err != nil {
		return nil, err
	}

	s.sendUnack = tcpResp.SequenceNumber
	s.sendNext = tcpResp.SequenceNumber + 1

	return ipResp, nil
}

func (s *TcpSocket) handleSend(data []byte) (send int, resp *tcpip.IPPack, err error) {
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

	ipResp, _, err := NewPacketBuilder(s.network.opt).
		SetAddr(s.SocketAddr).
		SetSeq(s.sendNext).
		SetAck(s.recvNext).
		SetFlags(tcpip.TcpACK).
		SetPayload(tcpip.NewRawPack(data[:send])).
		Build()
	if err != nil {
		return 0, nil, err
	}

	s.sendUnack = s.sendNext
	s.sendNext = s.sendNext + uint32(send)

	return send, ipResp, nil
}

func (s *TcpSocket) checkSeqAck(tcpPack *tcpip.TcpPack) (valid bool) {
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
	return tcpPack.AckNumber > s.sendUnack && tcpPack.AckNumber <= s.sendNext
}

func (s *TcpSocket) cacheSendData(data []byte) int {
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

func (s *TcpSocket) sendBufferRemain() int {
	// tail - 1 - head + 1
	tail := int(s.sendNext) % len(s.sendBuffer)
	head := int(s.sendUnack) % len(s.sendBuffer)
	if tail >= head {
		return len(s.sendBuffer) - (tail - head)
	}
	return head - tail
}

func (s *TcpSocket) connect() (err error) {
	err = s.Listen(1)
	if err != nil {
		return err
	}
	ipResp, err := s.activeConnect()
	if err != nil {
		return err
	}
	data, err := ipResp.Encode()
	if err != nil {
		return err
	}
	s.network.writeCh <- data
	<-s.acceptQueue
	return nil
}

func (s *TcpSocket) activeConnect() (ipResp *tcpip.IPPack, err error) {
	s.State = tcpip.TcpStateSynSent
	var seq uint32
	if s.network.opt.Seq == 0 {
		seq = uint32(rand.Int())
	} else {
		seq = s.network.opt.Seq
	}

	s.sendUnack = seq
	s.sendNext = seq

	ipResp, _, err = NewPacketBuilder(s.network.opt).
		SetAddr(s.SocketAddr).
		SetSeq(s.sendNext).
		SetFlags(tcpip.TcpSYN).
		Build()
	if err != nil {
		return nil, err
	}

	s.sendNext++

	s.listener = s

	return ipResp, nil
}
