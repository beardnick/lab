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
				mountInfo: bytes.NewBufferString(`24 28 0:22 / /proc rw,nosuid,nodev,noexec,relatime shared:5 - proc proc rw
25 28 0:23 / /sys rw,nosuid,nodev,noexec,relatime shared:6 - sysfs sys rw
26 28 0:5 / /dev rw,nosuid,relatime shared:2 - devtmpfs dev rw,size=4067428k,nr_inodes=1016857,mode=755,inode64
27 28 0:24 / /run rw,nosuid,nodev,relatime shared:13 - tmpfs run rw,mode=755,inode64
28 1 8:1 / / rw,noatime shared:1 - ext4 /dev/sda1 rw
29 25 0:7 / /sys/kernel/security rw,nosuid,nodev,noexec,relatime shared:7 - securityfs securityfs rw
30 26 0:25 / /dev/shm rw,nosuid,nodev shared:3 - tmpfs tmpfs rw,inode64
31 26 0:26 / /dev/pts rw,nosuid,noexec,relatime shared:4 - devpts devpts rw,gid=5,mode=620,ptmxmode=000
32 25 0:27 / /sys/fs/cgroup ro,nosuid,nodev,noexec shared:8 - tmpfs tmpfs ro,size=4096k,nr_inodes=1024,mode=755,inode64
33 32 0:28 / /sys/fs/cgroup/unified rw,nosuid,nodev,noexec,relatime shared:9 - cgroup2 cgroup2 rw,nsdelegate
34 32 0:29 / /sys/fs/cgroup/systemd rw,nosuid,nodev,noexec,relatime shared:10 - cgroup cgroup rw,xattr,name=systemd
35 25 0:30 / /sys/fs/pstore rw,nosuid,nodev,noexec,relatime shared:11 - pstore pstore rw
36 25 0:31 / /sys/fs/bpf rw,nosuid,nodev,noexec,relatime shared:12 - bpf none rw,mode=700
37 32 0:32 / /sys/fs/cgroup/memory rw,nosuid,nodev,noexec,relatime shared:14 - cgroup cgroup rw,memory
38 32 0:33 / /sys/fs/cgroup/hugetlb rw,nosuid,nodev,noexec,relatime shared:15 - cgroup cgroup rw,hugetlb
39 32 0:34 / /sys/fs/cgroup/net_cls,net_prio rw,nosuid,nodev,noexec,relatime shared:16 - cgroup cgroup rw,net_cls,net_prio
40 32 0:35 / /sys/fs/cgroup/perf_event rw,nosuid,nodev,noexec,relatime shared:17 - cgroup cgroup rw,perf_event
41 32 0:36 / /sys/fs/cgroup/devices rw,nosuid,nodev,noexec,relatime shared:18 - cgroup cgroup rw,devices
42 32 0:37 / /sys/fs/cgroup/cpu,cpuacct rw,nosuid,nodev,noexec,relatime shared:19 - cgroup cgroup rw,cpu,cpuacct
43 32 0:38 / /sys/fs/cgroup/cpuset rw,nosuid,nodev,noexec,relatime shared:20 - cgroup cgroup rw,cpuset
44 32 0:39 / /sys/fs/cgroup/pids rw,nosuid,nodev,noexec,relatime shared:21 - cgroup cgroup rw,pids
45 32 0:40 / /sys/fs/cgroup/blkio rw,nosuid,nodev,noexec,relatime shared:22 - cgroup cgroup rw,blkio
46 32 0:41 / /sys/fs/cgroup/rdma rw,nosuid,nodev,noexec,relatime shared:23 - cgroup cgroup rw,rdma
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

func TestCopyDir(t *testing.T) {
	type args struct {
		src string
		dst string
	}
	tests := []struct {
		name    string
		args    args
		wantErr bool
	}{
		{
			name: "normal",
			args: args{
				src: "testdata/src",
				dst: "testdata/dst",
			},
			wantErr: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := CopyDir(tt.args.src, tt.args.dst); (err != nil) != tt.wantErr {
				t.Errorf("CopyDir() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}
