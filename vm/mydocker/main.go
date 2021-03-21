// +build linux

package main

import (
	"fmt"
	"github.com/spf13/cobra"
	"log"
	"os"
	"os/exec"
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
