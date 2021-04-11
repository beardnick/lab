package main

import (
	"errors"
	"github.com/gin-gonic/gin"
	"net/http"
)

func RegisterRouters(router *gin.Engine) {
	router.POST("/production", CreateProductionHandler)
	router.GET("/production/cnt", GetProductionCntHandler)
}

func GetProductionCntHandler(c *gin.Context) {
	id := c.Query("id")
	if id == "" {
		c.JSON(http.StatusOK, ErrResponse(errors.New("id is empty")))
		return
	}
	p := NewProductionDao()
	r, err := p.GetProductionCnt(id)
	c.JSON(http.StatusOK, ResultOrErrResponse(r, err))
}

func CreateProductionHandler(c *gin.Context) {
	production := Production{}
	err := c.ShouldBindJSON(&production)
	if err != nil {
		c.JSON(http.StatusOK, ErrResponse(err))
		return
	}
	id, err := NewProductionDao().Insert(production)
	resp := NewResponse()
	resp.addAttribute("id", id)
	c.JSON(http.StatusOK, ResultOrErrResponse(resp, err))
}
