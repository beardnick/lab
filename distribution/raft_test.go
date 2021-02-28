package main

import (
	"testing"
	"time"

	"github.com/go-playground/assert/v2"
)

func TestElectionTerm(t *testing.T) {
	n1 := &Node{Term: 1, Timer: time.NewTimer(0)}
	n2 := &Node{Term: 2, Timer: time.NewTimer(0)}
	n3 := &Node{Term: 3, Timer: time.NewTimer(0)}
	n4 := &Node{Term: 4, Timer: time.NewTimer(0)}

	assert.Equal(t, sendVote(n3, n1).Result, Accept)
	assert.Equal(t, n1.Term, n3.Term)
	// 一个term只能进行一次投票
	assert.Equal(t, sendVote(n3, n1).Result, Reject)
	assert.Equal(t, sendVote(n3, n2).Result, Accept)
	assert.Equal(t, n2.Term, n3.Term)
	assert.Equal(t, sendVote(n3, n4).Result, Reject)
}

func sendVote(from, to *Node) (result VoteResult) {
	req := VoteReq{
		Ip:   from.Ip,
		Port: from.Port,
		Term: from.Term,
	}
	return to.voteReq(req)
}
