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
	// counter with cnt is concurrent unsafe
	assert.NotEqual(t, c.Cnt, times)

	clock := NewCounterWithLock(0)
	ConcurrentInvoker(times, func() {
		clock.Count()
	})
	// counter with lock is concurrent unsafe
	assert.Equal(t, clock.Cnt, times)

	clockFree := NewCounterLockFree(0)
	ConcurrentInvoker(times, func() {
		clockFree.Count()
	})
	assert.Equal(t, int(clockFree.Cnt), times)
}
