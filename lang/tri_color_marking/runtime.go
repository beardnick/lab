package main

import (
	"fmt"
	"sync"
)

type Runtime struct {
	memoryLock sync.RWMutex
	memory     []uint16

	objectLock sync.RWMutex
	objects    map[uint16]struct{}

	allocatedLock sync.RWMutex
	allocated     []bool

	gcRootsLock  sync.RWMutex
	gcRoots      map[uint16]struct{}
	size         uint16
	maxAllocSize uint16
}

func NewRuntime(size, maxAllocSize uint16) *Runtime {
	return &Runtime{
		memory:       make([]uint16, size),
		allocated:    make([]bool, size),
		objects:      make(map[uint16]struct{}),
		gcRoots:      make(map[uint16]struct{}),
		size:         size,
		maxAllocSize: maxAllocSize,
	}
}

func (r *Runtime) Malloc(size uint16) (uint16, error) {
	if size > r.maxAllocSize {
		return 0, fmt.Errorf("size %d exceeds max size %d", size, r.maxAllocSize)
	}
	var cnt uint16 = 0
	addr := -1
	r.allocatedLock.Lock()
	defer r.allocatedLock.Unlock()
	for i, v := range r.allocated {
		if v {
			cnt = 0
			addr = -1
			continue
		}
		if cnt == 0 {
			addr = i
		}
		cnt++
		if cnt == size+1 {
			break
		}
	}
	if addr == -1 || cnt != size+1 {
		return 0, fmt.Errorf("no memory block of size %d available", size)
	}
	r.memoryLock.Lock()
	r.memory[addr] = MemoryBlock(size)
	r.memoryLock.Unlock()
	var i uint16
	for i = 0; i < size+1; i++ {
		r.allocated[uint16(addr)+i] = true
	}
	obj := Pointer(uint16(addr + 1))
	r.objectLock.Lock()
	r.objects[obj] = struct{}{}
	r.objectLock.Unlock()
	return obj, nil
}

func (r *Runtime) Free(address uint16) {
	baseAddr := Address(address)
	r.memoryLock.RLock()
	size := MemoryBlockSize(r.memory[baseAddr-1])
	r.memoryLock.RUnlock()
	var i uint16
	r.allocatedLock.Lock()
	defer r.allocatedLock.Unlock()
	for i = 0; i < size+1; i++ {
		r.allocated[baseAddr-1+i] = false
	}
	r.objectLock.Lock()
	delete(r.objects, address)
	r.objectLock.Unlock()

	r.gcRootsLock.Lock()
	delete(r.gcRoots, address)
	r.gcRootsLock.Unlock()
}

func (r *Runtime) Unset(base, offset uint16) {
	baseAddr := Address(base)
	r.memoryLock.Lock()
	size := MemoryBlockSize(r.memory[baseAddr-1])
	if offset >= size {
		panic(fmt.Sprintf("%d out of index %d", offset, size))
	}
	r.memory[baseAddr+offset] = 0
	r.memoryLock.Unlock()
}

func (r *Runtime) Set(base, offset, value uint16) {
	if !IsPointer(value) {
		panic(fmt.Sprintf("value %x is not a pointer", value))
	}
	baseAddr := Address(base)
	r.memoryLock.Lock()
	size := MemoryBlockSize(r.memory[baseAddr-1])
	if offset >= size {
		panic(fmt.Sprintf("%d out of index %d", offset, size))
	}
	r.memory[baseAddr+offset] = value
	r.memoryLock.Unlock()
}

func (r *Runtime) Get(base, offset uint16) uint16 {
	baseAddr := Address(base)
	r.memoryLock.RLock()
	size := MemoryBlockSize(r.memory[baseAddr-1])
	if offset >= size {
		panic(fmt.Sprintf("%d out of index %d", offset, size))
	}
	value := r.memory[baseAddr+offset]
	r.memoryLock.RUnlock()
	return value
}

func (r *Runtime) Pointers(address uint16) []uint16 {
	baseAddr := Address(address)
	r.memoryLock.RLock()
	size := MemoryBlockSize(r.memory[baseAddr-1])
	result := make([]uint16, 0, size)
	var i uint16
	for i = 0; i < size; i++ {
		data := r.memory[baseAddr+i]
		if !IsPointer(data) {
			continue
		}
		result = append(result, data)
	}
	r.memoryLock.RUnlock()
	return result
}

func (r *Runtime) Objects() []uint16 {
	r.objectLock.RLock()
	result := make([]uint16, 0, len(r.objects))
	for obj := range r.objects {
		result = append(result, obj)
	}
	r.objectLock.RUnlock()
	return result
}

func (r *Runtime) MallocGcRoot(size uint16) (uint16, error) {
	obj, err := r.Malloc(size)
	if err != nil {
		return 0, err
	}
	r.gcRootsLock.Lock()
	r.gcRoots[obj] = struct{}{}
	r.gcRootsLock.Unlock()
	return obj, nil
}

func (r *Runtime) GcRoot() []uint16 {
	r.gcRootsLock.RLock()
	result := make([]uint16, 0, len(r.gcRoots))
	for root := range r.gcRoots {
		result = append(result, root)
	}
	r.gcRootsLock.RUnlock()
	return result
}

func (r *Runtime) objectExits(address uint16) bool {
	r.objectLock.RLock()
	_, ok := r.objects[address]
	r.objectLock.RUnlock()
	return ok
}

func (r *Runtime) gcRootExits(address uint16) bool {
	r.gcRootsLock.RLock()
	_, ok := r.gcRoots[address]
	r.gcRootsLock.RUnlock()
	return ok
}

func MemoryBlock(size uint16) uint16 {
	if size > 0x3FFF {
		panic(fmt.Sprintf("size %d exceeds max size 16383", size))
	}
	return 0x8000 + size
}
func MemoryBlockSize(data uint16) uint16 {
	if !IsMemoryBlock(data) {
		panic(fmt.Sprintf("data %x is not a memory block", data))
	}
	return data & 0x3FFF
}

func Pointer(addr uint16) uint16 {
	if addr > 0x3FFF {
		panic(fmt.Sprintf("addr %d exceeds max addr 16383", addr))
	}
	return 0x4000 + addr
}

func IsPointer(data uint16) bool {
	// 0b0100_0000_0000_0000
	return data&0xC000 == 0x4000
}

func IsMemoryBlock(data uint16) bool {
	// 0b1000_0000_0000_0000
	return data&0xC000 == 0x8000
}

func Address(data uint16) uint16 {
	if !IsPointer(data) {
		panic(fmt.Sprintf("data %x is not a pointer", data))
	}
	return data & 0x3FFF
}
