package main

import (
	"github.com/stretchr/testify/assert"
	"sync/atomic"
	"testing"
	"time"
)

func TestBarrier(t *testing.T) {
	barrier([]string{"https://www.baidu.com", "http://www.sina.com", "https://segmentfault.com/"}...)
}

func TestPipeline(t *testing.T) {
	// 1^2 + 2^2 + 3^2 == 14
	assert.Equal(t, 14, <-sum(power(generator(3))))
}

func TestWorkerPool(t *testing.T) {
	bufferSize := 100
	var workerPool = NewWorkerPool(bufferSize)
	workers := 4
	for i := 0; i < workers; i++ {
		workerPool.AddWorker()
	}

	var sum int32
	testFunc := func(i interface{}) {
		n := i.(int32)
		atomic.AddInt32(&sum, n)
	}
	var i, n int32
	n = 1000
	for ; i < n; i++ {
		task := Task{
			i,
			testFunc,
		}
		workerPool.SendTask(task)
	}
	workerPool.Release()
	assert.Equal(t, int32(499500), sum)
}

func TestPubSub(t *testing.T) {
	pub := NewPublisher()
	go pub.start()

	sub1 := NewSubscriber(1)
	Register(sub1, pub)

	sub2 := NewSubscriber(2)
	Register(sub2, pub)

	commands := []int{1, 2, 3, 4, 5, 6, 7, 8, 9}
	for _, c := range commands {
		pub.in <- c
	}

	pub.stop <- struct{}{}
	time.Sleep(time.Second * 1)
}
