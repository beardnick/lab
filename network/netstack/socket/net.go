package socket

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"net"
	"netstack/tcpip"
	"netstack/tuntap"
	"strconv"
	"sync"
	"time"
)

type SockFile interface {
	Close() error
	Read() (data []byte, err error)
	Write(b []byte) (n int, err error)
}

func TcpSocket() (fd int, err error) {
	if defaultNetwork == nil {
		return 0, ErrNoNetwork
	}
	sock := NewSocket(defaultNetwork)
	fd = defaultNetwork.addSocket(sock)
	return fd, nil
}

func Bind(fd int, addr string) (err error) {
	if defaultNetwork == nil {
		return ErrNoNetwork
	}
	return defaultNetwork.bind(fd, addr)
}

func Listen(fd int, backlog int) (err error) {
	if defaultNetwork == nil {
		return ErrNoNetwork
	}
	return defaultNetwork.listen(fd, backlog)
}

func Accept(fd int) (cfd int, err error) {
	if defaultNetwork == nil {
		return 0, ErrNoNetwork
	}
	return defaultNetwork.accept(fd)
}

func AcceptWithTimeout(fd int, timeout time.Duration) (cfd int, err error) {
	if defaultNetwork == nil {
		return 0, ErrNoNetwork
	}
	return defaultNetwork.acceptWithTimeout(fd, timeout)
}

func Read(fd int) (data []byte, err error) {
	if defaultNetwork == nil {
		return nil, ErrNoNetwork
	}
	return defaultNetwork.read(fd)
}

func Send(fd int, data []byte) (err error) {
	if defaultNetwork == nil {
		return ErrNoNetwork
	}
	return defaultNetwork.send(fd, data)
}

func Connect(fd int, serverAddr string) (err error) {
	if defaultNetwork == nil {
		return ErrNoNetwork
	}
	return defaultNetwork.connect(fd, serverAddr)
}

func Close(fd int) (err error) {
	if defaultNetwork == nil {
		return ErrNoNetwork
	}
	return defaultNetwork.close(fd)
}

var (
	ErrNoNetwork    = errors.New("no network setup")
	ErrAlreadyInUse = errors.New("address already in use")
	ErrNoSocket     = errors.New("no socket")
)

type NetworkOptions struct {
	MTU        int
	WindowSize uint16
	Seq        uint32
	Backlog    int
	Debug      bool
}

type Network struct {
	sync.Mutex
	ctx       context.Context
	tun       *tuntap.Tun
	writeCh   chan []byte
	opt       NetworkOptions
	sockets   sync.Map
	socketFds sync.Map
}

func NewNetwork(
	ctx context.Context,
	tun *tuntap.Tun,
	opt NetworkOptions,
) *Network {
	if opt.MTU == 0 {
		opt.MTU = 1500
	}
	if opt.WindowSize == 0 {
		opt.WindowSize = 1024
	}
	if opt.Backlog == 0 {
		opt.Backlog = 10
	}

	return &Network{
		ctx:       ctx,
		tun:       tun,
		sockets:   sync.Map{},
		socketFds: sync.Map{},
		writeCh:   make(chan []byte),
		opt:       opt,
	}
}

func NetworkInitialized() bool {
	return defaultNetwork != nil
}

func SetupDefaultNetwork(ctx context.Context, tun *tuntap.Tun, opt NetworkOptions) {
	defaultNetwork = NewNetwork(ctx, tun, opt)
	defaultNetwork.runloop()
}

var defaultNetwork *Network

func (n *Network) runloop() {
	started := sync.WaitGroup{}
	started.Add(2)
	go n.readloop(&started)
	go n.writeloop(&started)
	started.Wait()
}

func (n *Network) readloop(started *sync.WaitGroup) {
	started.Done()
	for {
		select {
		case <-n.ctx.Done():
			log.Println("read loop exit")
			return
		default:
			buf := make([]byte, n.opt.MTU)
			num, err := n.tun.Read(buf)
			if err != nil {
				log.Println("read from tun failed", err)
				continue
			}
			if n.opt.Debug {
				debugPacket("recv", buf[:num])
			}
			n.handle(buf[:num])
		}
	}
}

