package main

import (
	"github.com/stretchr/testify/assert"
	"strconv"
	"sync"
	"testing"
	"time"
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

//func TestElection(t *testing.T) {
//	n1 := &Node{ip: "127.0.0.1", port: "9091", term: 0, timer: time.NewTimer(1)}
//	n2 := &Node{ip: "127.0.0.1", port: "9092", term: 0, timer: time.NewTimer(2)}
//	n3 := &Node{ip: "127.0.0.1", port: "9093", term: 0, timer: time.NewTimer(3)}
//	n4 := &Node{ip: "127.0.0.1", port: "9094", term: 0, timer: time.NewTimer(4)}
//}

type MockCluster struct {
	nodes func() (nodes []INode, err error)
}

type MockNode struct {
	heartBeat       func() (err error)
	handleHeartBeat func(req VoteReq, respC chan VoteResult)
	handleReq       func(req VoteReq, respC chan VoteResult)
	handleResult    func(result VoteResult)
	campaignLeader  func() (succeed bool)
	timeOut         func() (timeout <-chan time.Time)
}

func (m MockNode) HeartBeat() (err error) {
	return m.heartBeat()
}

func (m MockNode) HandleHeartBeat(req VoteReq, respC chan VoteResult) {
	m.handleHeartBeat(req, respC)
}

func (m MockNode) HandleReq(req VoteReq, respC chan VoteResult) {
	m.handleReq(req, respC)
}

func (m MockNode) HandleResult(result VoteResult) {
	m.handleResult(result)
}

func (m MockNode) CampaignLeader() (succeed bool) {
	return m.campaignLeader()
}

func (m MockNode) TimeOut() (timeout <-chan time.Time) {
	return m.timeOut()
}

func (m MockCluster) Nodes() (nodes []INode, err error) {
	return m.nodes()
}

// 去掉了网络通信后的node
func genMockNode(n *Node) MockNode {
	node := MockNode{}
	node.heartBeat = func() (err error) { return n.HeartBeat() }
	node.handleHeartBeat = func(req VoteReq, respC chan VoteResult) { n.handleHeartBeat(req) }
	node.handleReq = func(req VoteReq, respC chan VoteResult) { respC <- n.handleReq(req) }
	node.handleResult = func(result VoteResult) { n.HandleResult(result) }
	node.campaignLeader = func() (succeed bool) { return n.CampaignLeader() }
	node.timeOut = func() (timeout <-chan time.Time) { return n.TimeOut() }
	return node
}

func TestCampaignLeader(t *testing.T) {
	n1 := &Node{ip: "127.0.0.1", port: "9091", term: 0, timer: time.NewTimer(0)}
	n2 := &Node{ip: "127.0.0.1", port: "9092", term: 0, timer: time.NewTimer(0)}
	n3 := &Node{ip: "127.0.0.1", port: "9093", term: 0, timer: time.NewTimer(0)}
	n4 := &Node{ip: "127.0.0.1", port: "9094", term: 0, timer: time.NewTimer(0)}
	mn1 := genMockNode(n1)
	mn2 := genMockNode(n2)
	mn3 := genMockNode(n3)
	mn4 := genMockNode(n4)
	cluster := MockCluster{}
	cluster.nodes = func() (nodes []INode, err error) { return []INode{mn1, mn2, mn3, mn4}, nil }
	n1.cluster = cluster
	n2.cluster = cluster
	n3.cluster = cluster
	n4.cluster = cluster

	assert.True(t, mn1.CampaignLeader())
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
