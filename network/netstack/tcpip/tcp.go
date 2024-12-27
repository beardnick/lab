package tcpip

import (
	"encoding/binary"
	"errors"
)

type TcpFlag uint8

const (
	TcpFIN TcpFlag = 1 << iota
	TcpSYN
	TcpRST
	TcpPSH
	TcpACK
	TcpURG
	TcpECE
	TcpCWR
)

type TcpState uint8

const (
	TcpStateClosed TcpState = iota
	TcpStateListen
	TcpStateSynSent
	TcpStateSynReceived
	TcpStateEstablished
	TcpStateFinWait1
	TcpStateFinWait2
	TcpStateCloseWait
	TcpStateLastAck
	TcpStateTimeWait
)

type TcpHeader struct {
	SrcPort        uint16
	DstPort        uint16
	SequenceNumber uint32
	AckNumber      uint32
	DataOffset     uint8
	Reserved       uint8
	Flags          uint8
	WindowSize     uint16
	Checksum       uint16
	UrgentPointer  uint16
	Options        []byte
}

type PseudoHeader struct {
	SrcIP []byte
	DstIP []byte
}

type TcpPack struct {
	*PseudoHeader
	*TcpHeader
	Payload NetworkPacket
}

func NewTcpPack(payload NetworkPacket) *TcpPack {
	return &TcpPack{Payload: payload}
}

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
		SrcPort:        binary.BigEndian.Uint16(data[0:2]),
		DstPort:        binary.BigEndian.Uint16(data[2:4]),
		SequenceNumber: binary.BigEndian.Uint32(data[4:8]),
		AckNumber:      binary.BigEndian.Uint32(data[8:12]),
		DataOffset:     (data[12] >> 4) * 4,
		Reserved:       data[12] & 0x0F,
		Flags:          data[13],
		WindowSize:     binary.BigEndian.Uint16(data[14:16]),
		Checksum:       binary.BigEndian.Uint16(data[16:18]),
		UrgentPointer:  binary.BigEndian.Uint16(data[18:20]),
	}
	header.Options = data[20:header.DataOffset]
	t.TcpHeader = header
	payload, err := t.Payload.Decode(data[header.DataOffset:])
	if err != nil {
		return nil, err
	}
	t.Payload = payload
	return t, nil
}

func (t *TcpPack) Encode() ([]byte, error) {
	data := make([]byte, 0)
	data = binary.BigEndian.AppendUint16(data, t.SrcPort)
	data = binary.BigEndian.AppendUint16(data, t.DstPort)
	data = binary.BigEndian.AppendUint32(data, t.SequenceNumber)
	data = binary.BigEndian.AppendUint32(data, t.AckNumber)
	if t.DataOffset == 0 {
		t.DataOffset = uint8(20 + len(t.Options))
	}
	data = append(data, ((t.DataOffset>>2)<<4)|t.Reserved)
	data = append(data, t.Flags)
	data = binary.BigEndian.AppendUint16(data, t.WindowSize)
	data = binary.BigEndian.AppendUint16(data, t.Checksum)
	data = binary.BigEndian.AppendUint16(data, t.UrgentPointer)
	data = append(data, t.Options...)
	if t.Payload != nil {
		payload, err := t.Payload.Encode()
		if err != nil {
			return nil, err
		}
		data = append(data, payload...)
	}
	if t.Checksum == 0 {
		if t.PseudoHeader == nil {
			return nil, errors.New("pseudo header is required to calculate tcp checksum")
		}
		t.Checksum = calculateTcpChecksum(t.PseudoHeader, data)
		binary.BigEndian.PutUint16(data[16:18], t.Checksum)
	}
	return data, nil
}

func (t *TcpPack) SetPseudoHeader(srcIP, dstIP []byte) {
	t.PseudoHeader = &PseudoHeader{SrcIP: srcIP, DstIP: dstIP}
}

func calculateTcpChecksum(pseudo *PseudoHeader, headerPayloadData []byte) uint16 {
	length := uint32(len(headerPayloadData))
	pseudoHeader := make([]byte, 0)
	pseudoHeader = append(pseudoHeader, pseudo.SrcIP...)
	pseudoHeader = append(pseudoHeader, pseudo.DstIP...)
	pseudoHeader = binary.BigEndian.AppendUint32(pseudoHeader, uint32(ProtocolTCP))
	pseudoHeader = binary.BigEndian.AppendUint32(pseudoHeader, length)

	sumData := make([]byte, 0)
	sumData = append(sumData, pseudoHeader...)
	sumData = append(sumData, headerPayloadData...)

	if len(sumData)%2 == 1 {
		sumData = append(sumData, 0)
	}

	var sum uint32
	for i := 0; i < len(sumData); i += 2 {
		sum += uint32(binary.BigEndian.Uint16(sumData[i : i+2]))
	}

	for sum>>16 != 0 {
		sum = (sum & 0xffff) + (sum >> 16)
	}

	return ^uint16(sum)
}
