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

var (
	IndexInconsistentErr = RaftErr{
		Code:        500001,
		Description: "Index  Inconsistent",
	}
)

type RaftErr struct {
	Code        int
	Description string
}

func (r RaftErr) Error() string {
	return r.Description
}

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
	sync.Mutex
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
	data             map[string]string
	setLog           []SetLog
	unSetLog         []SetLog
}

// 从Node组合而来的都可以与Node相等
func (n *Node) Equal(other INode) bool {
	if o, ok := other.(*Node); ok {
		return o.ip == n.ip && o.port == n.port
	}
	return other.Equal(n)
}

// 只有ip port信息
func (n *Node) HandleSet(key, value string, respC chan error) {
	var (
		err error
	)
	req := SetReq{
		Key:   key,
		Value: value,
	}
	result := SetResp{}
	resp := Response{
		Data: &result,
	}
	_, err = resty.New().
		SetTimeout(time.Second).
		SetRetryCount(3).
		R().
		SetBody(req).
		SetResult(&resp).
		Post(fmt.Sprintf("http://%s:%s/internal/set", n.ip, n.port))
	// 未知客户端错误
	if err != nil {
		fmt.Println("handle set error:", err)
		respC <- nil
		return
	}
	// 服务端处理出错
	if resp.Code != 0 {
		err = errors.New(resp.Msg)
	}
	respC <- err
}

type logCompensationReq struct {
	Logs  []SetLog `json:"logs"`
	Index int      `json:"index"`
}

func calcCompensation(index int, logs []SetLog) (i int, l []SetLog) {
	//fmt.Println("calc:", index, logs)
	if index > len(logs) {
		i = len(logs)
		l = []SetLog{}
	} else {
		i = index
		l = logs[index:]
	}
	return
}

func (n *Node) logCompensation(ip, port string, index int, logs []SetLog) (err error) {
	req := logCompensationReq{
		Logs:  logs,
		Index: index,
	}
	resp := Response{}
	_, err = resty.New().
		SetTimeout(time.Second).
		SetRetryCount(3).
		R().
		SetBody(req).
		SetResult(&resp).
		Post(fmt.Sprintf("http://%s:%s/log/compensation", n.ip, n.port))
	if err != nil {
		return
	}
	if resp.Code != 0 {
		err = errors.New(resp.Msg)
	}
	return
}

func (n *Node) Set(key, value string) (err error) {
	if n.role != Leader {
		err = errors.New("only leader can get and set")
		return
	}
	n.cluster.Lock()
	defer n.cluster.Unlock()
	nodes, err := n.cluster.Nodes()
	if err != nil {
		return
	}
	respC := make(chan error)
	for _, node := range nodes {
		if n.Equal(node) {
			continue
		}
		go node.HandleSet(key, value, respC)
	}
	cnt := 1
FOR:
	for {
		select {
		case err = <-respC:
			cnt = cnt + 1
			if err != nil {
				// 只要有一个失败就返回用户失败信息
				return
			}
			if cnt == len(nodes) {
				break FOR
			}
		}
	}
	err = n.set(key, value)
	return
}

//func (n *Node) handleSet(key, value string) (err error) {
//}

func (n *Node) Get(key string) (value string, err error) {
	if n.role != Leader {
		err = errors.New("only leader can get and set")
		return
	}
	return n.get(key)
}

func (n *Node) set(key, value string) (err error) {
	if n.data == nil {
		n.data = map[string]string{}
	}
	n.unSetLog = append(n.unSetLog, SetLog{Key: key, Value: n.data[key]})
	n.data[key] = value
	n.setLog = append(n.setLog, SetLog{Key: key, Value: value})
	return
}

func (n *Node) get(key string) (value string, err error) {
	value = n.data[key]
	return
}

func (n *Node) handleHeartBeat(req HeartBeatReq) (err error) {
	if req.Term < n.term {
		return
	}
	n.Lock()
	defer n.Unlock()
	n.term = req.Term
	n.timer.Reset(n.timeout)
	if len(n.setLog) != req.Index {
		err = IndexInconsistentErr
	}
	return
}

