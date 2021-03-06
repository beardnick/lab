package main

import (
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/go-playground/assert/v2"
)

func TestMain(m *testing.M) {
	SetTestMode(true)
	m.Run()
}

func TestElectionTerm(t *testing.T) {
	n1 := &Node{ip: "127.0.0.1", port: "9091", term: 1, timer: time.NewTimer(0)}
	n2 := &Node{ip: "127.0.0.1", port: "9092", term: 2, timer: time.NewTimer(0)}
	n3 := &Node{ip: "127.0.0.1", port: "9093", term: 3, timer: time.NewTimer(0)}
	n4 := &Node{ip: "127.0.0.1", port: "9094", term: 4, timer: time.NewTimer(0)}

	assert.Equal(t, sendVote(n3, n1).Result, Accept)
	assert.Equal(t, n1.term, n3.term)
	// 一个term只能进行一次投票
	assert.Equal(t, sendVote(n3, n1).Result, Reject)
	assert.Equal(t, sendVote(n3, n2).Result, Accept)
	assert.Equal(t, n2.term, n3.term)
	assert.Equal(t, sendVote(n3, n4).Result, Reject)
}

func TestConcurrentElectionTerm(t *testing.T) {
	n0 := &Node{ip: "", port: "", term: 0, timer: time.NewTimer(0)}
	done := sync.WaitGroup{}
	start := 1
	end := 100000
	button := make(chan struct{})
	for i := 0; i < end; i++ {
		node := &Node{port: strconv.Itoa(start + i), term: start + end - 1 - i, timer: time.NewTimer(0)}
		done.Add(1)
		go concurrentSendVote(&done, button, node, n0)
	}
	close(button)
	done.Wait()
	assert.Equal(t, n0.term, start+end-1)
}

func concurrentSendVote(done *sync.WaitGroup, button chan struct{}, from, to *Node) (result VoteResult) {
	defer done.Done()
	req := VoteReq{
		Ip:   from.ip,
		Port: from.port,
		Term: from.term,
	}
	<-button
	result = to.handleReq(req)
	return
}

func sendVote(from, to *Node) (result VoteResult) {
	req := VoteReq{
		Ip:   from.ip,
		Port: from.port,
		Term: from.term,
	}
	return to.handleReq(req)
}
