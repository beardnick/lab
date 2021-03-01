package main

import (
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-resty/resty/v2"
)

// term一半小于candicate，一半大于candidate,选举结果如何?

type RaftRole int

const (
	Follower RaftRole = iota
	Leader
	Candidate
)

func (r RaftRole) String() string {
	switch r {
	case Follower:
		return "Follower"
	case Leader:
		return "Leader"
	case Candidate:
		return "Candidate"
	}
	return "Unknown raft role"
}

type Console struct {
	Ip   string
	Port string
}

type Cluster struct {
	Console Console
}

type Instance struct {
	Ip   string `json:"ip,omitempty"`
	Port string `json:"port,omitempty"`
}

type Response struct {
	Code int         `json:"code,omitempty"`
	Data interface{} `json:"data,omitempty"`
	Msg  string      `json:"msg,omitempty"`
}

type Node struct {
	sync.Mutex
	role             RaftRole      `json:"role,omitempty"`
	timeout          time.Duration `json:"time_out,omitempty"`
	HeartBeatTimeOut time.Duration `json:"heart_beat_time_out,omitempty"`
	Term             int           `json:"term,omitempty"`
	Ip               string        `json:"ip,omitempty"`
	Port             string        `json:"port,omitempty"`
	Score            int           `json:"score,omitempty"`
	Cluster          Cluster       `json:"-"`
	timer            *time.Timer   `json:"-"`
}

func (n *Node) HandleHeartBeat(req VoteReq, respC chan VoteResult) {
	if req.Ip == n.Ip && req.Port == n.Port {
		return
	}
	_, _ = resty.New().SetTimeout(time.Second).R().SetBody(req).
		Post(fmt.Sprintf("http://%s:%s/heartbeat", n.Ip, n.Port))
}

func (n *Node) TimeOut() (timeout <-chan time.Time) {
	n.timer.Reset(n.timeout)
	return n.timer.C
}

func (n *Node) CampaignLeader() (succeed bool) {
	// todo: 处理同时被选中的情况
	//if voted {
	//    timeout, err := RandTimeout()
	//    if err != nil {
	//    }
	//    continue
	//}
	n.role = Candidate
	n.Term = n.Term + 1
	nodes, err := n.Cluster.Console.Nodes()
	if err != nil {
		fmt.Println(err)
		return
	}
	respC := make(chan VoteResult)
	for _, peer := range nodes {
		req := VoteReq{Ip: n.Ip, Port: n.Port, Term: n.Term}
		go peer.HandleReq(req, respC)
	}
	cnt := 0
FOR:
	for {
		select {
		case result := <-respC:
			n.HandleResult(result)
			// todo:怎样判断所有请求是否已经到达
			// todo:判断超过半数票之后直接退出是否可行
			cnt = cnt + 1
			fmt.Println("cnt:", cnt, "score:", n.Score)
			if cnt == len(nodes) {
				break FOR
			}
		}
	}
	if n.Score > len(nodes)/2 {
		n.role = Leader
	}
	succeed = n.role == Leader
	return
}

const (
	Reject = iota
	Accept
)

type VoteResult struct {
	Result int `json:"result,omitempty"`
}
type VoteReq struct {
	Ip   string `json:"ip,omitempty"`
	Port string `json:"port,omitempty"`
	Term int    `json:"term,omitempty"`
}

func NodeHandler(node *Node) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.JSON(http.StatusOK, Response{
			Data: node,
		})
		return
	}
}

func HeartBeatHandler(node *Node) gin.HandlerFunc {
	return func(c *gin.Context) {
		v := VoteReq{}
		err := c.ShouldBindJSON(&v)
		if err != nil {
			c.JSON(http.StatusOK, Response{
				Code: 1,
				Msg:  err.Error(),
			})
			return
		}
		node.Term = v.Term
		node.timer.Reset(node.timeout)
		c.JSON(http.StatusOK, Response{})
		return
	}
}

func VoteHandler(node *Node) gin.HandlerFunc {
	return func(c *gin.Context) {
		v := VoteReq{}
		err := c.ShouldBindJSON(&v)
		if err != nil {
			c.JSON(http.StatusOK, Response{
				Code: 1,
				Msg:  err.Error(),
			})
			return
		}
		result := node.handleReq(v)
		c.JSON(http.StatusOK, Response{
			Data: result,
		})
	}
}

func (c Console) Nodes() (insts []INode, err error) {
	//insts = []*Node{}
	resp := Response{
		Data: &insts,
	}
	_, err = resty.New().SetTimeout(time.Second).R().
		SetResult(&resp).
		Get(fmt.Sprintf("http://%s:%s/nodes", c.Ip, c.Port))
	if err != nil {
		return
	}
	if resp.Code != 0 {
		err = errors.New(resp.Msg)
		return
	}
	return
}

func (c Console) Register(node Node) (err error) {
	r := Response{}
	n := Instance{
		Ip:   node.Ip,
		Port: node.Port,
	}
	resp, err := resty.New().
		SetTimeout(time.Second).
		SetRetryCount(20).
		R().
		SetBody(n).
		SetResult(&r).
		Post(fmt.Sprintf("http://%s:%s/register", c.Ip, c.Port))
	if err != nil {
		return
	}
	if resp.StatusCode() != 200 || r.Code != 0 {
		err = errors.New(r.Msg)
		return
	}
	return
}

func (n *Node) HeartBeatLoop() {
	t := time.NewTicker(time.Second)
	for {
		<-t.C
		err := n.HeartBeat()
		if err != nil {
			fmt.Println(err)
		}
	}

}

