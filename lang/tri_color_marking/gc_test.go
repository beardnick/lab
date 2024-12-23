package main

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func Test_GcNoGarbage(t *testing.T) {
	r := NewRuntime(1000, 10)
	a, err := r.MallocGcRoot(5)
	assert.Nil(t, err)
	b, err := r.Malloc(5)
	assert.Nil(t, err)
	c, err := r.Malloc(5)
	assert.Nil(t, err)
	r.Set(a, 0, b)
	r.Set(b, 0, c)

	info := Gc(r)
	assert.Equal(t, GcInfo{0}, info)
}

func Test_GcChain(t *testing.T) {
	r := NewRuntime(1000, 10)
	a, err := r.MallocGcRoot(5)
	assert.Nil(t, err)
	b, err := r.Malloc(5)
	assert.Nil(t, err)
	c, err := r.Malloc(5)
	assert.Nil(t, err)
	r.Set(a, 0, b)
	r.Set(b, 0, c)
	r.Unset(a, 0)

	info := Gc(r)
	assert.Equal(t, GcInfo{2}, info)
}

func Test_GcLoop(t *testing.T) {
	r := NewRuntime(1000, 10)
	a, err := r.Malloc(5)
	assert.Nil(t, err)
	b, err := r.Malloc(5)
	assert.Nil(t, err)
	c, err := r.Malloc(5)
	assert.Nil(t, err)
	r.Set(a, 0, b)
	r.Set(b, 0, c)
	r.Set(c, 0, b)
	r.Unset(a, 0)

	info := Gc(r)
	assert.Equal(t, GcInfo{2}, info)
}

func Test_GcLoopNoGarbage(t *testing.T) {
	r := NewRuntime(1000, 10)
	a, err := r.Malloc(5)
	assert.Nil(t, err)
	b, err := r.Malloc(5)
	assert.Nil(t, err)
	c, err := r.Malloc(5)
	assert.Nil(t, err)
	r.Set(a, 0, b)
	r.Set(b, 0, c)
	r.Set(c, 0, b)

	info := Gc(r)
	assert.Equal(t, GcInfo{0}, info)
}

func Test_GcMultipleGcRoots(t *testing.T) {
	r := NewRuntime(1000, 10)
	a, err := r.MallocGcRoot(5)
	assert.Nil(t, err)
	b, err := r.Malloc(5)
	assert.Nil(t, err)
	c, err := r.Malloc(5)
	assert.Nil(t, err)
	d, err := r.MallocGcRoot(5)
	assert.Nil(t, err)
	r.Set(a, 0, b)
	r.Set(b, 0, c)
	r.Set(c, 0, b)
	r.Set(d, 0, c)
	r.Unset(a, 0)

	info := Gc(r)
	assert.Equal(t, GcInfo{0}, info)
}

func Test_GcConcurrently(t *testing.T) {
	r := NewRuntime(1000, 10)
	gcf := func(ctx context.Context, interval time.Duration) {
		for {
			select {
			case <-ctx.Done():
				return
			default:
				time.Sleep(interval)
				Gc(r)
			}
		}
	}
	f := func(loop int, wg *sync.WaitGroup) {
		defer wg.Done()
	}
	cnt := 10
	loop := 100
	duration := time.Millisecond
	wg := &sync.WaitGroup{}
	wg.Add(cnt + 1)

	ctx, cancel := context.WithCancel(context.Background())
	go gcf(ctx, duration)
	for i := 0; i < cnt; i++ {
		go f(loop, wg)
	}
	wg.Wait()
	cancel()
}
