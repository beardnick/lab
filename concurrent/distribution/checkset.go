package main

import (
	"fmt"
	"sync"
	"time"
)

var number int = 0

func Set(n int) {
	if n > number {
		// 增加耗时操作可以让更多协程进入if block
		time.Sleep(time.Second)
		number = n
	}
}

func CheckSet(done *sync.WaitGroup, button chan struct{}, n int) {
	defer done.Done()
	<-button
	Set(n)
}

func main() {
	done := sync.WaitGroup{}
	button := make(chan struct{})
	end := 1000
	for i := 0; i < end; i++ {
		done.Add(1)
		go CheckSet(&done, button, i)
	}
	close(button)
	done.Wait()
	fmt.Println("final:", number)
}