// 只有ip port信息
func (n *Node) HandleHeartBeat(req HeartBeatReq, respC chan error) {
	if req.Ip == n.ip && req.Port == n.port {
		return
	}
	result := HeartBeatResult{}
	resp := Response{
		Data: &result,
	}
	_, err := resty.New().SetTimeout(time.Second).R().SetBody(req).SetResult(&resp).
		Post(fmt.Sprintf("http://%s:%s/heartbeat", n.ip, n.port))
	// 未知客户端错误
	if err != nil {
		fmt.Println("handle set error:", err)
		respC <- nil
		return
	}
	// 数据不一致 补偿数据
	if resp.Code == IndexInconsistentErr.Code {
		index, l := calcCompensation(result.Index, req.logs)
		respC <- n.logCompensation(n.ip, n.port, index, l)
		return
	}
	// 服务端处理出错
	if resp.Code != 0 {
		respC <- errors.New(resp.Msg)
	}
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
	n.cluster.Lock()
	defer n.cluster.Unlock()
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

type HeartBeatResult struct {
	Result int `json:"result,omitempty"`
	Index  int `json:"index"`
}

type HeartBeatReq struct {
	Ip    string   `json:"ip,omitempty"`
	Port  string   `json:"port,omitempty"`
	Term  int      `json:"term,omitempty"`
	Index int      `json:"index"`
	logs  []SetLog `json:"-"`
}

type VoteReq struct {
	Ip   string `json:"ip,omitempty"`
	Port string `json:"port,omitempty"`
	Term int    `json:"term,omitempty"`
}
type SetReq struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	Index int
}

type SetLog struct {
	Key   string
	Value string
}

type SetResp struct {
	Key   string
	Value string
}

type GetReq struct {
	Key string `json:"key" form:"key"`
}

type GetResp struct {
	Key   string
	Value string
}

func InternalSetHandler(node *Node) gin.HandlerFunc {
	return func(c *gin.Context) {
		req := SetReq{}
		err := c.ShouldBindJSON(&req)
		if err != nil {
			c.JSON(http.StatusOK, Response{
				Code: 1,
				Msg:  err.Error(),
			})
			return
		}
		err = node.set(req.Key, req.Value)
		if err != nil {
			c.JSON(http.StatusOK, Response{
				Code: 1,
				Msg:  err.Error(),
			})
			return
		}
		c.JSON(http.StatusOK, Response{
			Data: SetResp{Key: req.Key, Value: req.Value},
		})
	}
}

func SetHandler(node *Node) gin.HandlerFunc {
	return func(c *gin.Context) {
		req := SetReq{}
		err := c.ShouldBindJSON(&req)
		if err != nil {
			c.JSON(http.StatusOK, Response{
				Code: 1,
				Msg:  err.Error(),
			})
			return
		}
		err = node.Set(req.Key, req.Value)
		if err != nil {
			c.JSON(http.StatusOK, Response{
				Code: 1,
				Msg:  err.Error(),
			})
			return
		}
		c.JSON(http.StatusOK, Response{
			Data: SetResp{Key: req.Key, Value: req.Value},
		})
	}
}

