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
	Leader = iota
	Follower
	Candidate
)

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
	Inst    Instance
}

func VoteHandler(c *gin.Context) {
	n := Instance{}
	err := c.ShouldBindJSON(n)
	if err != nil {
		c.JSON(http.StatusOK, Response{
			Code: 1,
			Msg:  err.Error(),
		})
		return
	}
}

var node Node

func Register(ip, port string) (err error) {
	r := Response{}
	n := Instance{
		Ip:   ip,
		Port: port,
	}
	resp, err := resty.New().R().
		SetBody(n).
		SetResult(&r).
		Post(fmt.Sprintf("http://%s:%s/register", ip, port))
	if err != nil {
		return
	}
	if resp.StatusCode() != 200 || r.Code != 0 {
		err = errors.New(r.Msg)
		return
	}
	return
}

func main() {
	if len(os.Args) < 5 {
		fmt.Println("Usage: node ip port console_ip console_port")
		return
	}
	rand.Seed(time.Now().Unix())
	node = Node{
		Role:    Follower,
		TimeOut: time.Second * time.Duration((rand.Int() % 10)),
		Term:    0,
		Inst: Instance{
			Ip:   os.Args[1],
			Port: os.Args[2],
		},
	}
	err := Register(os.Args[3], os.Args[4])
	if err != nil {
		fmt.Println(err)
		return
	}
	r := gin.Default()
	r.POST("/vote", VoteHandler)
	r.Run(fmt.Sprintf("%s:%s", os.Args[1], os.Args[2]))
}
