package main

import (
	"reflect"
	"testing"
)

func Test_parseDnsHeader(t *testing.T) {
	type args struct {
		buf []byte
	}
	tests := []struct {
		name string
		args args
		want DnsReq
	}{
		{
			// dig example.com @127.0.0.1 -p 1953
			name: "normal",
			args: args{
				buf: []byte{
					0xec, 0x9f, 0x01, 0x20, 0x00, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x07,
					0x65, 0x78, 0x61, 0x6d, 0x70, 0x6c, 0x65, 0x03, 0x63, 0x6f, 0x6d, 0x00, 0x00,
					0x01, 0x00, 0x01, 0x00, 0x00, 0x29, 0x10, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
					0x00,
				},
			},
			want: DnsReq{
				ID:                   60575,
				Flags:                288,
				QrFlag:               0,
				OpcodeFlag:           0,
				TCFlag:               0,
				RDFlag:               1,
				ZFlag:                0,
				NonAuthenticatedFlag: 0,
				QuestionCount:        1,
				AnswerCount:          0,
				AuthorityCount:       0,
				AdditionalCount:      1,
				Name:                 "example.com",
				QType:                DnsTypeA,
				QClass:               DnsClassIN,
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := parseDnsReq(tt.args.buf); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("parseDnsReq() = %v, want %v", got, tt.want)
			}
		})
	}
}
