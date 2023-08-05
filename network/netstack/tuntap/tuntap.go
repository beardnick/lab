//go:build linux
// +build linux

package tuntap

import (
	"errors"
	"fmt"
	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"log"
	"os/exec"
	"sync"
	"syscall"
	"unsafe"
)

var routeTable = map[string]*TunTap{}

type TunTapDevice string

const (
	TunDevice TunTapDevice = "tun"
	TapDevice TunTapDevice = "tap"
)

type TunTap struct {
	Name       string
	Addr       string
	Nic        int
	flags      int
	DeviceType TunTapDevice
	//handle     map[string]chan Packet
	ports    map[uint16]chan Packet
	portLock sync.RWMutex
}

// [create tun tap](https://www.kernel.org/doc/html/latest/networking/tuntap.html?highlight=tuntap)
func CreateTunTap(name string, flags int) (device TunTap, err error) {
	// syscall.IFF_NO_PI only raw package
	//flags := syscall.IFF_TUN | syscall.IFF_NO_PI
	//flags := syscall.IFF_TAP | syscall.IFF_NO_PI
	fd, err := syscall.Open("/dev/net/tun", syscall.O_RDWR, 0)
	if err != nil {
		return
	}
	var ifr struct {
		name  [16]byte // device name
		flags uint16   // flag
		_     [22]byte //padding
	}
	copy(ifr.name[:], name)
	ifr.flags = uint16(flags)
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), syscall.TUNSETIFF, uintptr(unsafe.Pointer(&ifr)))
	if errno != 0 {
		syscall.Close(fd)
		err = errno
		return
	}
	var deviceType TunTapDevice
	if flags&syscall.IFF_TUN > 0 {
		deviceType = TunDevice
	}
	if flags&syscall.IFF_TAP > 0 {
		deviceType = TapDevice
	}
	device = TunTap{
		Name:       name,
		Nic:        fd,
		flags:      flags,
		DeviceType: deviceType,
		ports:      map[uint16]chan Packet{},
	}
	return

}

func NewTun(name string) (device TunTap, err error) {
	flags := syscall.IFF_TUN | syscall.IFF_NO_PI
	return CreateTunTap(name, flags)
}

func NewTap(name string) (device TunTap, err error) {
	flags := syscall.IFF_TAP | syscall.IFF_NO_PI
	return CreateTunTap(name, flags)
}

func (t *TunTap) StartUp(ipCidr string) (err error) {
	// todo : validate ipCidr
	t.Addr = ipCidr
	err = SetIp(t.Name, t.Addr)
	if err != nil {
		return
	}
	err = SetUpLink(t.Name)
	if err != nil {
		return
	}
	t.Up()
	routeTable[ipCidr] = t
	return
}

func (t *TunTap) Up() {
	go func() {
		for {
			pack, err := ReadTcpPacket(t.Nic)
			if err != nil {
				continue
			}
			dstPort := uint16(pack.TcpPack.DstPort)
			t.portLock.Lock()
			portC, ok := t.ports[dstPort]
			if !ok {
				portC = make(chan Packet)
				t.ports[dstPort] = portC
			}
			t.portLock.Unlock()
			select {
			case portC <- pack:
			}
		}
	}()
}

func (t *TunTap) ReadTcpPacket(port uint16) (pack Packet, err error) {
	t.portLock.RLock()
	portC := t.ports[port]
	t.portLock.RUnlock()
	pack = <-portC
	return
}

func (t *TunTap) Write(data []byte) (err error) {
	_, err = syscall.Write(t.Nic, data)
	return
}

func (t *TunTap) Bind(port uint16) {
	t.portLock.Lock()
	_, ok := t.ports[port]
	if !ok {
		t.ports[port] = make(chan Packet)
	}
	t.portLock.Unlock()
}

// # startup tun/tap device
// ip link set ${device_name} up
func SetUpLink(name string) error {
	out, err := exec.Command("ip", "link", "set", name, "up").CombinedOutput()
	if err != nil {
		err = fmt.Errorf("%v:%v", err, string(out))
		return err
	}
	return nil
}

// # set tun/tap device ip
// ip addr add 192.168.0.1/24 dev ${device_name}
func SetIp(name, ip string) error {
	out, err := exec.Command("ip", "addr", "add", ip, "dev", name).CombinedOutput()
	if err != nil {
		err = fmt.Errorf("%v:%v", err, string(out))
		return err
	}
	return nil
}

func SetRoute(name, addr string) error {
	out, err := exec.Command("ip", "route", "add", addr, "dev", name).CombinedOutput()
	if err != nil {
		err = fmt.Errorf("%v:%v", err, string(out))
		return err
	}
	return nil
}

// # show tun/tap device info
// ip addr show $name
func IpShow(name string) (string, error) {
	out, err := exec.Command("ip", "addr", "show", name).CombinedOutput()
	if err != nil {
		err = fmt.Errorf("%v:%v", err, string(out))
		return "", err
	}
	return string(out), nil
}

func ReadTcpPacket(fd int) (pack Packet, err error) {
	buf := make([]byte, 1024)
	n, err := Read(fd, buf)
	if err != nil {
		log.Fatal(err)
	}
	p := gopacket.NewPacket(
		buf[:n],
		layers.LayerTypeIPv4,
		gopacket.Default,
	)
	return UnWrapTcp(p)
}

func UnWrapTcp(packet gopacket.Packet) (pack Packet, err error) {
	ipLayer := packet.Layer(layers.LayerTypeIPv4)
	if ipLayer == nil {
		err = ConnectionClosedErr
		return
	}
	pack.IpPack, _ = ipLayer.(*layers.IPv4)
	tcpLayer := packet.Layer(layers.LayerTypeTCP)
	if tcpLayer == nil {
		err = NotValidTcpErr
		return
	}
	pack.TcpPack, _ = tcpLayer.(*layers.TCP)
	return
}

type Packet struct {
	IpPack  *layers.IPv4
	TcpPack *layers.TCP
}

var (
	ConnectionClosedErr = Err{Code: 1, Msg: "connection closed"}
	NotValidTcpErr      = Err{Code: 2, Msg: "not valid tcp packet"}
	PortAlreadyInUsed   = Err{Code: 3, Msg: "port already in used"}
)

type Err struct {
	Code int
	Msg  string
}

func (e Err) Error() string {
	return e.Msg
}

func (e Err) Is(err error) bool {
	if newErr, ok := err.(Err); ok {
		return newErr.Code == e.Code
	}
	return false
}

func Route(host string) (t *TunTap, err error) {
	// todo 路由判断
	if len(routeTable) == 0 {
		err = errors.New("no route found")
		return
	}
	for _, tap := range routeTable {
		t = tap
	}
	return
}

type MockNick struct {
}

func (m *MockNick) StartUp(ipCidr string) (err error) {
	//TODO implement me
	panic("implement me")
}

func (m *MockNick) Up() {
	//TODO implement me
	panic("implement me")
}

func (m *MockNick) ReadTcpPacket(port uint16) (pack Packet, err error) {
	//TODO implement me
	panic("implement me")
}

func (m *MockNick) Write(data []byte) (err error) {
	//TODO implement me
	panic("implement me")
}

func (m *MockNick) Bind(port uint16) {
	//TODO implement me
	panic("implement me")
}

type INicDevice interface {
	StartUp(ipCidr string) (err error)
	Up()
	ReadTcpPacket(port uint16) (pack Packet, err error)
	Write(data []byte) (err error)
	Bind(port uint16)
}
