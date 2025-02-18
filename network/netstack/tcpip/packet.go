package tcpip

import "encoding/binary"

type NetworkPacket interface {
	Decode(data []byte) (NetworkPacket, error)
	Encode() ([]byte, error)
}

type RawPack struct {
	data []byte
}

func NewRawPack(data []byte) *RawPack {
	return &RawPack{data: data}
}

func (r *RawPack) Decode(data []byte) (NetworkPacket, error) {
	r.data = data
	return r, nil
}

func (r *RawPack) Encode() ([]byte, error) {
	return r.data, nil
}

func OnesComplementSum(data []byte) uint16 {
	var sum uint16
	for i := 0; i < len(data); i += 2 {
		sum += binary.BigEndian.Uint16(data[i : i+2])
		if sum < binary.BigEndian.Uint16(data[i:i+2]) {
			sum++ // handle carry
		}
	}
	return sum
}
