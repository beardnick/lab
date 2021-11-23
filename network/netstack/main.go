package main

import (
	"log"
	"network/tuntap"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
)

// NOTE: ping 192.168.1.1
// will not find packet
// because local route table redirect packet to lo
// ip route show table local
func main() {
	dev := "mytun"
	// tun running on the third level
	// tap running on the second level
	//net, err := tuntap.NewTap(dev)
	net, err := tuntap.NewTun(dev)
	if err != nil {
		log.Fatal(err)
	}
	err = tuntap.SetIp(dev, "192.168.1.1/24")
	if err != nil {
		log.Fatal(err)
	}
	err = tuntap.SetUpLink(dev)
	if err != nil {
		log.Fatal(err)
	}
	for {
		err = ParsePacket(net)
		if err != nil {
			log.Println(err)
			continue
		}
	}

}

func ParsePacket(fd int) (err error) {
	buf := make([]byte, 1024)
	n, err := tuntap.Read(fd, buf)
	if err != nil {
		log.Fatal(err)
	}
	pack := gopacket.NewPacket(
		buf[:n],
		layers.LayerTypeEthernet,
		gopacket.Default,
	)
	return Parse(pack)
}

func Parse(packet gopacket.Packet) (err error) {
	etherLayer := packet.Layer(layers.LayerTypeEthernet)
	if etherLayer != nil {
		ether, _ := etherLayer.(*layers.Ethernet)
		log.Printf("type: %v %v -. %v\n", ether.EthernetType, ether.SrcMAC, ether.DstMAC)
	}
	ipLayer := packet.Layer(layers.LayerTypeIPv4)
	if ipLayer != nil {
		ip, _ := ipLayer.(*layers.IPv4)
		log.Printf("ip: %v -> %v\n", ip.SrcIP, ip.DstIP)
	}
	tcpLayer := packet.Layer(layers.LayerTypeTCP)
	if tcpLayer != nil {
		tcp, _ := tcpLayer.(*layers.TCP)
		log.Printf("tcp: %v -> %v\n", tcp.SrcPort, tcp.DstPort)
		return
	}
	udpLayer := packet.Layer(layers.LayerTypeUDP)
	if udpLayer != nil {
		udp, _ := udpLayer.(*layers.UDP)
		log.Printf("tcp: %v -> %v\n", udp.SrcPort, udp.DstPort)
		return
	}
	return
}
