package main

import (
	"encoding/binary"
	"fmt"
	"net"
	"strings"
)

func main() {
	// 创建 UDP 服务器
	addr, err := net.ResolveUDPAddr("udp", ":1953")
	if err != nil {
		fmt.Println("Error resolving UDP address:", err)
		return
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		fmt.Println("Error listening on UDP:", err)
		return
	}
	defer conn.Close()

	for {
		// 读取 DNS 请求
		buf := make([]byte, 512)
		_, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			fmt.Println("Error reading from UDP:", err)
			continue
		}
		req := parseDnsReq(buf)
		fmt.Println(req)
	}
}

type DnsReq struct {
	ID                   uint16
	Flags                uint16
	QrFlag               uint16
	OpcodeFlag           uint16
	TCFlag               uint16
	RDFlag               uint16
	ZFlag                uint16
	NonAuthenticatedFlag uint16
	QuestionCount        uint16
	AnswerCount          uint16
	AuthorityCount       uint16
	AdditionalCount      uint16
	Name                 string
	QType                DnsType
	QClass               DnsClass
}

func parseDnsReq(buf []byte) DnsReq {
	// 解析 DNS 报头和 DNS 问题
	dnsHeader := DnsReq{}
	dnsHeader.ID = binary.BigEndian.Uint16(buf[:2])
	dnsHeader.Flags = binary.BigEndian.Uint16(buf[2:4])

	dnsHeader.QrFlag = dnsHeader.Flags >> 15 & 0x0001
	dnsHeader.OpcodeFlag = dnsHeader.Flags >> 11 & 0x000f
	dnsHeader.TCFlag = dnsHeader.Flags >> 9 & 0x0001
	dnsHeader.RDFlag = dnsHeader.Flags >> 8 & 0x0001
	dnsHeader.ZFlag = dnsHeader.Flags >> 6 & 0x0001
	dnsHeader.NonAuthenticatedFlag = dnsHeader.Flags >> 4 & 0x0001

	dnsHeader.QuestionCount = binary.BigEndian.Uint16(buf[4:6])
	dnsHeader.AnswerCount = binary.BigEndian.Uint16(buf[6:8])
	dnsHeader.AuthorityCount = binary.BigEndian.Uint16(buf[8:10])
	dnsHeader.AdditionalCount = binary.BigEndian.Uint16(buf[10:12])

	// 标识符序列
	// 头一个字节代表长度
	// 长度为0时结束
	// |len(1)|str(len)|...|0|
	offset := 12
	length := 0
	sequences := []string{}
	for {
		offset = offset + length + 1
		length = int(buf[offset-1])
		if length == 0 {
			break
		}
		data := buf[offset : offset+length]
		sequences = append(sequences, string(data))
	}
	dnsHeader.Name = strings.Join(sequences, ".")
	dnsHeader.QType = DnsType(binary.BigEndian.Uint16(buf[offset : offset+2]))
	dnsHeader.QClass = DnsClass(binary.BigEndian.Uint16(buf[offset+2 : offset+4]))

	return dnsHeader
}

//A          	 1
//AAAA       	 28
//AFSDB      	 18
//APL        	 42
//CAA        	 257
//CDNSKEY    	 60
//CDS        	 59
//CERT       	 37
//CNAME      	 5
//CSYNC      	 62
//DHCID      	 49
//DLV        	 32769
//DNAME      	 39
//DNSKEY     	 48
//DS         	 43
//EUI48      	 108
//EUI64      	 109
//HINFO      	 13
//HIP        	 55
//HTTPS      	 65
//IPSECKEY   	 45
//KEY        	 25
//KX         	 36
//LOC        	 29
//MX         	 15
//NAPTR      	 35
//NS         	 2
//NSEC       	 47
//NSEC3      	 50
//NSEC3PARAM 	 51
//OPENPGPKEY 	 61
//PTR        	 12
//RRSIG      	 46
//RP         	 17
//SIG        	 24
//SMIMEA     	 53
//SOA        	 6
//SRV        	 33
//SSHFP      	 44
//SVCB       	 64
//TA         	 32768
//TKEY       	 249
//TLSA       	 52
//TSIG       	 250
//TXT        	 16
//URI        	 256
//ZONEMD     	 63

type DnsType uint16
type DnsClass uint16

