package main

import (
	"bytes"
	"io"
	"testing"
)

func TestGetSubsystemPath(t *testing.T) {
	type args struct {
		mountInfo io.Reader
		subSystem string
	}
	tests := []struct {
		name string
		args args
		want string
	}{
		{
			name: "memory",
			args: args{
				mountInfo: bytes.NewBufferString(`
5328 5325 0:21 / /sys/fs/cgroup/blkio rw,nosuid,nodev,noexec,relatime - cgroup cgroup rw,blkio
5330 5325 0:22 / /sys/fs/cgroup/pids rw,nosuid,nodev,noexec,relatime - cgroup cgroup rw,pids
5331 5325 0:23 / /sys/fs/cgroup/cpuset rw,nosuid,nodev,noexec,relatime - cgroup cgroup rw,cpuset
5332 5325 0:24 / /sys/fs/cgroup/memory rw,nosuid,nodev,noexec,relatime - cgroup cgroup rw,memory
5333 5325 0:25 / /sys/fs/cgroup/hugetlb rw,nosuid,nodev,noexec,relatime - cgroup cgroup rw,hugetlb
`),
				subSystem: "memory",
			},
			want: "/sys/fs/cgroup/memory",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GetSubsystemPath(tt.args.mountInfo, tt.args.subSystem); got != tt.want {
				t.Errorf("GetSubsystemPath() = %v, want %v", got, tt.want)
			}
		})
	}
}
