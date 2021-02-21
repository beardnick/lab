package main

import (
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/go-resty/resty/v2"
)

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
	role             RaftRole      `json:"role,omitempty"`
	TimeOut          time.Duration `json:"time_out,omitempty"`
	HeartBeatTimeOut time.Duration `json:"heart_beat_time_out,omitempty"`
	Term             int           `json:"term,omitempty"`
	Ip               string        `json:"ip,omitempty"`
	Port             string        `json:"port,omitempty"`
	Score            int           `json:"score,omitempty"`
	Cluster          Cluster       `json:"cluster,omitempty"`
	Timer            *time.Timer   `json:"timer,omitempty"`
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
		if node.Role() == Leader || node.Role() == Candidate || node.Term > v.Term {
			c.JSON(http.StatusOK, Response{
				Data: VoteResult{Reject},
			})
			return
		}
		node.Timer.Reset(node.TimeOut)
		c.JSON(http.StatusOK, Response{
			Data: VoteResult{Accept},
		})
	}
}

func (c Console) Nodes() (insts []Instance, err error) {
	resp := Response{
		Data: &insts,
	}
	_, err = resty.New().R().
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
	resp, err := resty.New().R().
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
			_, _ = resty.New().R().
				Get(fmt.Sprintf("http://%s:%s/heartbeat", inst.Ip, inst.Port))
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
	_, err = resty.New().R().
		SetBody(req).
		SetResult(&resp).
		Post(fmt.Sprintf("http://%s:%s/vote", inst.Ip, inst.Port))
	if err != nil || resp.Code != 0 {
		result.Result = Reject
		return
	}
	respC <- result
}

func (n *Node) SetRole(role RaftRole) {
	fmt.Println(role)
	n.role = role
}

func (n *Node) Role() RaftRole {
	return n.role
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
	n.Term = n.Term + 1
	cnt := 0
	select {
	case result := <-respC:
		if result.Result == Accept {
			n.Score = n.Score + 1
		}
		cnt = cnt + 1
		if cnt == len(insts)-1 {
			break
		}
	}
	if n.Score > len(insts)/2 {
		n.SetRole(Leader)
	}
	return
}

func Loop(node *Node) {
	node.Timer = time.NewTimer(node.TimeOut)
	for {
		<-node.Timer.C
		node.SetRole(Candidate)
		node.Score = 1
		err := node.Vote()
		if err != nil {
			fmt.Println(err)
			node.Timer.Reset(node.TimeOut)
			continue
		}
		if node.Role() == Leader {
			node.HeartBeatLoop()
		}
	}
}

func main() {
	if len(os.Args) < 5 {
		fmt.Println("Usage: node ip port console_ip console_port")
		return
	}
	rand.Seed(time.Now().Unix())
	node := Node{
		TimeOut:          time.Second * time.Duration((rand.Int()%10)+5),
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
	err := console.Register(node)
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
	r.GET("/heartbeat", HeartBeatHandler(&node))
	r.GET("/node", NodeHandler(&node))
	r.Run(fmt.Sprintf("%s:%s", os.Args[1], os.Args[2]))
}
