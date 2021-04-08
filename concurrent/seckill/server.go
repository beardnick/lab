package main

import "github.com/gin-gonic/gin"

func RegisterRouters(router gin.Engine)  {
	router.POST("/production",CreateProductionHandler)
	router.GET("/production/cnt",GetProductionCntHandler)
}

func GetProductionCntHandler(c *gin.Context) {
	id := c.Query("id")
	if id == "" {
	}
}

func CreateProductionHandler(c *gin.Context) {
	
}
