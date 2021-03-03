package main

import (
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/go-playground/assert/v2"
)

func TestElectionTerm(t *testing.T) {
	n1 := &Node{Ip:"127.0.0.1", Port:"9091",Term: 1, timer: time.NewTimer(0)}
	n2 := &Node{Ip:"127.0.0.1", Port:"9092",Term: 2, timer: time.NewTimer(0)}
	n3 := &Node{Ip:"127.0.0.1", Port:"9093",Term: 3, timer: time.NewTimer(0)}
	n4 := &Node{Ip:"127.0.0.1", Port:"9094",Term: 4, timer: time.NewTimer(0)}

	assert.Equal(t, sendVote(n3, n1).Result, Accept)
	assert.Equal(t, n1.Term, n3.Term)
	// 一个term只能进行一次投票
	assert.Equal(t, sendVote(n3, n1).Result, Reject)
	assert.Equal(t, sendVote(n3, n2).Result, Accept)
	assert.Equal(t, n2.Term, n3.Term)
	assert.Equal(t, sendVote(n3, n4).Result, Reject)
}


func TestConcurrentElectionTerm(t *testing.T) {
	n0 := &Node{Ip: "",Port:"",Term: 0, timer: time.NewTimer(0)}
	done := sync.WaitGroup{}
	start := 1
	end := 1000000
	for i := 0; i < end; i++ {
		node := &Node{Port:strconv.Itoa(start + i),Term: start + i , timer: time.NewTimer(0)}
		done.Add(1)
		go concurrentSendVote(&done,node, n0)
	}
	done.Wait()
	assert.Equal(t,n0.Term , start + end - 1)
}


func concurrentSendVote(done *sync.WaitGroup,from, to *Node) (result VoteResult) {
	defer done.Done()
	req := VoteReq{
		Ip:   from.Ip,
		Port: from.Port,
		Term: from.Term,
	}
	result = to.handleReq(req)
	return
}

func sendVote(from, to *Node) (result VoteResult) {
	req := VoteReq{
		Ip:   from.Ip,
		Port: from.Port,
		Term: from.Term,
	}
	return to.handleReq(req)
}