func (n *Network) writeloop(started *sync.WaitGroup) {
	started.Done()
	for {
		select {
		case data := <-n.writeCh:
			var (
				num int
				err error
			)
			if n.opt.Debug {
				debugPacket("send", data)
			}
			for num < len(data) {
				data = data[num:]
				num, err = n.tun.Write(data)
				if err != nil {
					log.Println("write to tun failed", err)
					break
				}
			}
		case <-n.ctx.Done():
			log.Println("write loop exit")
			return
		}
	}
}

func (n *Network) handle(data []byte) {
	if !tcpip.IsIPv4(data) {
		log.Println("not ipv4 packet,just skip")
		return
	}
	if !tcpip.IsTCP(data) {
		log.Println("not tcp packet,just skip")
		return
	}

	ipPack, err := tcpip.NewIPPack(tcpip.NewTcpPack(tcpip.NewRawPack(nil))).Decode(data)
	if err != nil {
		log.Println("decode tcp packet failed", err)
		return
	}
	tcpPack := ipPack.Payload.(*tcpip.TcpPack)
	sock, ok := n.getSocket(
		SocketAddr{
			SrcIP:   ipPack.SrcIP.String(),
			SrcPort: tcpPack.SrcPort,
			DstIP:   ipPack.DstIP.String(),
			DstPort: tcpPack.DstPort,
		},
	)
	if !ok {
		return
	}
	sock.Lock()
	if sock.State == tcpip.TcpStateUnInitialized {
		log.Printf("socket %s:%d is not initialized,drop packet", sock.localIP, sock.localPort)
		sock.Unlock()
		return
	}
	sock.Unlock()
	select {
	case sock.writeCh <- ipPack:
	default:
		log.Printf("socket %s:%d is full,drop packet", sock.localIP, sock.localPort)
	}
}

func (n *Network) bind(fd int, addr string) (err error) {
	ip, port, err := parseAddress(addr)
	if err != nil {
		return err
	}
	n.Lock()
	defer n.Unlock()
	sock, ok := n.getSocketByFd(fd)
	if !ok {
		return fmt.Errorf("%w: %d", ErrNoSocket, fd)
	}
	sock.localIP = ip.String()
	sock.localPort = port
	n.bindSocket(SocketAddr{
		DstIP:   ip.String(),
		DstPort: port,
	}, fd)
	return nil
}

func (n *Network) listen(fd int, backlog int) (err error) {
	sock, ok := n.getSocketByFd(fd)
	if !ok {
		return fmt.Errorf("%w: %d", ErrNoSocket, fd)
	}
	InitListenSocket(sock)
	return sock.Listen(backlog)
}

func (n *Network) accept(fd int) (cfd int, err error) {
	sock, ok := n.getSocketByFd(fd)
	if !ok {
		return 0, fmt.Errorf("%w: %d", ErrNoSocket, fd)
	}
	return sock.Accept()
}

func (n *Network) acceptWithTimeout(fd int, timeout time.Duration) (cfd int, err error) {
	sock, ok := n.getSocketByFd(fd)
	if !ok {
		return 0, fmt.Errorf("%w: %d", ErrNoSocket, fd)
	}
	return sock.AcceptWithTimeout(timeout)
}

func (n *Network) close(fd int) (err error) {
	sock, ok := n.getSocketByFd(fd)
	if !ok {
		return fmt.Errorf("%w: %d", ErrNoSocket, fd)
	}
	return sock.Close()
}

func (n *Network) read(fd int) (data []byte, err error) {
	sock, ok := n.getSocketByFd(fd)
	if !ok {
		return nil, fmt.Errorf("%w: %d", ErrNoSocket, fd)
	}
	return sock.Read()
}

func (n *Network) send(fd int, data []byte) (err error) {
	sock, ok := n.getSocketByFd(fd)
	if !ok {
		return fmt.Errorf("%w: %d", ErrNoSocket, fd)
	}
	_, err = sock.Write(data)
	return err
}

