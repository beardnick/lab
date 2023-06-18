//go:build linux

package main

//var name = flag.String("name", "tun0", "tuntap device name")
//var mode = flag.String("mode", "tun", "tuntap mode tun or tap")
//var ipCidr = flag.String("ip", "192.168.1.1/24", "tuntap ip cidr")

func main() {
	//flag.Parse()
	//net, err := tuntap.NewTun(*name)
	//if err != nil {
	//	log.Fatal(err)
	//}
	//err = tuntap.SetIp(*name, *ipCidr)
	//if err != nil {
	//	log.Fatal(err)
	//}
	//err = tuntap.SetUpLink(*name)
	//if err != nil {
	//	log.Fatal(err)
	//}
	//log.Println("net:", net)
	//nic := tcp.NewNic(net)
	//nic.Up()
	//sock := tcp.NewSocket(nic)
	//sock.Listen("192.168.1.1", 9090)
	//conn, err := sock.Accept()
	//if err != nil {
	//	return
	//}
	//fmt.Println("conn:", conn)
	//for {
	//	buf, err := sock.Rcvd(conn)
	//	if errors.Is(err, tcp.ConnectionClosedErr) {
	//		break
	//	}
	//	if err != nil {
	//		fmt.Println("read err:", err)
	//		continue
	//	}
	//	fmt.Printf("read:'%s'\n", string(buf))
	//	if len(buf) == 0 {
	//		continue
	//	}
	//	_, err = sock.Send(conn, buf)
	//	if err != nil {
	//		fmt.Println("send err:", err)
	//	}
	//	if string(buf) == "byte" {
	//		break
	//	}
	//}
	//fmt.Println("connection closed")
}