const (
	DnsTypeA          DnsType = 1
	DnsTypeAAAA       DnsType = 28
	DnsTypeAFSDB      DnsType = 18
	DnsTypeAPL        DnsType = 42
	DnsTypeCAA        DnsType = 257
	DnsTypeCDNSKEY    DnsType = 60
	DnsTypeCDS        DnsType = 59
	DnsTypeCERT       DnsType = 37
	DnsTypeCNAME      DnsType = 5
	DnsTypeCSYNC      DnsType = 62
	DnsTypeDHCID      DnsType = 49
	DnsTypeDLV        DnsType = 32769
	DnsTypeDNAME      DnsType = 39
	DnsTypeDNSKEY     DnsType = 48
	DnsTypeDS         DnsType = 43
	DnsTypeEUI48      DnsType = 108
	DnsTypeEUI64      DnsType = 109
	DnsTypeHINFO      DnsType = 13
	DnsTypeHIP        DnsType = 55
	DnsTypeHTTPS      DnsType = 65
	DnsTypeIPSECKEY   DnsType = 45
	DnsTypeKEY        DnsType = 25
	DnsTypeKX         DnsType = 36
	DnsTypeLOC        DnsType = 29
	DnsTypeMX         DnsType = 15
	DnsTypeNAPTR      DnsType = 35
	DnsTypeNS         DnsType = 2
	DnsTypeNSEC       DnsType = 47
	DnsTypeNSEC3      DnsType = 50
	DnsTypeNSEC3PARAM DnsType = 51
	DnsTypeOPENPGPKEY DnsType = 61
	DnsTypePTR        DnsType = 12
	DnsTypeRRSIG      DnsType = 46
	DnsTypeRP         DnsType = 17
	DnsTypeSIG        DnsType = 24
	DnsTypeSMIMEA     DnsType = 53
	DnsTypeSOA        DnsType = 6
	DnsTypeSRV        DnsType = 33
	DnsTypeSSHFP      DnsType = 44
	DnsTypeSVCB       DnsType = 64
	DnsTypeTA         DnsType = 32768
	DnsTypeTKEY       DnsType = 249
	DnsTypeTLSA       DnsType = 52
	DnsTypeTSIG       DnsType = 250
	DnsTypeTXT        DnsType = 16
	DnsTypeURI        DnsType = 256
	DnsTypeZONEMD     DnsType = 63
)

const (
	DnsClassIN DnsClass = 1
	DnsClassCS DnsClass = 2
	DnsClassCH DnsClass = 3
	DnsClassHS DnsClass = 4
)

func (d DnsClass) String() string {
	switch d {
	case DnsClassIN:
		return "IN"
	case DnsClassCS:
		return "CS"
	case DnsClassCH:
		return "CH"
	case DnsClassHS:
		return "HS"
	}
	return ""
}

func (d DnsType) String() string {
	switch d {
	case DnsTypeA:
		return "A"
	case DnsTypeAAAA:
		return "AAAA"
	case DnsTypeAFSDB:
		return "AFSDB"
	case DnsTypeAPL:
		return "APL"
	case DnsTypeCAA:
		return "CAA"
	case DnsTypeCDNSKEY:
		return "CDNSKEY"
	case DnsTypeCDS:
		return "CDS"
	case DnsTypeCERT:
		return "CERT"
	case DnsTypeCNAME:
		return "CNAME"
	case DnsTypeCSYNC:
		return "CSYNC"
	case DnsTypeDHCID:
		return "DHCID"
	case DnsTypeDLV:
		return "DLV"
	case DnsTypeDNAME:
		return "DNAME"
	case DnsTypeDNSKEY:
		return "DNSKEY"
	case DnsTypeDS:
		return "DS"
	case DnsTypeEUI48:
		return "EUI48"
	case DnsTypeEUI64:
		return "EUI64"
	case DnsTypeHINFO:
		return "HINFO"
	case DnsTypeHIP:
		return "HIP"
	case DnsTypeHTTPS:
		return "HTTPS"
	case DnsTypeIPSECKEY:
		return "IPSECKEY"
	case DnsTypeKEY:
		return "KEY"
	case DnsTypeKX:
		return "KX"
	case DnsTypeLOC:
		return "LOC"
	case DnsTypeMX:
		return "MX"
	case DnsTypeNAPTR:
		return "NAPTR"
	case DnsTypeNS:
		return "NS"
	case DnsTypeNSEC:
		return "NSEC"
	case DnsTypeNSEC3:
		return "NSEC3"
	case DnsTypeNSEC3PARAM:
		return "NSEC3PARAM"
	case DnsTypeOPENPGPKEY:
		return "OPENPGPKEY"
	case DnsTypePTR:
		return "PTR"
	case DnsTypeRRSIG:
		return "RRSIG"
	case DnsTypeRP:
		return "RP"
	case DnsTypeSIG:
		return "SIG"
	case DnsTypeSMIMEA:
		return "SMIMEA"
	case DnsTypeSOA:
		return "SOA"
	case DnsTypeSRV:
		return "SRV"
	case DnsTypeSSHFP:
		return "SSHFP"
	case DnsTypeSVCB:
		return "SVCB"
	case DnsTypeTA:
		return "TA"
	case DnsTypeTKEY:
		return "TKEY"
	case DnsTypeTLSA:
		return "TLSA"
	case DnsTypeTSIG:
		return "TSIG"
	case DnsTypeTXT:
		return "TXT"
	case DnsTypeURI:
		return "URI"
	case DnsTypeZONEMD:
		return "ZONEMD"
	}
	return ""
}
