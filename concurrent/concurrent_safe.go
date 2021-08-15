package main

import (
	"sync"
	"sync/atomic"
)

// 无状态

// 如数学函数
func Add(a, b int) int {
	return a + b
}

// 变量的生命周期在单个线程内
func Swap(a, b *int) {
	c := a // c 变量的生命周期限制在函数内
	a = b
	b = c
}

// 反例 有状态
func NewCounter(cnt int) *Counter {
	return &Counter{cnt}
}

type Counter struct {
	Cnt int
}

// 并发调用同一个counter的Count方法时就会产生并发问题
func (c *Counter) Count() int {
	c.Cnt = c.Cnt + 1
	return c.Cnt
}

type CounterWithLock struct {
	Cnt int
	sync.Mutex
}

func (c *CounterWithLock) Count() int {
	c.Lock()
	defer c.Unlock()
	c.Cnt = c.Cnt + 1
	return c.Cnt
}

func NewCounterWithLock(cnt int) *CounterWithLock {
	return &CounterWithLock{
		Cnt:   0,
		Mutex: sync.Mutex{},
	}

}

type CounterLockFree struct {
	Cnt int64
}

func (c *CounterLockFree) Count() int {
	r := atomic.AddInt64(&c.Cnt, 1)
	return int(r)
}

func NewCounterLockFree(cnt int) *CounterLockFree {
	return &CounterLockFree{
		Cnt: int64(cnt),
	}
}

type CounterSelect struct {
	Cnt   int
	cntC  chan int
	cntR  chan int
	stopC chan int
}

func (c *CounterSelect) Count() int {
	c.cntC <- 1
	return <-c.cntR
}
func (c *CounterSelect) Stop() {
	c.stopC <- 1
}

// 这里的cnt状态限制在同一个goroutine里面,所以也是并发安全的
func (c *CounterSelect) Start() {
	go func() {
		for {
			select {
			case <-c.cntC:
				c.Cnt = c.Cnt + 1
				c.cntR <- c.Cnt
			case <-c.stopC:
				break
			}
		}
	}()

}

func NewCounterSelect(cnt int) *CounterSelect {
	c := &CounterSelect{
		Cnt:   cnt,
		cntC:  make(chan int),
		cntR:  make(chan int),
		stopC: make(chan int),
	}
	return c
}
