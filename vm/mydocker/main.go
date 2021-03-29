// +build linux

package main

import (
	"bufio"
	"errors"
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
	log.SetFlags(log.Llongfile)
	root := &cobra.Command{
		Use:   "mydocker",
		Short: "my simple docker",
		Long:  `a docker create for learning`,
		FParseErrWhitelist: cobra.FParseErrWhitelist{
			UnknownFlags: true,
		},
	}
	runCmd := &cobra.Command{
		Use: "run",
		Run: Run,
		FParseErrWhitelist: cobra.FParseErrWhitelist{
			UnknownFlags: true,
		},
	}
	runCmd.Flags().Bool("it", false, "run with tty")
	runCmd.Flags().StringP("memory", "m", "100m", "limit memory resource")
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
	fmt.Println("init", args)
	// https://github.com/xianlubird/mydocker/issues/58#issuecomment-574059632
	// this is needed in new linux kernel
	// otherwise, the mount ns will not work as expected
	cmds, err := readUserCommand()
	if err != nil {
		log.Println(err)
		return
	}
	log.Println("cmds:", cmds)
	err = syscall.Mount("", "/", "", syscall.MS_PRIVATE|syscall.MS_REC, "")
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
	path, err := exec.LookPath(cmds[0])
	if err != nil {
		log.Println(err)
		return
	}
	err = syscall.Exec(path, cmds, os.Environ())
	if err != nil {
		log.Println(err)
	}
}

func Run(cmd *cobra.Command, args []string) {
	fmt.Println("run", args)
	if len(args) < 1 {
		log.Println("args is needed")
		return
	}
	args = strings.Fields(args[0])
	tty, err := cmd.Flags().GetBool("it")
	if err != nil {
		log.Println(err)
		return
	}
	read, write, err := NewPipe()
	if err != nil {
		log.Println(err)
		return
	}
	c := exec.Command("/proc/self/exe", "init")
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
	limit, err := cmd.Flags().GetString("memory")
	if err != nil {
		log.Println(err)
		return
	}
	cgroup := "mydocker-cgroup"
	memorySubsys := MemorySubsystem{}
	err = memorySubsys.AddCgroup(cgroup)
	if err != nil {
		log.Println(err)
		return
	}
	err = memorySubsys.LimitCgroup(cgroup, limit)
	if err != nil {
		log.Println(err)
		return
	}
	// pass pipe to sub process
	c.ExtraFiles = []*os.File{read}
	err = c.Start()
	if err != nil {
		log.Fatal(err)
	}
	_, err = write.WriteString(strings.Join(args, " "))
	if err != nil {
		log.Println(err)
		return
	}
	// #NOTE don't use defer write.close()
	// write string succeed only after write.close()
	write.Close()
	fmt.Println("pid", c.Process.Pid)
	err = memorySubsys.AddTaskToCgroup(c.Process.Pid, cgroup)
	if err != nil {
		log.Println(err)
		return
	}
	defer memorySubsys.RemoveCgroup(cgroup)
	c.Wait()
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
	// don't use sort_limit_in_bytes
	return ioutil.WriteFile(filepath.Join(root, "memory.limit_in_bytes"), []byte(limit), 644)
}

func (m MemorySubsystem) AddCgroup(cgroup string) (err error) {
	// add cgrou just create sub dir
	root, err := GetCgroupRootPath(cgroup, m.Name())
	if err != nil {
		return
	}
	_, err = os.Stat(root)
	// create when not exists
	if errors.Is(err, os.ErrNotExist) {
		return os.Mkdir(root, 644)
	}
	return
}

func (m MemorySubsystem) RemoveCgroup(cgroup string) (err error) {
	root, err := GetCgroupRootPath(cgroup, m.Name())
	if err != nil {
		return
	}
	return os.RemoveAll(root)
}

func (m MemorySubsystem) AddTaskToCgroup(task int, cgroup string) (err error) {
	root, err := GetCgroupRootPath(cgroup, m.Name())
	if err != nil {
		return
	}
	f, err := os.OpenFile(filepath.Join(root, "tasks"), os.O_RDWR, 0755)
	if err != nil {
		return
	}
	defer f.Close()
	_, err = io.WriteString(f, fmt.Sprintf("%d\n", task))
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
		opt := strings.Split(fields[len(fields)-1], ",")
		if len(opt) < 2 {
			continue
		}
		if opt[1] == subSystem {
			return fields[4]
		}
	}
	return ""
}

func NewPipe() (read, write *os.File, err error) {
	read, write, err = os.Pipe()
	return
}

func readUserCommand() (args []string, err error) {
	// stdin 0 staout 1 stderr 2
	pipe := os.NewFile(uintptr(3), "pipe")
	msg, err := ioutil.ReadAll(pipe)
	if err != nil {
		return
	}
	args = strings.Split(string(msg), " ")
	return
}
