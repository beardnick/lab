// +build linux

package main

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/google/uuid"
	"github.com/spf13/cobra"
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
	err := setUpMount()
	if err != nil {
		log.Println(err)
		return
	}
	fmt.Println("init", args)
	cmds, err := readUserCommand()
	if err != nil {
		log.Println(err)
		return
	}
	log.Println("cmds:", cmds)
	path, err := exec.LookPath(cmds[0])
	if err != nil {
		log.Println(err)
		return
	}
	fmt.Println("look up path", path)
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
	init := NewInitCommand()
	args = strings.Fields(args[0])
	err := SendArgs(init, args)
	if err != nil {
		log.Println(err)
		return
	}
	tty, err := cmd.Flags().GetBool("it")
	if err != nil {
		log.Println(err)
		return
	}
	if tty {
		SetTty(init)
	}
	init.Dir = "/data/busybox"
	err = init.Start()
	if err != nil {
		log.Fatal(err)
		return
	}
	limit, err := cmd.Flags().GetString("memory")
	if err != nil {
		log.Println(err)
		return
	}
	cgroup := "mydocker-cgroup"
	subsys, err := LimitMemory(init.Process.Pid, cgroup, limit)
	if err != nil {
		log.Println(err)
		return
	}
	defer subsys.RemoveCgroup(cgroup)
	init.Wait()
}

func NewInitCommand() *exec.Cmd {
	c := exec.Command("/proc/self/exe", "init")
	c.SysProcAttr = &syscall.SysProcAttr{
		Cloneflags: syscall.CLONE_NEWUTS |
			syscall.CLONE_NEWPID |
			syscall.CLONE_NEWNS |
			syscall.CLONE_NEWNET |
			syscall.CLONE_NEWIPC,
	}
	return c
}
func SetTty(c *exec.Cmd) {
	c.Stdin = os.Stdin
	c.Stderr = os.Stderr
	c.Stdout = os.Stdout
}
func LimitMemory(pid int, cgroup, limit string) (subsys MemorySubsystem, err error) {
	memorySubsys := MemorySubsystem{}
	err = memorySubsys.AddCgroup(cgroup)
	if err != nil {
		return
	}
	err = memorySubsys.LimitCgroup(cgroup, limit)
	if err != nil {
		return
	}
	err = memorySubsys.AddTaskToCgroup(pid, cgroup)
	return
}
func SendArgs(child *exec.Cmd, args []string) (err error) {
	read, write, err := NewPipe()
	if err != nil {
		return
	}
	child.ExtraFiles = []*os.File{read}
	_, err = write.WriteString(strings.Join(args, " "))
	if err != nil {
		return
	}
	err = write.Close()
	return
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

func pivotRoot(root string) (err error) {
	fmt.Println("pivotroot", root)
	// todo: why mount the same root?
	err = syscall.Mount(root, root, "bind", syscall.MS_BIND|syscall.MS_REC, "")
	if err != nil {
		return
	}
	pivotDir := filepath.Join(root, ".pivot_root")
	err = os.Mkdir(pivotDir, 0777)
	if err != nil {
		return
	}
	// set root as newroot
	// put old root to pivotDir
	err = syscall.PivotRoot(root, pivotDir)
	if err != nil {
		return
	}
	err = syscall.Chdir("/")
	if err != nil {
		return
	}
	pivotDir = filepath.Join("/", ".pivot_root")
	err = syscall.Unmount(pivotDir, syscall.MNT_DETACH)
	if err != nil {
		return
	}
	err = os.Remove(pivotDir)
	return
}

func setUpMount() (err error) {
	// https://github.com/xianlubird/mydocker/issues/58#issuecomment-574059632
	// this is needed in new linux kernel
	// otherwise, the mount ns will not work as expected
	err = syscall.Mount("", "/", "", syscall.MS_PRIVATE|syscall.MS_REC, "")
	if err != nil {
		return
	}
	workspace := filepath.Join("/data", uuid.NewString())
	err = NewWorkspace(workspace, "/data/busybox")
	if err != nil {
		return
	}
	fmt.Println("workspace", workspace)
	err = pivotRoot(filepath.Join(workspace, "merger"))
	if err != nil {
		return
	}
	defaultFlag := syscall.MS_NOEXEC | syscall.MS_NOSUID | syscall.MS_NODEV
	err = syscall.Mount("proc", "/proc", "proc", uintptr(defaultFlag), "")
	if err != nil {
		return
	}
	err = syscall.Mount("tmpfs", "/dev", "tmpfs", syscall.MS_NOSUID|syscall.MS_STRICTATIME, "mode=755")
	return
}

func NewWorkspace(dir, image string) (err error) {
	err = MkOverlayDir(dir)
	if err != nil {
		return
	}
	merger := filepath.Join(dir, "/merger")
	lower := filepath.Join(dir, "/lower")
	upper := filepath.Join(dir, "/upper")
	worker := filepath.Join(dir, "/worker")
	fmt.Println("merger", merger)
	err = CopyDir(image, lower)
	if err != nil {
		return
	}
	defaultFlag := syscall.MS_NOEXEC | syscall.MS_NOSUID | syscall.MS_NODEV
	err = syscall.Mount("overlayfs:/overlay", merger, "overlay", uintptr(defaultFlag), fmt.Sprintf("lowerdir=%s,upperdir=%s,workdir=%s", lower, upper, worker))
	return
}

func MkOverlayDir(dir string) (err error) {
	dirs := []string{"lower", "upper", "merger", "worker"}
	for _, v := range dirs {
		err = os.MkdirAll(filepath.Join(dir, v), 0777)
		if err != nil {
			return
		}
	}
	return
}

func CopyDir(src string, dst string) error {
	var err error
	var fds []os.FileInfo
	var srcinfo os.FileInfo

	if srcinfo, err = os.Stat(src); err != nil {
		return err
	}

	if err = os.MkdirAll(dst, srcinfo.Mode()); err != nil {
		return err
	}

	if fds, err = ioutil.ReadDir(src); err != nil {
		return err
	}
	for _, fd := range fds {
		srcfp := filepath.Join(src, fd.Name())
		dstfp := filepath.Join(dst, fd.Name())

		if fd.IsDir() {
			if err = CopyDir(srcfp, dstfp); err != nil {
				fmt.Println(err)
			}
		} else {
			if err = CopyFile(srcfp, dstfp); err != nil {
				fmt.Println(err)
			}
		}
	}
	return nil
}

// File copies a single file from src to dst
func CopyFile(src, dst string) error {
	var err error
	var srcfd *os.File
	var dstfd *os.File
	var srcinfo os.FileInfo

	if srcfd, err = os.Open(src); err != nil {
		return err
	}
	defer srcfd.Close()

	if dstfd, err = os.Create(dst); err != nil {
		return err
	}
	defer dstfd.Close()

	if _, err = io.Copy(dstfd, srcfd); err != nil {
		return err
	}
	if srcinfo, err = os.Stat(src); err != nil {
		return err
	}
	return os.Chmod(dst, srcinfo.Mode())
}
