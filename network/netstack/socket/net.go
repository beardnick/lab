package socket

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"netstack/tcpip"
	"netstack/tuntap"
	"strconv"
	"sync"
)

type SockFile interface {
	Close() error
	Read() (data []byte, err error)
	Write(b []byte) (n int, err error)
}

func TcpSocket() (fd int, err error) {
	if defaultNetwork == nil {
		return 0, NoNetworkErr
	}
	return defaultNetwork.applyListenSocket(), nil
}

func Bind(fd int, addr string) (err error) {
	if defaultNetwork == nil {
		return NoNetworkErr
	}
	return defaultNetwork.bind(fd, addr)
}

func Listen(fd int, backlog int) (err error) {
	if defaultNetwork == nil {
		return NoNetworkErr
	}
	return defaultNetwork.listen(fd, backlog)
}

func Accept(fd int) (cfd int, err error) {
	if defaultNetwork == nil {
		return 0, NoNetworkErr
	}
	return defaultNetwork.accept(fd)
}

func Read(fd int) (data []byte, err error) {
	if defaultNetwork == nil {
		return nil, NoNetworkErr
	}
	return defaultNetwork.read(fd)
}

func Send(fd int, data []byte) (err error) {
	if defaultNetwork == nil {
		return NoNetworkErr
	}
	return defaultNetwork.send(fd, data)
}

func Connect(fd int, addr string) (err error) {
	panic("not implemented")
}

func Close(fd int) (err error) {
	if defaultNetwork == nil {
		return NoNetworkErr
	}
	return defaultNetwork.close(fd)
}

var (
	NoNetworkErr    = errors.New("no network setup")
	AlreadyInUseErr = errors.New("address already in use")
	NoSocketErr     = errors.New("no socket")
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
	ctx     context.Context
	tun     *tuntap.Tun
	writeCh chan []byte
	opt     NetworkOptions
	// files   map[int]SockFile
	// route   map[string]map[int]int // todo real route match
	route sync.Map
	files sync.Map
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
		ctx:     ctx,
		tun:     tun,
		files:   sync.Map{},
		route:   sync.Map{},
		writeCh: make(chan []byte),
		opt:     opt,
	}
}

func SetupDefaultNetwork(ctx context.Context, tun *tuntap.Tun, opt NetworkOptions) {
	defaultNetwork = NewNetwork(ctx, tun, opt)
	defaultNetwork.runloop()
}

var defaultNetwork *Network

func (n *Network) runloop() {
	go n.readloop()
	go n.writeloop()
}

func (n *Network) readloop() {
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

func (n *Network) writeloop() {
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
	sock, ok := n.getListenSocket(ipPack.DstIP, int(tcpPack.DstPort))
	if !ok {
		return
	}
	select {
	case sock.writeCh <- ipPack:
	default:
		log.Printf("socket %s:%d is full,drop packet", sock.ip, sock.port)
	}
}

func (n *Network) bind(fd int, addr string) (err error) {
	ip, port, err := parseAddress(addr)
	if err != nil {
		return err
	}
	n.Lock()
	defer n.Unlock()
	sock, ok := n.getListenSocketByFd(fd)
	if !ok {
		return fmt.Errorf("%w: %d", NoSocketErr, fd)
	}
	_, ok = n.getListenSocket(ip, port)
	if ok {
		return fmt.Errorf("%w: %s", AlreadyInUseErr, addr)
	}
	sock.ip = ip
	sock.port = port
	n.registerSocket(fd, ip, port)
	return nil
}

func (n *Network) listen(fd int, backlog int) (err error) {
	sock, ok := n.getListenSocketByFd(fd)
	if !ok {
		return fmt.Errorf("%w: %d", NoSocketErr, fd)
	}
	return sock.Listen(backlog)
}

func (n *Network) accept(fd int) (cfd int, err error) {
	sock, ok := n.getListenSocketByFd(fd)
	if !ok {
		return 0, fmt.Errorf("%w: %d", NoSocketErr, fd)
	}
	return sock.Accept()
}

func (n *Network) close(fd int) (err error) {
	sock, ok := n.getConnectSocket(fd)
	if !ok {
		return fmt.Errorf("%w: %d", NoSocketErr, fd)
	}
	return sock.Close()
}

func (n *Network) read(fd int) (data []byte, err error) {
	sock, ok := n.getConnectSocket(fd)
	if !ok {
		return nil, fmt.Errorf("%w: %d", NoSocketErr, fd)
	}
	return sock.Read()
}

func (n *Network) send(fd int, data []byte) (err error) {
	sock, ok := n.getConnectSocket(fd)
	if !ok {
		return fmt.Errorf("%w: %d", NoSocketErr, fd)
	}
	_, err = sock.Write(data)
	return err
}

func (n *Network) getSocketFd(ip net.IP, port int) (fd int, ok bool) {
	ports, ok := n.route.Load(ip.String())
	if !ok {
		return 0, false
	}
	portsMap := ports.(*sync.Map)
	value, ok := portsMap.Load(port)
	if !ok {
		return 0, false
	}
	return value.(int), true
}

func (n *Network) getListenSocket(ip net.IP, port int) (sock *ListenSocket, ok bool) {
	fd, ok := n.getSocketFd(ip, port)
	if !ok {
		return nil, false
	}
	return n.getListenSocketByFd(fd)
}

func (n *Network) getListenSocketByFd(fd int) (sock *ListenSocket, ok bool) {
	f, ok := n.files.Load(fd - 1)
	if !ok {
		return nil, false
	}
	sock = f.(*ListenSocket)
	return sock, true
}

func (n *Network) getConnectSocket(fd int) (sock *ConnectSocket, ok bool) {
	f, ok := n.files.Load(fd - 1)
	if !ok {
		return nil, false
	}
	sock = f.(*ConnectSocket)
	return sock, true
}

func (n *Network) registerSocket(fd int, ip net.IP, port int) {
	ports, ok := n.route.Load(ip.String())
	if !ok {
		ports = &sync.Map{}
		n.route.Store(ip.String(), ports)
	}
	portsMap := ports.(*sync.Map)
	portsMap.Store(port, fd)
}

func (n *Network) applyListenSocket() (fd int) {
	n.Lock()
	defer n.Unlock()
	for i := 0; ; i++ {
		if _, ok := n.files.Load(i); !ok {
			fd = i + 1
			n.files.Store(i, NewListenSocket(n, fd))
			return fd
		}
	}
}

func (n *Network) addFile(f SockFile) (fd int) {
	n.Lock()
	defer n.Unlock()
	for i := 0; ; i++ {
		if _, ok := n.files.Load(i); !ok {
			fd = i + 1
			n.files.Store(i, f)
			return fd
		}
	}
}

func parseAddress(addr string) (ip net.IP, port int, err error) {
	host, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, 0, fmt.Errorf("invalid address %s: %w", addr, err)
	}

	ip = net.ParseIP(host)
	if ip == nil {
		return nil, 0, fmt.Errorf("invalid ip address: %s", host)
	}

	portNum, err := strconv.Atoi(portStr)
	if err != nil || portNum < 0 || portNum > 65535 {
		return nil, 0, fmt.Errorf("invalid port number: %s", portStr)
	}
	return ip, portNum, nil
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
		"%s %s:%d -> %s:%d %v seq=%d ack=%d len=%d\n",
		prefix,
		ipPack.SrcIP,
		tcpPack.SrcPort,
		ipPack.DstIP,
		tcpPack.DstPort,
		tcpip.InspectFlags(tcpPack.Flags),
		tcpPack.SequenceNumber,
		tcpPack.AckNumber,
		len(payload),
	)
}