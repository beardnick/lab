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

const (
	Follower = iota
	Leader
	Candidate
)

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
	Role    int
	TimeOut time.Duration
	Term    int
	Ip      string
	Port    string
	Score   int
	Cluster Cluster
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

func VoteHandler(node Node) gin.HandlerFunc {
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
		if node.Role == Leader || node.Role == Candidate || node.Term > v.Term {
			c.JSON(http.StatusOK, Response{
				Data: VoteResult{Reject},
			})
			return
		}
		c.JSON(http.StatusOK, Response{
			Data: VoteResult{Accept},
		})
	}
}

func (c Console) Nodes() (insts []Instance, err error) {
	panic("not implemented")
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

func (n *Node) Loop() {

}

func vote(req VoteReq, inst Instance, respC chan VoteResult) {
	var (
		err error
	)
	resp := Response{}
	_, err = resty.New().R().
		SetBody(req).
		SetResult(&resp).
		Post(fmt.Sprintf("http://%s:%s/vote", inst.Ip, inst.Port))
	result := VoteResult{}
	if err != nil || resp.Code != 0 {
		result.Result = Reject
		return
	}
	result, ok := resp.Data.(VoteResult)
	if !ok {
		result.Result = Reject
	}
	respC <- result
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
	respC := make(chan VoteResult)
	for _, i := range insts {
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
	return
}

func main() {
	if len(os.Args) < 5 {
		fmt.Println("Usage: node ip port console_ip console_port")
		return
	}
	rand.Seed(time.Now().Unix())
	node := Node{
		Role:    Follower,
		TimeOut: time.Second * time.Duration((rand.Int() % 10)),
		Term:    0,
		Ip:      os.Args[1],
		Port:    os.Args[2],
	}
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
	r := gin.Default()
	r.POST("/vote", VoteHandler(node))
	r.Run(fmt.Sprintf("%s:%s", os.Args[1], os.Args[2]))
}
