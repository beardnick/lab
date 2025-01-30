package main

import (
	"fmt"
	"testing"
)

func TestNewId(t *testing.T) {
	tests := []struct {
		name    string
		wantId  string
		wantErr bool
	}{
		{
			name:    "normal",
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotId, err := NewId()
			if (err != nil) != tt.wantErr {
				t.Errorf("NewId() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			fmt.Println("got snowflakeid", gotId)
		})
	}
}
