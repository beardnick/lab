package main

import (
	"github.com/gin-gonic/gin"
)

func RegisterRouters(router *gin.Engine) {
	router.POST("/production", CreateProductionHandler)
	router.GET("/production/cnt", GetProductionCntHandler)
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
