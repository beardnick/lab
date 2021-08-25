package main

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

var (
	ParameterErr = Err{Code: 4000}
	InternalErr  = Err{Code: 5000}
	UnknownErr   = Err{Code: 6000}
)

type Err struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
}

func (e Err) Is(err Err) bool {
	return e.Code == err.Code
}

func (e Err) Of(msg string) Err {
	return Err{
		Code: e.Code,
		Msg:  msg,
	}
}

func (e Err) OfErr(err error) Err {
	if err == nil {
		return Err{}
	}
	return e.Of(err.Error())
}

type Response struct {
	Err
	Data interface{} `json:"data"`
}

func ErrResponse(c *gin.Context, err Err) {
	if err.Is(InternalErr) {
		c.JSON(http.StatusInternalServerError, Response{
			Err:  err,
			Data: nil,
		})
		return
	}
	c.JSON(http.StatusOK, Response{
		Err:  err,
		Data: nil,
	})
}

func SuccessResponse(c *gin.Context, data interface{}) {
	c.JSON(http.StatusOK, Response{
		Err:  Err{},
		Data: data,
	})
}

func ErrOrSuccessResponse(c *gin.Context, data interface{}, err Err) {
	if err.Code != 0 {
		ErrResponse(c, err)
		return
	}
	SuccessResponse(c, data)
}
