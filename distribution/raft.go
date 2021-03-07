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

type Mode struct {
	testMode bool
	sync.RWMutex
}

var mode Mode = Mode{testMode: false}

func SetTestMode(test bool) {
	mode.Lock()
	defer mode.Unlock()
	mode.testMode = test
}

func TestMode() bool {
	mode.RLock()
	defer mode.RUnlock()
	return mode.testMode
}

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

func (c Cluster) Nodes() (nodes []INode, err error) {
	return c.Console.Nodes()
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
	heartBeatTimeOut time.Duration `json:"heart_beat_time_out,omitempty"`
	term             int           `json:"term,omitempty"`
	ip               string        `json:"ip,omitempty"`
	port             string        `json:"port,omitempty"`
	score            int           `json:"score,omitempty"`
	cluster          ICluster      `json:"-"`
	timer            *time.Timer   `json:"-"`
}

func (n *Node) handleHeartBeat(req VoteReq) {
	if req.Term < n.term {
		return
	}
	n.Lock()
	defer n.Unlock()
	n.term = req.Term
	n.timer.Reset(n.timeout)
}

func (n *Node) HandleHeartBeat(req VoteReq, respC chan VoteResult) {
	if req.Ip == n.ip && req.Port == n.port {
		return
	}
	_, _ = resty.New().SetTimeout(time.Second).R().SetBody(req).
		Post(fmt.Sprintf("http://%s:%s/heartbeat", n.ip, n.port))
}

func (n *Node) TimeOut() (timeout <-chan time.Time) {
	if n.timer == nil {
		n.timer = time.NewTimer(n.timeout)
	}
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
	n.term = n.term + 1
	nodes, err := n.cluster.Nodes()
	if err != nil {
		fmt.Println(err)
		return
	}
	if len(nodes) < 3 {
		fmt.Printf("至少要有3个节点，现在只有%d个\n", len(nodes))
		return
	}
	respC := make(chan VoteResult)
	for _, peer := range nodes {
		req := VoteReq{Ip: n.ip, Port: n.port, Term: n.term}
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
			fmt.Println("cnt:", cnt, "score:", n.score)
			if cnt == len(nodes) {
				break FOR
			}
		}
	}
	if n.score > len(nodes)/2 {
		n.role = Leader
		fmt.Println("Leader")
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
			Data: gin.H{
				"term":    node.term,
				"role":    node.role.String(),
				"timeout": node.timeout,
				"ip":      node.ip,
				"port":    node.port,
				"score":   node.score,
			},
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
		node.handleHeartBeat(v)
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

func (c Console) Nodes() (nodes []INode, err error) {
	var insts []Instance
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
	for _, inst := range insts {
		nodes = append(nodes, &Node{ip: inst.Ip, port: inst.Port})
	}
	return
}

func (c Console) Register(node Node) (err error) {
	r := Response{}
	n := Instance{
		Ip:   node.ip,
		Port: node.port,
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
	nodes, err := n.cluster.Nodes()
	if err != nil {
		return
	}
	if len(nodes) < 3 {
		err = fmt.Errorf("at least 3 nodes but only %d", len(nodes))
		return
	}
	respC := make(chan VoteResult)
	for _, i := range nodes {
		req := VoteReq{Ip: n.ip, Port: n.port, Term: n.term}
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
		Post(fmt.Sprintf("http://%s:%s/vote", n.ip, n.port))
	if err != nil || resp.Code != 0 {
		fmt.Println("vote err:", err, "resp:", resp)
		result.Result = Reject
	}
	respC <- result
}

func (n *Node) handleReq(req VoteReq) (result VoteResult) {
	if req.Ip == n.ip && req.Port == n.port {
		result.Result = Accept
		return
	}
	n.Lock()
	defer n.Unlock()
	if req.Term > n.term {
		if TestMode() {
			// 打印增加延时，更容易出现并发错误
			fmt.Println("n0.term:", n.term)
		}
		n.role = Follower
		n.term = req.Term
		n.timer.Reset(n.timeout)
		result.Result = Accept
		return
	}
	result.Result = Reject
	return
}

func (n *Node) HandleResult(result VoteResult) {
	n.Lock()
	defer n.Unlock()
	if result.Result == Accept {
		n.score = n.score + 1
	}
}

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
		term:             0,
		ip:               os.Args[1],
		port:             os.Args[2],
		heartBeatTimeOut: time.Second * 10,
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
	node.cluster = Cluster{
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

type ICluster interface {
	Nodes() (nodes []INode, err error)
}
