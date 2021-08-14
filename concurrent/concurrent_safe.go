package main

import (
	"fmt"
	"sync"
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
func NewCounter(cnt int) Counter {
	return Counter{cnt}
}

type Counter struct {
	Cnt int
}

// 并发调用同一个counter的Count方法时就会产生并发问题
func (c *Counter) Count() int {
	c.Cnt = c.Cnt + 1
	return c.Cnt
}

func ConcurrentCounter(n int) int {
	c := NewCounter(0)
	wg := &sync.WaitGroup{}
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(c *Counter) {
			c.Count()
			wg.Done()
		}(&c)
	}
	wg.Wait()
	return c.Cnt
}

func main() {
	r := ConcurrentCounter(1000)
	fmt.Println(r)
}
