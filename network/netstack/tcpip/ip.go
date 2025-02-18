package tcpip

import (
	"encoding/binary"
	"net"
)

type IPProtocol uint8

const (
	ProtocolTCP IPProtocol = 6
)

type IPHeader struct {
	SrcIP          net.IP
	DstIP          net.IP
	Version        uint8
	HeaderLength   uint8
	TypeOfService  uint8
	TotalLength    uint16
	Identification uint16
	Flags          uint8
	FragmentOffset uint16
	TimeToLive     uint8
	Protocol       uint8
	HeaderChecksum uint16
	Options        []byte
}

type IPPack struct {
	*IPHeader
	Payload NetworkPacket
}

func NewIPPack(payload NetworkPacket) *IPPack {
	return &IPPack{Payload: payload}
}

// https://datatracker.ietf.org/doc/html/rfc791#section-3.1
// 0                   1                   2                   3
// 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// |Version|  IHL  |Type of Service|          Total Length         |
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// |         Identification        |Flags|      Fragment Offset    |
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// |  Time to Live |    Protocol   |         Header Checksum       |
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// |                       Source Address                          |
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// |                    Destination Address                        |
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// |                    Options                    |    Padding    |
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
func (i *IPPack) Decode(data []byte) (*IPPack, error) {
	header := &IPHeader{
		Version:        data[0] >> 4,
		HeaderLength:   (data[0] & 0x0f) * 4,
		TypeOfService:  data[1],
		TotalLength:    binary.BigEndian.Uint16(data[2:4]),
		Identification: binary.BigEndian.Uint16(data[4:6]),
		Flags:          data[6] >> 5,
		FragmentOffset: binary.BigEndian.Uint16(data[6:8]) & 0x1fff,
		TimeToLive:     data[8],
		Protocol:       data[9],
		HeaderChecksum: binary.BigEndian.Uint16(data[10:12]),
		SrcIP:          net.IP(data[12:16]),
		DstIP:          net.IP(data[16:20]),
	}
	header.Options = data[20:header.HeaderLength]
	i.IPHeader = header
	payload, err := i.Payload.Decode(data[header.HeaderLength:])
	if err != nil {
		return nil, err
	}
	i.Payload = payload
	return i, nil
}

func (i *IPPack) Encode() ([]byte, error) {
	var (
		payload []byte
		err     error
	)
	if i.Payload != nil {
		payload, err = i.Payload.Encode()
		if err != nil {
			return nil, err
		}
	}
	data := make([]byte, 0)
	if i.HeaderLength == 0 {
		i.HeaderLength = uint8(20 + len(i.Options))
	}
	data = append(data, i.Version<<4|i.HeaderLength/4)
	data = append(data, i.TypeOfService)
	if i.TotalLength == 0 {
		i.TotalLength = uint16(i.HeaderLength) + uint16(len(payload))
	}
	data = binary.BigEndian.AppendUint16(data, i.TotalLength)
	data = binary.BigEndian.AppendUint16(data, i.Identification)
	data = binary.BigEndian.AppendUint16(data, uint16(i.Flags)<<13|i.FragmentOffset)
	data = append(data, i.TimeToLive)
	data = append(data, i.Protocol)
	data = binary.BigEndian.AppendUint16(data, i.HeaderChecksum)
	data = append(data, i.SrcIP...)
	data = append(data, i.DstIP...)
	data = append(data, i.Options...)
	if i.HeaderChecksum == 0 {
		i.HeaderChecksum = calculateIPChecksum(data)
	}
	binary.BigEndian.PutUint16(data[10:12], i.HeaderChecksum)
	data = append(data, payload...)

	return data, nil
}

// https://datatracker.ietf.org/doc/html/rfc1071#autoid-1
func calculateIPChecksum(headerData []byte) uint16 {
	if len(headerData)%2 == 1 {
		headerData = append(headerData, 0)
	}
	return ^OnesComplementSum(headerData)
}

func IsIPv4(data []byte) bool {
	return data[0]>>4 == 4
}

func IsTCP(data []byte) bool {
	return data[9] == uint8(ProtocolTCP)
}