func (n *Network) connect(fd int, serverAddr string) (err error) {
	serverIP, serverPort, err := parseAddress(serverAddr)
	if err != nil {
		return err
	}
	n.Lock()
	defer n.Unlock()
	sock, ok := n.getSocketByFd(fd)
	if !ok {
		return fmt.Errorf("%w: %d", ErrNoSocket, fd)
	}
	var addr SocketAddr
	if sock.localIP == "" && sock.localPort == 0 {
		addr, err = n.getAvailableAddress()
		if err != nil {
			return err
		}
		sock.localIP = addr.DstIP
		sock.localPort = addr.DstPort
	} else {
		n.unbindSocket(SocketAddr{
			DstIP:   sock.localIP,
			DstPort: sock.localPort,
		})
		addr = SocketAddr{
			DstIP:   sock.localIP,
			DstPort: sock.localPort,
		}
	}
	addr.SrcIP = serverIP.String()
	addr.SrcPort = serverPort
	n.bindSocket(addr, fd)
	InitConnectSocket(
		sock,
		nil,
		net.ParseIP(sock.localIP),
		sock.localPort,
		serverIP,
		serverPort,
	)
	return sock.Connect()
}

func (n *Network) getSocket(addr SocketAddr) (sock *Socket, ok bool) {
	value, ok := n.socketFds.Load(addr)
	if ok {
		return n.getSocketByFd(value.(int))
	}
	newAddr := SocketAddr{
		DstIP:   addr.DstIP,
		DstPort: addr.DstPort,
	}
	value, ok = n.socketFds.Load(newAddr)
	if ok {
		return n.getSocketByFd(value.(int))
	}
	return nil, false
}

func (n *Network) getSocketByFd(fd int) (sock *Socket, ok bool) {
	f, ok := n.sockets.Load(fd)
	if !ok {
		return nil, false
	}
	sock = f.(*Socket)
	return sock, true
}

func (n *Network) bindSocket(addr SocketAddr, fd int) {
	n.socketFds.Store(addr, fd)
}

func (n *Network) unbindSocket(addr SocketAddr) {
	n.socketFds.Delete(addr)
}

func (n *Network) getAvailableAddress() (addr SocketAddr, err error) {
	// todo: use a better algorithm to find a available address
	ip := n.tun.IP()
	ip = ip.To4()
	ip[3]++
	localIp := ip.String()
	var p uint16
	for p = 1; p < 65535; p++ {
		if _, ok := n.socketFds.Load(SocketAddr{DstIP: localIp, DstPort: p}); !ok {
			return SocketAddr{
				DstIP:   localIp,
				DstPort: p,
			}, nil
		}
	}
	return SocketAddr{}, fmt.Errorf("no available address")
}

func (n *Network) applyFd() (fd int) {
	// todo: use a better algorithm to find a available fd
	for i := 1; ; i++ {
		if _, ok := n.sockets.Load(i); !ok {
			fd = i
			return fd
		}
	}
}

func (n *Network) addSocket(f *Socket) (fd int) {
	n.Lock()
	defer n.Unlock()
	f.fd = n.applyFd()
	n.sockets.Store(f.fd, f)
	return f.fd
}

func (n *Network) removeSocket(fd int) {
	n.sockets.Delete(fd)
}

func parseAddress(addr string) (ip net.IP, port uint16, err error) {
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, 0, fmt.Errorf("invalid address %s: %w", addr, err)
	}

	ip = net.ParseIP(host).To4()
	if ip == nil {
		return nil, 0, fmt.Errorf("invalid ip address: %s", host)
	}

	portNum, err := strconv.Atoi(portStr)
	if err != nil || portNum < 0 || portNum > 65535 {
		return nil, 0, fmt.Errorf("invalid port number: %s", portStr)
	}
	return ip, uint16(portNum), nil
}

func debugPacket(prefix string, data []byte) {
	if !tcpip.IsIPv4(data) {
		return
	}
	if !tcpip.IsTCP(data) {
		return
	}

	ipPack, err := tcpip.NewIPPack(tcpip.NewTcpPack(tcpip.NewRawPack(nil))).Decode(data)
	if err != nil {
		log.Println("decode tcp packet failed", err)
		return
	}
	tcpPack := ipPack.Payload.(*tcpip.TcpPack)
	payload, err := tcpPack.Payload.Encode()
	if err != nil {
		log.Println("encode tcp payload failed", err)
		return
	}
	log.Printf(
		"%s %s:%d -> %s:%d %v seq=%d ack=%d len=%d %s\n",
		prefix,
		ipPack.SrcIP,
		tcpPack.SrcPort,
		ipPack.DstIP,
		tcpPack.DstPort,
		tcpip.InspectFlags(tcpPack.Flags),
		tcpPack.SequenceNumber,
		tcpPack.AckNumber,
		len(payload),
		hex.Dump(payload),
	)
}
