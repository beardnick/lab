// +build linux

package main

import (
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
)

const cgroupMemMount = "/sys/fs/cgroup/memory"

func main() {
	if os.Args[0] == "/proc/self/exe" {
		// container process
		fmt.Println("current pid", syscall.Getpid())
		cmd := exec.Command("sh", "-c", `stress --vm-bytes 100m --vm-keep -m 1`)
		//cmd := exec.Command("sh", "-c", `free -h`)
		cmd.SysProcAttr = &syscall.SysProcAttr{}
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		err := cmd.Run()
		if err != nil {
			log.Fatal(err)
			os.Exit(1)
		}
		return
	}
	// self process
	cmd := exec.Command("/proc/self/exe")
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags: syscall.CLONE_NEWUTS | syscall.CLONE_NEWPID | syscall.CLONE_NEWNS,
	}
	cmd.Stdin = os.Stdin
	cmd.Stderr = os.Stderr
	cmd.Stdout = os.Stdout
	err := cmd.Start()
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("pid", cmd.Process.Pid)
	cgroup := filepath.Join(cgroupMemMount, "memlimit")
	// create cgroup
	os.Mkdir(cgroup, 0755)
	// add current process to cgroup
	ioutil.WriteFile(filepath.Join(cgroup, "tasks"), []byte(strconv.Itoa(cmd.Process.Pid)), 0644)
	// limit process resource
	ioutil.WriteFile(filepath.Join(cgroup, "memory.limit_in_bytes"), []byte("100m"), 0644)
	cmd.Process.Wait()
}
