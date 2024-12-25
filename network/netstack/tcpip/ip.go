package tcpip

import (
	"encoding/binary"
	"net"
)

type IPHeader struct {
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
	SourceIP       net.IP
	Options        []byte
	DestinationIP  net.IP
}

type IPPack struct {
	header  *IPHeader
	payload NetworkPacket
}

func NewIPPack(payload NetworkPacket) *IPPack {
	return &IPPack{payload: payload}
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
		HeaderLength:   data[0] & 0x0f,
		TypeOfService:  data[1],
		TotalLength:    binary.BigEndian.Uint16(data[2:4]),
		Identification: binary.BigEndian.Uint16(data[4:6]),
		Flags:          data[6] >> 5,
		FragmentOffset: binary.BigEndian.Uint16(data[6:8]) & 0x1fff,
		TimeToLive:     data[8],
		Protocol:       data[9],
		HeaderChecksum: binary.BigEndian.Uint16(data[10:12]),
		SourceIP:       net.IP(data[12:16]),
		DestinationIP:  net.IP(data[16:20]),
	}
	header.Options = data[20 : header.HeaderLength*4]
	i.header = header
	payload, err := i.payload.Decode(data[header.HeaderLength*4:])
	if err != nil {
		return nil, err
	}
	i.payload = payload
	return i, nil
}

func (i *IPPack) Encode() ([]byte, error) {
	data := make([]byte, 0)
	data = append(data, i.header.Version<<4|i.header.HeaderLength)
	data = append(data, i.header.TypeOfService)
	data = binary.BigEndian.AppendUint16(data, i.header.TotalLength)
	data = binary.BigEndian.AppendUint16(data, i.header.Identification)
	data = binary.BigEndian.AppendUint16(data, uint16(i.header.Flags)<<13|i.header.FragmentOffset)
	data = append(data, i.header.TimeToLive)
	data = append(data, i.header.Protocol)
	data = binary.BigEndian.AppendUint16(data, i.header.HeaderChecksum)
	data = append(data, i.header.SourceIP...)
	data = append(data, i.header.DestinationIP...)
	data = append(data, i.header.Options...)
	payload, err := i.payload.Encode()
	if err != nil {
		return nil, err
	}
	data = append(data, payload...)
	return data, nil
}
