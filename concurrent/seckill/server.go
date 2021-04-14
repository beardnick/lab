package main

import (
	"github.com/gin-gonic/gin"
)

func RegisterRouters(router *gin.Engine) {
	router.POST("/production", CreateProductionHandler)
	router.GET("/production/cnt", GetProductionCntHandler)
	router.GET("/order",GetOrderHandler)
	router.POST("/order",CreateOrderHandler)
}

func CreateOrderHandler(c *gin.Context) {
	order := Order{}
	err := c.ShouldBindJSON(&order)
	if err != nil {
		ErrResponse(c, UnknownErr.Of(err.Error()))
		return
	}
	id, err := NewOrderDao().Insert(order)
	ErrOrSuccessResponse(c, gin.H{"id": id}, UnknownErr.OfErr(err))
}

func GetOrderHandler(c *gin.Context) {
	id := c.Query("id")
	if id == "" {
		ErrResponse(c, ParameterErr.Of("id is empty"))
		return
	}
	p := NewOrderDao()
	r, err := p.GetOrder(id)
	ErrOrSuccessResponse(c, r, UnknownErr.OfErr(err))
}



func GetProductionCntHandler(c *gin.Context) {
	id := c.Query("id")
	if id == "" {
		ErrResponse(c, ParameterErr.Of("id is empty"))
		return
	}
	p := NewProductionDao()
	r, err := p.GetProductionCnt(id)
	ErrOrSuccessResponse(c, r, UnknownErr.OfErr(err))
}

func CreateProductionHandler(c *gin.Context) {
	production := Production{}
	err := c.ShouldBindJSON(&production)
	if err != nil {
		ErrResponse(c, UnknownErr.Of(err.Error()))
		return
	}
	id, err := NewProductionDao().Insert(production)
	ErrOrSuccessResponse(c, gin.H{"id": id}, UnknownErr.OfErr(err))
}
