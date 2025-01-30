package main

import (
	"github.com/stretchr/testify/assert"
	"sync"
	"testing"
)

func ConcurrentInvoker(n int, f func()) {
	wg := &sync.WaitGroup{}
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			f()
			wg.Done()
		}()
	}
	wg.Wait()
}

func TestCounter_Count(t *testing.T) {
	times := 100000
	c := NewCounter(0)
	ConcurrentInvoker(times, func() {
		c.Count()
	})
	// 非并发安全
	assert.NotEqual(t, c.Cnt, times)

	clock := NewCounterWithLock(0)
	ConcurrentInvoker(times, func() {
		clock.Count()
	})
	assert.Equal(t, clock.Cnt, times)

	clockFree := NewCounterLockFree(0)
	ConcurrentInvoker(times, func() {
		clockFree.Count()
	})
	assert.Equal(t, int(clockFree.Cnt), times)

	cSelect := NewCounterSelect(0)
	cSelect.Start()
	defer cSelect.Stop()
	ConcurrentInvoker(times, func() {
		cSelect.Count()
	})
	assert.Equal(t, int(cSelect.Cnt), times)
}
