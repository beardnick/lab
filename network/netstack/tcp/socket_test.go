package tcp

import (
	"github.com/google/gopacket/layers"
	"github.com/stretchr/testify/assert"
	"testing"
)

func TestConnection_caculateNext(t *testing.T) {
	type fields struct {
		PeerNxt uint32
	}
	type args struct {
		ip  *layers.IPv4
		tcp *layers.TCP
	}
	tests := []struct {
		name         string
		fields       fields
		args         args
		wantPeerNext uint32
	}{
		{
			name: "syn packet",
			fields: fields{
				PeerNxt: 1,
			},
			args: args{
				ip: &layers.IPv4{},
				tcp: &layers.TCP{
					Seq: 1,
					FIN: false,
					SYN: true,
					PSH: false,
					ACK: false,
				},
			},
			wantPeerNext: 2,
		},
		{
			name: "with payload",
			fields: fields{
				PeerNxt: 1,
			},
			args: args{
				ip: &layers.IPv4{},
				tcp: &layers.TCP{
					BaseLayer: layers.BaseLayer{
						Payload: []byte("hello world"),
					},
					Seq: 1,
					FIN: false,
					SYN: false,
					PSH: true,
					ACK: true,
				},
			},
			wantPeerNext: 12,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Connection{
				dstAcked: tt.fields.PeerNxt,
			}
			c.caculatePeerNext(tt.args.ip, tt.args.tcp)
			assert.Equal(t, tt.wantPeerNext, c.dstAcked)
		})
	}
}
