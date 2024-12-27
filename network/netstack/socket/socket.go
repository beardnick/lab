package socket

import (
	"log"
	"math/rand"
	"net"
	"netstack/tcpip"
)

// nic read packet -> <srcip,srcport,dstip,dstport> -> listener exists -> listener.handle(data)
// listener data -> haven't connected -> handshake -> new socket -> accept -> socket
// send(socket,data) -> encode data -> nic write packet
// nic_layer <-channel-> listen_socket_layer <-channel-> connect_socket_layer <-read/write-> api_layer

type ListenSocket struct {
	network        *Network
	Fd             int
	ip             net.IP
	port           int
	listening      bool
	acceptQueue    chan *ConnectSocket
	synQueue       map[uint16]*ConnectSocket
	connectSockets map[uint16]*ConnectSocket
	readCh         chan []byte
	writeCh        chan *tcpip.IPPack
}

func NewListenSocket(network *Network, fd int) *ListenSocket {
	return &ListenSocket{
		network:        network,
		Fd:             fd,
		synQueue:       make(map[uint16]*ConnectSocket),
		connectSockets: make(map[uint16]*ConnectSocket),
		readCh:         make(chan []byte),
		writeCh:        make(chan *tcpip.IPPack),
	}
}

func (s *ListenSocket) getConnectSocket(port uint16) (cs *ConnectSocket, ok bool) {
	cs, ok = s.connectSockets[port]
	return
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
	sock, ok := s.getConnectSocket(tcpPack.DstPort)
	if !ok {
		sock = NewConnectSocket(tcpPack.DstPort)
	}
	if !sock.checkSeqAck(tcpPack) {
		log.Printf("seq %d or ack %d invalid", tcpPack.SequenceNumber, tcpPack.AckNumber)
		return
	}
	switch sock.State {
	case tcpip.TcpStateClosed:
		if tcpPack.Flags&uint8(tcpip.TcpSYN) != 0 {
			s.handleSyn(sock, ipPack, tcpPack)
		}
	case tcpip.TcpStateSynReceived:
		if tcpPack.Flags&uint8(tcpip.TcpACK) != 0 {
			s.handleFirstAck(sock)
		}

	case tcpip.TcpStateEstablished:
		if tcpPack.Flags&uint8(tcpip.TcpFIN) != 0 {
			s.handleFin(sock, tcpPack)
			return
		}
		s.handleData(sock, tcpPack)
	default:
	}
}

func (s *ListenSocket) handleSyn(sock *ConnectSocket, ipPack *tcpip.IPPack, tcpPack *tcpip.TcpPack) {
	sock.State = tcpip.TcpStateSynReceived
	sock.recvNext = tcpPack.SequenceNumber + 1
	s.synQueue[tcpPack.DstPort] = sock
	s.connectSockets[tcpPack.DstPort] = sock

	tcpResp := &tcpip.TcpPack{
		PseudoHeader: &tcpip.PseudoHeader{
			SrcIP: ipPack.SrcIP,
			DstIP: ipPack.DstIP,
		},
		TcpHeader: &tcpip.TcpHeader{
			SrcPort:        tcpPack.DstPort,
			DstPort:        tcpPack.SrcPort,
			SequenceNumber: uint32(rand.Int()),
			AckNumber:      tcpPack.SequenceNumber + 1,
			Flags:          uint8(tcpip.TcpSYN | tcpip.TcpACK),
			WindowSize:     s.network.opt.WindowSize,
		},
	}

	ipResp := &tcpip.IPPack{
		IPHeader: &tcpip.IPHeader{
			Version:    4,
			SrcIP:      ipPack.DstIP,
			DstIP:      ipPack.SrcIP,
			Flags:      2,
			TimeToLive: 64,
			Protocol:   uint8(tcpip.ProtocolTCP),
		},
		Payload: tcpResp,
	}

	data, err := ipResp.Encode()
	if err != nil {
		log.Println("encode tcp response failed", err)
		return
	}

	s.network.writeCh <- data

	sock.sendUnack = tcpResp.SequenceNumber
	sock.sendNext = tcpResp.SequenceNumber + 1
}

func (s *ListenSocket) handleFirstAck(sock *ConnectSocket) {
	sock.State = tcpip.TcpStateEstablished
	delete(s.synQueue, sock.port)
	select {
	case s.acceptQueue <- sock:
	default:
		log.Println("accept queue is full, drop connection")
	}
}

func (s *ListenSocket) handleData(sock *ConnectSocket, tcpPack *tcpip.TcpPack) {
}

func (s *ListenSocket) handleFin(sock *ConnectSocket, tcpPack *tcpip.TcpPack) {
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
	fd      int
	port    uint16
	State   tcpip.TcpState
	readCh  chan []byte
	writeCh chan []byte

	recvNext  uint32
	sendNext  uint32
	sendUnack uint32
}

func NewConnectSocket(port uint16) *ConnectSocket {
	return &ConnectSocket{
		port:    port,
		State:   tcpip.TcpStateClosed,
		readCh:  make(chan []byte),
		writeCh: make(chan []byte),
	}
}

func (s *ConnectSocket) Read() (data []byte, err error) {
	data = <-s.readCh
	return
}

func (s *ConnectSocket) Write(data []byte) (n int, err error) {
	s.writeCh <- data
	return len(data), nil
}

func (s *ConnectSocket) Close() error {
	panic("not implemented")
}

func (s *ConnectSocket) checkSeqAck(tcpPack *tcpip.TcpPack) (valid bool) {
	if s.State == tcpip.TcpStateClosed {
		return true
	}
	return (tcpPack.SequenceNumber != s.recvNext) ||
		(tcpPack.AckNumber < s.sendUnack || tcpPack.AckNumber >= s.sendNext)
}
