package tcpip

import "encoding/binary"

type TcpFlag uint8

const (
	TcpCWR TcpFlag = 1 << iota
	TcpECE
	TcpURG
	TcpACK
	TcpPSH
	TcpRST
	TcpSYN
	TcpFIN
)

type TcpHeader struct {
	SourcePort      uint16
	DestinationPort uint16
	SequenceNumber  uint32
	AckNumber       uint32
	DataOffset      uint8
	Reserved        uint8
	Flags           uint8
	WindowSize      uint16
	Checksum        uint16
	UrgentPointer   uint16
	Options         []byte
}

type TcpPack struct {
	header  *TcpHeader
	payload NetworkPacket
}

func NewTcpPack(payload NetworkPacket) *TcpPack {
	return &TcpPack{payload: payload}
}

// func EncodeTcpHeader(header *TcpHeader) []byte {
// }

// https://datatracker.ietf.org/doc/html/rfc9293#name-header-format
// 0                   1                   2                   3
// 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// |          Source Port          |       Destination Port        |
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// |                        Sequence Number                        |
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// |                    Acknowledgment Number                      |
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// |  Data |       |C|E|U|A|P|R|S|F|                               |
// | Offset| Rsrvd |W|C|R|C|S|S|Y|I|            Window             |
// |       |       |R|E|G|K|H|T|N|N|                               |
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// |           Checksum            |         Urgent Pointer        |
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// |                           [Options]                           |
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
// |                                                               :
// :                             Data                              :
// :                                                               |
// +-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
func (t *TcpPack) Decode(data []byte) (NetworkPacket, error) {
	header := &TcpHeader{
		SourcePort:      binary.BigEndian.Uint16(data[0:2]),
		DestinationPort: binary.BigEndian.Uint16(data[2:4]),
		SequenceNumber:  binary.BigEndian.Uint32(data[4:8]),
		AckNumber:       binary.BigEndian.Uint32(data[8:12]),
		DataOffset:      (data[12] >> 4) * 4,
		Reserved:        data[12] & 0x0F,
		Flags:           data[13],
		WindowSize:      binary.BigEndian.Uint16(data[14:16]),
		Checksum:        binary.BigEndian.Uint16(data[16:18]),
		UrgentPointer:   binary.BigEndian.Uint16(data[18:20]),
	}
	header.Options = data[20:header.DataOffset]
	t.header = header
	payload, err := t.payload.Decode(data[header.DataOffset:])
	if err != nil {
		return nil, err
	}
	t.payload = payload
	return t, nil
}

func (t *TcpPack) Encode() ([]byte, error) {
	data := make([]byte, 0)
	data = binary.BigEndian.AppendUint16(data, t.header.SourcePort)
	data = binary.BigEndian.AppendUint16(data, t.header.DestinationPort)
	data = binary.BigEndian.AppendUint32(data, t.header.SequenceNumber)
	data = binary.BigEndian.AppendUint32(data, t.header.AckNumber)
	data = append(data, ((t.header.DataOffset>>2)<<4)|t.header.Reserved)
	data = append(data, t.header.Flags)
	data = binary.BigEndian.AppendUint16(data, t.header.WindowSize)
	data = binary.BigEndian.AppendUint16(data, t.header.Checksum)
	data = binary.BigEndian.AppendUint16(data, t.header.UrgentPointer)
	data = append(data, t.header.Options...)
	payload, err := t.payload.Encode()
	if err != nil {
		return nil, err
	}
	data = append(data, payload...)
	return data, nil
}
