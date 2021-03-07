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

func TestHandleReq(t *testing.T) {
	// 并行拉票,是否会有并发问题
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
	// n3.term太小
	assert.Equal(t, sendVote(n3, n4).Result, Reject)
}

func TestHandleHeartBeat(t *testing.T) {

}

func TestCampaignLeader(t *testing.T) {
	n1 := MockNode{&Node{ip: "127.0.0.1", port: "9091", term: 0, timer: time.NewTimer(0)}}
	n2 := MockNode{&Node{ip: "127.0.0.1", port: "9092", term: 0, timer: time.NewTimer(0)}}
	n3 := MockNode{&Node{ip: "127.0.0.1", port: "9093", term: 0, timer: time.NewTimer(0)}}
	n4 := MockNode{&Node{ip: "127.0.0.1", port: "9094", term: 0, timer: time.NewTimer(0)}}
	cluster := MockCluster{}

	// 节点过少
	cluster.nodes = func() (nodes []INode, err error) { return []INode{n1, n2}, nil }
	n1.cluster = cluster
	assert.False(t, n1.CampaignLeader())

	// n1拉票
	cluster.nodes = func() (nodes []INode, err error) { return []INode{n1, n2, n3, n4}, nil }
	n1.cluster = cluster
	assert.True(t, n1.CampaignLeader())

	// n1离线
	cluster.nodes = func() (nodes []INode, err error) { return []INode{OfflineNode{n1}, n2, n3, n4}, nil }
	n2.cluster = cluster
	assert.True(t, n2.CampaignLeader())

	//n2离线,票数始终无法超过半数
	cluster.nodes = func() (nodes []INode, err error) { return []INode{OfflineNode{n1}, OfflineNode{n2}, n3, n4}, nil }
	n3.cluster = cluster
	assert.False(t, n3.CampaignLeader())
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

type MockCluster struct {
	nodes func() (nodes []INode, err error)
}

// 去掉网络通信的node
type MockNode struct {
	*Node
}

func (m MockNode) HeartBeat() (err error) {
	return m.Node.HeartBeat()
}

func (m MockNode) HandleHeartBeat(req VoteReq, respC chan VoteResult) {
	m.Node.handleHeartBeat(req)
}

func (m MockNode) HandleReq(req VoteReq, respC chan VoteResult) {
	respC <- m.Node.handleReq(req)
}

func (m MockNode) HandleResult(result VoteResult) {
	m.Node.HandleResult(result)
}

func (m MockNode) CampaignLeader() (succeed bool) {
	return m.Node.CampaignLeader()
}

func (m MockNode) TimeOut() (timeout <-chan time.Time) {
	return m.Node.TimeOut()
}

func (m MockCluster) Nodes() (nodes []INode, err error) {
	return m.nodes()
}

type OfflineNode struct {
	MockNode
}

func (m OfflineNode) HandleReq(req VoteReq, respC chan VoteResult) {
	time.Sleep(time.Second)
	respC <- VoteResult{Result: Reject}
}

func TestRandTimeout(t *testing.T) {
	set := map[time.Duration]bool{}
	tests := 1000
	for i := 0; i < tests; i++ {
		timeout, err := RandTimeout()
		assert.Nil(t, err)
		set[timeout] = true
	}
	// n个随机数不相等
	assert.Len(t, set, tests)
}
