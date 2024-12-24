package main

import (
	"context"
	"sync"
	"testing"

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
	assert.Equal(t, GcInfo{RecycledObjects: 0}, info)
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
	assert.Equal(t, GcInfo{RecycledObjects: 2}, info)
}

func Test_GcLoop(t *testing.T) {
	r := NewRuntime(1000, 10)
	a, err := r.MallocGcRoot(5)
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
	assert.Equal(t, GcInfo{RecycledObjects: 2}, info)
}

func Test_GcLoopNoGarbage(t *testing.T) {
	r := NewRuntime(1000, 10)
	a, err := r.MallocGcRoot(5)
	assert.Nil(t, err)
	b, err := r.Malloc(5)
	assert.Nil(t, err)
	c, err := r.Malloc(5)
	assert.Nil(t, err)
	r.Set(a, 0, b)
	r.Set(b, 0, c)
	r.Set(c, 0, b)

	info := Gc(r)
	assert.Equal(t, GcInfo{RecycledObjects: 0}, info)
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
	assert.Equal(t, GcInfo{RecycledObjects: 0}, info)
}

func Test_GcConcurrently(t *testing.T) {
	r := NewRuntime(10000, 10)
	gcC := make(chan struct{})
	gcf := func(ctx context.Context) {
		for {
			select {
			case <-ctx.Done():
				return
			case <-gcC:
				Gc(r)
			}
		}
	}
	f := func(loop int, wg *sync.WaitGroup) {
		defer wg.Done()
		for i := 0; i < loop; i++ {
			// a(root) -> b -> c
			a, err := r.MallocGcRoot(3)
			assert.Nil(t, err)
			b, err := r.MallocGcRoot(3)
			assert.Nil(t, err)
			c, err := r.MallocGcRoot(3)
			assert.Nil(t, err)
			r.Set(a, 0, b)
			r.Set(b, 0, c)
			r.RemoveGcRoot(b)
			r.RemoveGcRoot(c)

			r.Set(a, 1, c)
			r.Unset(b, 0)
			r.Unset(a, 0)
			assert.True(t, r.objectExits(a))
			assert.True(t, r.objectExits(c))
			r.RemoveGcRoot(a)
			gcC <- struct{}{}
		}
	}
	cnt := 10
	loop := 1000
	wg := &sync.WaitGroup{}
	wg.Add(cnt)

	ctx, cancel := context.WithCancel(context.Background())
	go gcf(ctx)
	for i := 0; i < cnt; i++ {
		go f(loop, wg)
	}
	wg.Wait()
	cancel()
}

func Test_GcWithConcurrentWrite(t *testing.T) {
	r := NewRuntime(10000, 10)
	s := NewState(r)

	// a(root) -> b -> c
	a, err := r.MallocGcRoot(3)
	assert.Nil(t, err)
	b, err := r.Malloc(3)
	assert.Nil(t, err)
	c, err := r.Malloc(3)
	assert.Nil(t, err)
	r.Set(a, 0, b)
	r.Set(b, 0, c)
	s.gcInitStage()

	s.scan()
	// a(black) -> b(gray) -> c(white)

	r.Set(a, 1, c)
	// a(black) -> b(gray)  c(gray due to write barrier)
	// |					  /\
	// |----------------------|
	r.Unset(b, 0)

	s.scan()
	// a(black) -> b(black)  c(black)
	// |					  /\
	// |----------------------|

	s.stopMarkStage()
	s.recycleStage()

	assert.True(t, r.objectExits(b))
	assert.True(t, r.objectExits(a))
	assert.True(t, r.objectExits(c))
}
