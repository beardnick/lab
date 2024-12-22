package main

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func Test_Pointer(t *testing.T) {
	assert.True(t, IsPointer(uint16(0x4000)))
	assert.False(t, IsPointer(uint16(0x0000)))
	assert.False(t, IsPointer(uint16(0x1000)))
	assert.True(t, IsPointer(uint16(0x5000)))
	assert.True(t, IsPointer(Pointer(1233)))
	assert.Equal(t, Address(Pointer(1233)), uint16(1233))
}

func Test_MemoryBlock(t *testing.T) {
	assert.True(t, IsMemoryBlock(MemoryBlock(10)))
	assert.Equal(t, MemoryBlockSize(MemoryBlock(10)), uint16(10))
}

func Test_Malloc(t *testing.T) {
	r := NewRuntime(4, 10)
	a, err := r.Malloc(1)
	assert.Nil(t, err)
	_, err = r.Malloc(1)
	assert.Nil(t, err)
	_, err = r.Malloc(1)
	assert.NotNil(t, err)

	r.Free(a)
	_, err = r.Malloc(1)
	assert.Nil(t, err)
	_, err = r.Malloc(1)
	assert.NotNil(t, err)
}

func Test_RuntimeConcurrently(t *testing.T) {
	r := NewRuntime(1000, 10)
	f := func(loop int, wg *sync.WaitGroup) {
		defer wg.Done()
		for i := 0; i < loop; i++ {
			a, err := r.Malloc(5)
			assert.Nil(t, err)
			b, err := r.Malloc(5)
			assert.Nil(t, err)
			c, err := r.MallocGcRoot(5)
			assert.Nil(t, err)
			d, err := r.MallocGcRoot(5)
			assert.Nil(t, err)
			r.Set(a, 0, b)
			assert.Equal(t, b, r.Get(a, 0))
			assert.True(t, r.objectExits(a))
			assert.True(t, r.objectExits(b))
			assert.True(t, r.gcRootExits(c))
			assert.True(t, r.gcRootExits(d))
			r.Free(b)
			r.Free(a)
			r.Free(c)
			r.Free(d)
		}
	}
	cnt := 10
	loop := 1000
	wg := &sync.WaitGroup{}
	wg.Add(cnt)
	for i := 0; i < cnt; i++ {
		go f(loop, wg)
	}
	wg.Wait()
}
