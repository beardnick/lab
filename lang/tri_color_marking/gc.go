package main

import (
	"log"
	"sync"
)

type Color int

const (
	ColorWhite Color = iota
	ColorGray
	ColorBlack
)

type GcInfo struct {
	Count int
}

func GcInit(r *Runtime) *State {
	s := NewState(r)
	s.r.StopTheWorld()
	defer s.r.ResumeTheWorld()
	log.Println("gc init")
	s.Init()
	return s
}

type State struct {
	sync.Mutex
	r         *Runtime
	grayNodes []uint16
	color     sync.Map
}

func NewState(r *Runtime) *State {
	return &State{
		r:         r,
		grayNodes: []uint16{},
		color:     sync.Map{},
	}
}

func (s *State) Init() {
	gcRoots := s.r.gcRoot()
	s.grayNodes = make([]uint16, len(gcRoots))
	copy(s.grayNodes, gcRoots)
	for _, v := range gcRoots {
		s.color.Store(v, ColorGray)
	}
	objs := s.r.objectSlice()
	for _, o := range objs {
		if _, ok := s.color.Load(o); ok {
			continue
		}
		s.color.Store(o, ColorWhite)
	}

	s.AddBarrier()
}

func (s *State) Start() GcInfo {
	log.Println("gc mark start")
	for !s.scan() {
	}
	s.r.StopTheWorld()
	s.r.unsetBarrier()
	s.r.ResumeTheWorld()
	log.Println("gc recycle start")
	cnt := 0
	for _, v := range s.r.Objects() {
		if color, ok := s.color.Load(v); ok && color == ColorWhite {
			log.Printf("gc recycle %d\n", v)
			s.r.Free(v)
			cnt += 1
		}
	}
	log.Println("gc end")
	return GcInfo{Count: cnt}
}

func (s *State) scan() (over bool) {
	log.Println("gc scan")
	grayNodes := s.GetAllGrays()
	for _, o := range grayNodes {
		log.Printf("mark %d as black\n", o)
		s.color.Store(o, ColorBlack)
		children := s.r.Pointers(o)
		for _, v := range children {
			if color, ok := s.color.Load(v); ok && color != ColorWhite {
				// avoid circles in the graph
				continue
			}
			s.AddGray(v)
		}
	}
	return len(grayNodes) == 0
}

func (s *State) AddBarrier() {
	// s.r.AddDeleteBarrier(o, func(r *Runtime, address uint16) {
	// 	s.color[address] = ColorBlack
	// })
	s.r.addWriteBarrier(func(r *Runtime, address uint16) {
		log.Printf("write barrier %d\n", address)
		s.AddGray(address)
	})
}

func (s *State) AddGray(obj uint16) {
	s.Lock()
	defer s.Unlock()
	s.grayNodes = append(s.grayNodes, obj)
	log.Printf("mark %d as gray\n", obj)
	s.color.Store(obj, ColorGray)
}

func (s *State) GetAllGrays() []uint16 {
	s.Lock()
	defer s.Unlock()
	return s.grayNodes
}
