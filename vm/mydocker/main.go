// +build linux

package main

import (
	"bufio"
	"fmt"
	"github.com/spf13/cobra"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

func main() {
	fmt.Println("exec")
	log.SetFlags(log.Llongfile)
	root := &cobra.Command{
		Use:   "mydocker",
		Short: "my simple docker",
		Long:  `a docker create for learning`,
	}
	runCmd := &cobra.Command{
		Use: "run",
		Run: Run,
	}
	runCmd.Flags().Bool("it", false, "run with tty")
	initCmd := &cobra.Command{
		Use: "init",
		Run: Init,
	}
	root.AddCommand(runCmd)
	root.AddCommand(initCmd)
	err := root.Execute()
	if err != nil {
		log.Println(err)
	}
}

func Init(cmd *cobra.Command, args []string) {
	log.Println("init", args)
	// https://github.com/xianlubird/mydocker/issues/58#issuecomment-574059632
	// this is needed in new linux kernel
	// otherwise, the mount ns will not work as expected
	err := syscall.Mount("", "/", "", syscall.MS_PRIVATE|syscall.MS_REC, "")
	if err != nil {
		log.Println(err)
		return
	}
	mFlags := syscall.MS_NOEXEC | syscall.MS_NOSUID | syscall.MS_NODEV
	err = syscall.Mount("proc", "/proc", "proc", uintptr(mFlags), "")
	if err != nil {
		log.Println(err)
		return
	}
	err = syscall.Exec(args[0], args, os.Environ())
	if err != nil {
		fmt.Println(err)
	}
}

func Run(cmd *cobra.Command, args []string) {
	log.Println("run", args)
	if len(args) < 1 {
		log.Println("args is needed")
		return
	}
	tty, err := cmd.Flags().GetBool("it")
	if err != nil {
		log.Println(err)
		return
	}
	args = append([]string{"init"}, args...)
	c := exec.Command("/proc/self/exe", args...)
	if tty {
		c.Stdin = os.Stdin
		c.Stderr = os.Stderr
		c.Stdout = os.Stdout
	}
	c.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags: syscall.CLONE_NEWUTS |
			syscall.CLONE_NEWPID |
			syscall.CLONE_NEWNS |
			syscall.CLONE_NEWNET |
			syscall.CLONE_NEWIPC,
	}
	err = c.Run()
	if err != nil {
		log.Fatal(err)
	}
}

type Subsystem interface {
	LimitCgroup(cgroup, limit string) (err error)
	AddCgroup(cgroup string) (err error)
	RemoveCgroup(cgroup string) (err error)
	AddTaskToCgroup(task, cgroup string) (err error)
	Name() string
}

// a cgroup only for memory control
type MemorySubsystem struct {
}

func (m MemorySubsystem) LimitCgroup(cgroup, limit string) (err error) {
	root, err := GetCgroupRootPath(cgroup, m.Name())
	if err != nil {
		return
	}
	return ioutil.WriteFile(filepath.Join(root, "memory.soft_limit_in_bytes"), []byte(limit), 644)
}

func (m MemorySubsystem) AddCgroup(cgroup string) (err error) {
	// add cgrou just create sub dir
	root, err := GetCgroupRootPath(cgroup, m.Name())
	if err != nil {
		return
	}
	return os.Mkdir(root, 644)
}

func (m MemorySubsystem) RemoveCgroup(cgroup string) (err error) {
	root, err := GetCgroupRootPath(cgroup, m.Name())
	if err != nil {
		return
	}
	return os.RemoveAll(root)
}

func (m MemorySubsystem) AddTaskToCgroup(task, cgroup string) (err error) {
	root, err := GetCgroupRootPath(cgroup, m.Name())
	if err != nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(root, "tasks"), os.O_RDWR, 0755)
	if err != nil {
		return
	}
	defer f.Close()
	_, err = io.WriteString(f, task+"\n")
	return
}

func (m MemorySubsystem) Name() string {
	// cannot simply use default value
	//return "/sys/fs/cgroup/memory"
	return "memory"
}

func GetCgroupRootPath(cgroup, subsys string) (root string, err error) {
	f, err := os.Open("/proc/self/mountinfo")
	if err != nil {
		return
	}
	defer f.Close()
	sub := GetSubsystemPath(f, subsys)
	root = filepath.Join(sub, cgroup)
	return
}

func GetSubsystemPath(mountInfo io.Reader, subSystem string) string {
	scanner := bufio.NewScanner(mountInfo)
	for scanner.Scan() {
		text := scanner.Text()
		fields := strings.Split(text, " ")
		opt := strings.Split(fields[len(fields)-1], ",")[1]
		if opt == subSystem {
			return fields[4]
		}
	}
	return ""
}
