package socket

import (
	"context"
	"net"
	"netstack/tcpip"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestSocketServer(t *testing.T) {
	// setup
	// {{{
	args := struct {
		srcIp     string
		srcPort   uint16
		dstIp     string
		dstPort   uint16
		clientSeq uint32
		serverSeq uint32
	}{
		srcIp:     "127.0.0.1",
		srcPort:   52494,
		dstIp:     "127.0.0.1",
		dstPort:   12345,
		clientSeq: 4056677341,
		serverSeq: 1,
	}

	l := NewListenSocket(NewNetwork(context.Background(), nil, NetworkOptions{Seq: args.serverSeq}), 0)
	sock := NewConnectSocket(
		l,
		net.ParseIP(args.dstIp),
		args.dstPort,
		net.ParseIP(args.srcIp),
		args.srcPort,
	)
	client := endpoint{ip: args.srcIp, port: args.srcPort, t: t}
	server := endpoint{ip: args.dstIp, port: args.dstPort, t: t}
	///	}}}

	// c -> syn -> s
	// c <- syn,ack <- s
	{
		synIPPack, _ := client.pack(
			args.dstIp,
			args.dstPort,
			args.clientSeq,
			tcpip.TcpSYN,
			0,
			1024,
			nil,
		)

		synRespIPPack, _ := server.pack(
			args.srcIp,
			args.srcPort,
			args.serverSeq,
			tcpip.TcpSYN|tcpip.TcpACK,
			args.clientSeq+1,
			1024,
			nil,
		)

		synResp, err := l.handleState(sock, synIPPack.Payload.(*tcpip.TcpPack))
		assert.Nil(t, err)
		_, err = synResp.Encode()
		assert.Nil(t, err)
		assert.Equal(t, synRespIPPack, synResp)
		assert.Equal(t, sock.State, tcpip.TcpStateSynReceived)
	}

	// c -> ack -> s
	{
		ackPack, _ := client.pack(
			args.dstIp,
			args.dstPort,
			args.clientSeq+1,
			tcpip.TcpACK,
			args.serverSeq+1,
			1024,
			nil,
		)
		ackResp, err := sock.listenSocket.handleState(sock, ackPack.Payload.(*tcpip.TcpPack))
		assert.Nil(t, err)
		assert.Nil(t, ackResp)
		assert.Equal(t, sock.State, tcpip.TcpStateEstablished)
	}

	// c -> data -> s
	// c <- ack <- s
	{
		dataIPPack, _ := client.pack(
			args.dstIp,
			args.dstPort,
			args.clientSeq+1,
			tcpip.TcpPSH,
			args.serverSeq+1,
			1024,
			[]byte("hello\n"),
		)
		dataRespPack, _ := server.pack(
			args.srcIp,
			args.srcPort,
			args.serverSeq+1,
			tcpip.TcpACK,
			args.clientSeq+1+6,
			1024,
			nil,
		)

		// ack data
		dataResp, err := sock.listenSocket.handleState(sock, dataIPPack.Payload.(*tcpip.TcpPack))
		assert.Nil(t, err)
		_, err = dataResp.Encode()
		assert.Nil(t, err)
		assert.Equal(t, dataRespPack, dataResp)
	}

	// c -> fin -> s
	// c <- ack <- s
	{
		finPack, _ := client.pack(
			args.dstIp,
			args.dstPort,
			args.clientSeq+1+6,
			tcpip.TcpFIN,
			args.serverSeq+1,
			1024,
			nil,
		)
		finAckRespPack, _ := server.pack(
			args.srcIp,
			args.srcPort,
			args.serverSeq+1,
			tcpip.TcpACK,
			args.clientSeq+1+6+1,
			1024,
			nil,
		)

		finAckResp, err := sock.listenSocket.handleState(sock, finPack.Payload.(*tcpip.TcpPack))
		assert.Nil(t, err)
		_, err = finAckResp.Encode()
		assert.Nil(t, err)
		assert.Equal(t, finAckRespPack, finAckResp)
	}

	// c <- fin <- s
	{
		wantFinResp, _ := server.pack(
			args.srcIp,
			args.srcPort,
			args.serverSeq+1,
			tcpip.TcpFIN|tcpip.TcpACK,
			args.clientSeq+1+6+1,
			1024,
			nil,
		)

		finResp := sock.passiveCloseSocket()
		_, err := finResp.Encode()
		assert.Nil(t, err)
		assert.Equal(t, wantFinResp, finResp)
	}
}

func TestSocketClient(t *testing.T) {
	// setup
	// {{{
	args := struct {
		srcIp     string
		srcPort   uint16
		dstIp     string
		dstPort   uint16
		clientSeq uint32
		serverSeq uint32
	}{
		srcIp:     "127.0.0.1",
		srcPort:   52494,
		dstIp:     "127.0.0.1",
		dstPort:   12345,
		clientSeq: 4056677341,
		serverSeq: 1,
	}

	l := NewListenSocket(NewNetwork(context.Background(), nil, NetworkOptions{Seq: args.serverSeq}), 0)
	sock := NewConnectSocket(
		l,
		net.ParseIP(args.dstIp),
		args.dstPort,
		net.ParseIP(args.srcIp),
		args.srcPort,
	)
	client := endpoint{ip: args.srcIp, port: args.srcPort, t: t}
	server := endpoint{ip: args.dstIp, port: args.dstPort, t: t}
	///	}}}

	// c -> syn -> s
	// c <- syn,ack <- s
	{
		synIPPack, _ := client.pack(
			args.dstIp,
			args.dstPort,
			args.clientSeq,
			tcpip.TcpSYN,
			0,
			1024,
			nil,
		)

		synRespIPPack, _ := server.pack(
			args.srcIp,
			args.srcPort,
			args.serverSeq,
			tcpip.TcpSYN|tcpip.TcpACK,
			args.clientSeq+1,
			1024,
			nil,
		)

		synResp, err := l.handleState(sock, synIPPack.Payload.(*tcpip.TcpPack))
		assert.Nil(t, err)
		_, err = synResp.Encode()
		assert.Nil(t, err)
		assert.Equal(t, synRespIPPack, synResp)
		assert.Equal(t, sock.State, tcpip.TcpStateSynReceived)
	}

	// c -> ack -> s
	{
		ackPack, _ := client.pack(
			args.dstIp,
			args.dstPort,
			args.clientSeq+1,
			tcpip.TcpACK,
			args.serverSeq+1,
			1024,
			nil,
		)
		ackResp, err := sock.listenSocket.handleState(sock, ackPack.Payload.(*tcpip.TcpPack))
		assert.Nil(t, err)
		assert.Nil(t, ackResp)
		assert.Equal(t, sock.State, tcpip.TcpStateEstablished)
	}

	// c -> data -> s
	// c <- ack <- s
	{
		dataIPPack, _ := client.pack(
			args.dstIp,
			args.dstPort,
			args.clientSeq+1,
			tcpip.TcpPSH,
			args.serverSeq+1,
			1024,
			[]byte("hello\n"),
		)
		dataRespPack, _ := server.pack(
			args.srcIp,
			args.srcPort,
			args.serverSeq+1,
			tcpip.TcpACK,
			args.clientSeq+1+6,
			1024,
			nil,
		)

		// ack data
		dataResp, err := sock.listenSocket.handleState(sock, dataIPPack.Payload.(*tcpip.TcpPack))
		assert.Nil(t, err)
		_, err = dataResp.Encode()
		assert.Nil(t, err)
		assert.Equal(t, dataRespPack, dataResp)
	}

	// c -> fin -> s
	// c <- ack <- s
	{
		finPack, _ := client.pack(
			args.dstIp,
			args.dstPort,
			args.clientSeq+1+6,
			tcpip.TcpFIN,
			args.serverSeq+1,
			1024,
			nil,
		)
		finAckRespPack, _ := server.pack(
			args.srcIp,
			args.srcPort,
			args.serverSeq+1,
			tcpip.TcpACK,
			args.clientSeq+1+6+1,
			1024,
			nil,
		)

		finAckResp, err := sock.listenSocket.handleState(sock, finPack.Payload.(*tcpip.TcpPack))
		assert.Nil(t, err)
		_, err = finAckResp.Encode()
		assert.Nil(t, err)
		assert.Equal(t, finAckRespPack, finAckResp)
	}

	// c <- fin <- s
	{
		wantFinResp, _ := server.pack(
			args.srcIp,
			args.srcPort,
			args.serverSeq+1,
			tcpip.TcpFIN|tcpip.TcpACK,
			args.clientSeq+1+6+1,
			1024,
			nil,
		)

		finResp := sock.passiveCloseSocket()
		_, err := finResp.Encode()
		assert.Nil(t, err)
		assert.Equal(t, wantFinResp, finResp)
	}
}

type endpoint struct {
	ip   string
	port uint16
	t    *testing.T
}

func (e *endpoint) pack(
	dstIp string,
	dstPort uint16,
	seq uint32,
	flags tcpip.TcpFlag,
	ackNumber uint32,
	windowSize uint16,
	payload []byte,
) (ipPack *tcpip.IPPack, tcpPack *tcpip.TcpPack) {
	ipPack = &tcpip.IPPack{
		IPHeader: &tcpip.IPHeader{
			SrcIP:          net.ParseIP(e.ip),
			DstIP:          net.ParseIP(dstIp),
			Version:        4,
			HeaderLength:   0,
			TypeOfService:  0,
			TotalLength:    0,
			Identification: 0,
			Flags:          2,
			FragmentOffset: 0,
			TimeToLive:     64,
			Protocol:       6,
			HeaderChecksum: 0,
			Options:        nil,
		},
	}

	tcpPack = &tcpip.TcpPack{
		PseudoHeader: &tcpip.PseudoHeader{
			SrcIP: net.ParseIP(e.ip),
			DstIP: net.ParseIP(dstIp),
		},
		TcpHeader: &tcpip.TcpHeader{
			SrcPort:        e.port,
			DstPort:        dstPort,
			SequenceNumber: seq,
			AckNumber:      ackNumber,
			DataOffset:     0,
			Reserved:       0,
			Flags:          uint8(flags),
			WindowSize:     windowSize,
			Checksum:       0,
			UrgentPointer:  0,
			Options:        nil,
		},
	}
	if payload != nil {
		tcpPack.Payload = tcpip.NewRawPack(payload)
	}
	ipPack.Payload = tcpPack

	_, err := ipPack.Encode()
	assert.Nil(e.t, err)
	return ipPack, tcpPack
}
