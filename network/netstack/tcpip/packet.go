package tcpip

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