func GetHandler(node *Node) gin.HandlerFunc {
	return func(c *gin.Context) {
		req := GetReq{}
		err := c.Bind(&req)
		if err != nil {
			c.JSON(http.StatusOK, Response{
				Code: 1,
				Msg:  err.Error(),
			})
			return
		}
		value, err := node.Get(req.Key)
		if err != nil {
			c.JSON(http.StatusOK, Response{
				Code: 1,
				Msg:  err.Error(),
			})
			return
		}
		c.JSON(http.StatusOK, Response{
			Data: GetResp{Key: req.Key, Value: value},
		})
	}
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
		v := HeartBeatReq{}
		err := c.ShouldBindJSON(&v)
		if err != nil {
			c.JSON(http.StatusOK, Response{
				Code: 1,
				Msg:  err.Error(),
			})
			return
		}
		err = node.handleHeartBeat(v)
		if errors.Is(err, IndexInconsistentErr) {
			c.JSON(http.StatusOK, Response{
				Code: IndexInconsistentErr.Code,
				Msg:  IndexInconsistentErr.Description,
			})
			return
		}
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
	n.cluster.Lock()
	defer n.cluster.Unlock()
	nodes, err := n.cluster.Nodes()
	if err != nil {
		return
	}
	if len(nodes) < 3 {
		err = fmt.Errorf("at least 3 nodes but only %d", len(nodes))
		return
	}
	respC := make(chan error)
	for _, i := range nodes {
		req := HeartBeatReq{Ip: n.ip, Port: n.port, Term: n.term, Index: len(n.setLog), logs: n.setLog}
		// #NOTE: 这里的i只有ip port信息
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

// 只有ip port信息
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
func (n *Node) rollBackTo(index int) (err error) {
	tail := len(n.unSetLog) - 1
	for i := tail; i > index; i-- {
		op := n.unSetLog[i]
		n.data[op.Key] = op.Value
		n.unSetLog = n.unSetLog[:len(n.unSetLog)-1]
		n.setLog = n.setLog[:len(n.setLog)-1]
	}
	return
}

func (n *Node) applyAll(logs []SetLog) (err error) {
	for _, log := range logs {
		err = n.set(log.Key, log.Value)
		if err != nil {
			return
		}
	}
	return
}

func (n *Node) overWriteLog(logs []SetLog, index int) (err error) {
	//fmt.Printf("overwrite %s:%s logs:%v  to index:%d logs:%v\n", n.ip, n.port, n.setLog, index, logs)
	//l := n.setLog[:index]
	//for _, i := range logs {
	//	l = append(l, i)
	//}
	//n.setLog = l
	//fmt.Println(n.setLog)
	err = n.rollBackTo(index - 1)
	if err != nil {
		return err
	}
	err = n.applyAll(logs)
	if err != nil {
		return err
	}
	//fmt.Println(n.setLog)
	//fmt.Println(n.unSetLog)
	return
}

func logCompensationHandler(n *Node) gin.HandlerFunc {
	return func(c *gin.Context) {
		req := logCompensationReq{}
		err := c.ShouldBindJSON(&req)
		if err != nil {
			c.JSON(http.StatusOK, Response{
				Code: 1,
				Msg:  err.Error(),
			})
			return
		}
		err = n.overWriteLog(req.Logs, req.Index)
		if err != nil {
			c.JSON(http.StatusOK, Response{
				Code: 1,
				Msg:  err.Error(),
			})
			return
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
	node.cluster = &Cluster{
		Console: console,
	}
	go Loop(&node)
	r := gin.Default()
	r.POST("/vote", VoteHandler(&node))
	r.POST("/heartbeat", HeartBeatHandler(&node))
	r.GET("/node", NodeHandler(&node))
	// 内部调用方法
	r.POST("/internal/set", InternalSetHandler(&node))
	r.POST("/set", SetHandler(&node))
	r.GET("/get", GetHandler(&node))
	r.POST("/log/compensation", logCompensationHandler(&node))
	r.Run(fmt.Sprintf("%s:%s", os.Args[1], os.Args[2]))
}

type INode interface {
	HeartBeat() (err error)
	HandleHeartBeat(req HeartBeatReq, respC chan error)
	HandleReq(req VoteReq, respC chan VoteResult)
	HandleSet(key, value string, respC chan error)
	HandleResult(result VoteResult)
	Set(key, value string) (err error)
	Get(key string) (value string, err error)
	CampaignLeader() (succeed bool)
	TimeOut() (timeout <-chan time.Time)
	Equal(n INode) bool
}

type ICluster interface {
	sync.Locker
	Nodes() (nodes []INode, err error)
}
