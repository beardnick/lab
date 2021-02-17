package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"

	"github.com/gin-gonic/gin"
)

var nodes = []Instance{}

type Instance struct {
	Ip   string `json:"ip,omitempty"`
	Port string `json:"port,omitempty"`
}

type Response struct {
	Code int         `json:"code,omitempty"`
	Data interface{} `json:"data,omitempty"`
	Msg  string      `json:"msg,omitempty"`
}

func RegisterHandler(c *gin.Context) {
	n := Instance{}
	err := c.ShouldBindJSON(&n)
	if err != nil {
		c.JSON(http.StatusOK, Response{
			Code: 1,
			Msg:  err.Error(),
		})
		return
	}
	nodes = append(nodes, n)
	c.JSON(http.StatusOK, Response{})
}

func NodesHandler(c *gin.Context) {
	c.JSON(http.StatusOK, Response{
		Data: nodes,
	})
}

func DebugHandler(req, resp bool) gin.HandlerFunc {
	return func(c *gin.Context) {
		if req {
			fmt.Println("req:")
			d, _ := ioutil.ReadAll(c.Request.Body)
			fmt.Printf("%+v\n", string(d))
			c.Request.Body = ioutil.NopCloser(bytes.NewBuffer(d))
		}
		c.Next()
		if resp {
			fmt.Println("resp:")
			fmt.Printf("%+v\n", c.Request.Response)
		}
	}
}

func main() {
	if len(os.Args) < 3 {
		fmt.Println("Usage: console ip port")
		return
	}
	r := gin.Default()
	r.Use(DebugHandler(true, false))
	r.POST("/register", RegisterHandler)
	r.GET("/nodes", NodesHandler)
	r.Run(fmt.Sprintf("%s:%s", os.Args[1], os.Args[2]))
}
