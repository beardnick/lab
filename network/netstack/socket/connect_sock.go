package socket

import (
	"fmt"
	"io"
	"net"
	"netstack/tcpip"
	"sync"
)

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

func (s *ConnectSocket) Close() error {
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

func (s *ConnectSocket) passiveCloseSocket() (ipResp *tcpip.IPPack) {
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
			WindowSize:     s.listenSocket.network.opt.WindowSize,
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

func (s *ConnectSocket) activeCloseSocket() (ipResp *tcpip.IPPack) {
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
			WindowSize:     s.listenSocket.network.opt.WindowSize,
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
