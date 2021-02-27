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
	Role             RaftRole      `json:"role,omitempty"`
	TimeOut          time.Duration `json:"time_out,omitempty"`
	HeartBeatTimeOut time.Duration `json:"heart_beat_time_out,omitempty"`
	Term             int           `json:"term,omitempty"`
	Ip               string        `json:"ip,omitempty"`
	Port             string        `json:"port,omitempty"`
	Score            int           `json:"score,omitempty"`
	Cluster          Cluster       `json:"-"`
	Timer            *time.Timer   `json:"-"`
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
		node.Timer.Reset(node.TimeOut)
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
		// todo:这个node修改的操作是互斥的,这里会不会有并发问题
		if v.Term > node.Term {
			node.Role = Follower
			node.Term = v.Term
			node.Timer.Reset(node.TimeOut)
			c.JSON(http.StatusOK, Response{
				Data: VoteResult{Accept},
			})
			return
		}
		//if node.Role == Candidate {
		//    c.JSON(http.StatusOK, Response{
		//        Data: VoteResult{Reject},
		//    })
		//    return
		//}
		node.Timer.Reset(node.TimeOut)
		c.JSON(http.StatusOK, Response{
			Data: VoteResult{Reject},
		})
	}
}

func (c Console) Nodes() (insts []Instance, err error) {
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
	insts, err := n.Cluster.Console.Nodes()
	if err != nil {
		return
	}
	if len(insts) < 3 {
		err = fmt.Errorf("at least 3 nodes but only %d", len(insts))
		return
	}
	for _, i := range insts {
		if i.Ip == n.Ip && i.Port == n.Port {
			continue
		}
		go func(inst Instance) {
			req := VoteReq{
				Ip:   n.Ip,
				Port: n.Port,
				Term: n.Term,
			}
			_, _ = resty.New().SetTimeout(time.Second).R().SetBody(req).
				Post(fmt.Sprintf("http://%s:%s/heartbeat", inst.Ip, inst.Port))
		}(i)
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

func (n *Node) SetRole(role RaftRole) {
	fmt.Println(role)
	n.Role = role
}

func (n *Node) Vote() (err error) {
	req := VoteReq{
		Ip:   n.Ip,
		Port: n.Port,
		Term: n.Term,
	}
	insts, err := n.Cluster.Console.Nodes()
	if err != nil {
		return
	}
	if len(insts) < 3 {
		err = fmt.Errorf("at least 3 nodes but only %d", len(insts))
		return
	}
	respC := make(chan VoteResult)
	for _, i := range insts {
		if i.Ip == n.Ip && i.Port == n.Port {
			continue
		}
		go vote(req, i, respC)
	}
	cnt := 0
FOR:
	for {
		select {
		case result := <-respC:
			if result.Result == Accept {
				n.Score = n.Score + 1
			}
			// todo:怎样判断所有请求是否已经到达
			// todo:判断超过半数票之后直接退出是否可行
			cnt = cnt + 1
			fmt.Println("cnt:", cnt, "score:", n.Score)
			if cnt == len(insts)-1 {
				break FOR
			}
		}
	}
	fmt.Println("out for")
	if n.Score > len(insts)/2 {
		n.SetRole(Leader)
	}
	//if n.Score > len(insts)/2 {
	//    n.SetRole(Leader)
	//}
	return
}

func Loop(node *Node) {
	node.Timer = time.NewTimer(node.TimeOut)
	for {
		<-node.Timer.C
		node.Timer.Reset(node.TimeOut)
		// todo: 处理同时被选中的情况
		//if voted {
		//    timeout, err := RandTimeout()
		//    if err != nil {
		//    }
		//    continue
		//}
		node.SetRole(Candidate)
		node.Score = 1
		node.Term = node.Term + 1
		err := node.Vote()
		if err != nil {
			fmt.Println(err)
			continue
		}
		if node.Role == Leader {
			node.HeartBeatLoop()
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
		TimeOut:          timeout,
		Term:             0,
		Ip:               os.Args[1],
		Port:             os.Args[2],
		HeartBeatTimeOut: time.Second * 10,
	}
	node.SetRole(Follower)
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