func (n *Node) HeartBeat() (err error) {
	nodes, err := n.Cluster.Console.Nodes()
	if err != nil {
		return
	}
	if len(nodes) < 3 {
		err = fmt.Errorf("at least 3 nodes but only %d", len(nodes))
		return
	}
	respC := make(chan VoteResult)
	for _, i := range nodes {
		req := VoteReq{Ip: n.Ip, Port: n.Port, Term: n.Term}
		go i.HandleHeartBeat(req, respC)
	}
	return
}

func vote(req VoteReq, inst Instance, respC chan VoteResult) {
	var (
		err error
	)
	result := VoteResult{}
	resp := Response{
		Data: &result,
	}
	_, err = resty.New().
		SetTimeout(time.Second).
		SetRetryCount(3).
		R().
		SetBody(req).
		SetResult(&resp).
		Post(fmt.Sprintf("http://%s:%s/vote", inst.Ip, inst.Port))
	if err != nil || resp.Code != 0 {
		fmt.Println("vote err:", err, "resp:", resp)
		result.Result = Reject
	}
	respC <- result
}

func (n *Node) HandleReq(req VoteReq, respC chan VoteResult) {
	var (
		err error
	)
	result := VoteResult{}
	resp := Response{
		Data: &result,
	}
	_, err = resty.New().
		SetTimeout(time.Second).
		SetRetryCount(3).
		R().
		SetBody(req).
		SetResult(&resp).
		Post(fmt.Sprintf("http://%s:%s/vote", n.Ip, n.Port))
	if err != nil || resp.Code != 0 {
		fmt.Println("vote err:", err, "resp:", resp)
		result.Result = Reject
	}
	respC <- result
}

func (n *Node) handleReq(req VoteReq) (result VoteResult) {
	// todo:这个node修改的操作是互斥的,这里会不会有并发问题
	if req.Ip == n.Ip && req.Port == n.Port {
		result.Result = Accept
		return
	}
	if req.Term > n.Term {
		n.role = Follower
		n.Term = req.Term
		//n.Timer.Reset(n.TimeOut)
		n.timer.Reset(n.timeout)
		result.Result = Accept
		return
	}
	result.Result = Reject
	return
}

//func (n *Node) vote(peer *Node) (result VoteResult) {
//    req := VoteReq{
//        Ip:   n.Ip,
//        Port: n.Port,
//        Term: n.Term,
//    }
//    return peer.voteReq(req)
//}

func (n *Node) HandleResult(result VoteResult) {
	if result.Result == Accept {
		n.Score = n.Score + 1
	}
}

//func Vote(n *Node) (err error) {
//	req := VoteReq{
//		Ip:   n.Ip,
//		Port: n.Port,
//		Term: n.Term,
//	}
//	insts, err := n.Cluster.Console.Nodes()
//	if err != nil {
//		return
//	}
//	if len(insts) < 3 {
//		err = fmt.Errorf("at least 3 nodes but only %d", len(insts))
//		return
//	}
//	respC := make(chan VoteResult)
//	for _, i := range insts {
//		if i.Ip == n.Ip && i.Port == n.Port {
//			continue
//		}
//		go vote(req, i, respC)
//	}
//	cnt := 0
//FOR:
//	for {
//		select {
//		case result := <-respC:
//			n.HandleResult(result)
//			// todo:怎样判断所有请求是否已经到达
//			// todo:判断超过半数票之后直接退出是否可行
//			cnt = cnt + 1
//			fmt.Println("cnt:", cnt, "score:", n.Score)
//			if cnt == len(insts)-1 {
//				break FOR
//			}
//		}
//	}
//	fmt.Println("out for")
//	if n.Score > len(insts)/2 {
//		n.SetRole(Leader)
//	}
//	//if n.Score > len(insts)/2 {
//	//    n.SetRole(Leader)
//	//}
//	return
//}
//

func Loop(node INode) {
	for {
		<-node.TimeOut()
		succeed := node.CampaignLeader()
		if !succeed {
			continue
		}
		// heart beat loop
		t := time.NewTicker(time.Second)
		for {
			<-t.C
			err := node.HeartBeat()
			if err != nil {
				fmt.Println(err)
			}
		}
	}
}

func RandTimeout() (timeout time.Duration, err error) {
	rnd, err := rand.Int(rand.Reader, big.NewInt(5000000000))
	if err != nil {
		return
	}
	timeout = time.Second*5 + time.Duration(rnd.Int64())
	return
}

// todo: 如何测试
func main() {
	if len(os.Args) < 5 {
		fmt.Println("Usage: node ip port console_ip console_port")
		return
	}
	timeout, err := RandTimeout()
	if err != nil {
		fmt.Println(err)
		return
	}
	node := Node{
		timeout:          timeout,
		Term:             0,
		Ip:               os.Args[1],
		Port:             os.Args[2],
		HeartBeatTimeOut: time.Second * 10,
	}
	node.role = Follower
	console := Console{
		Ip:   os.Args[3],
		Port: os.Args[4],
	}
	err = console.Register(node)
	if err != nil {
		fmt.Println(err)
		return
	}
	node.Cluster = Cluster{
		Console: console,
	}
	go Loop(&node)
	r := gin.Default()
	r.POST("/vote", VoteHandler(&node))
	r.POST("/heartbeat", HeartBeatHandler(&node))
	r.GET("/node", NodeHandler(&node))
	r.Run(fmt.Sprintf("%s:%s", os.Args[1], os.Args[2]))
}

type INode interface {
	HeartBeat() (err error)
	HandleHeartBeat(req VoteReq, respC chan VoteResult)
	HandleReq(req VoteReq, respC chan VoteResult)
	HandleResult(result VoteResult)
	CampaignLeader() (succeed bool)
	TimeOut() (timeout <-chan time.Time)
}
