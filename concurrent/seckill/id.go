package main

import (
	"github.com/bwmarrin/snowflake"
)

func NewId() (id string, err error) {
	n, err := snowflake.NewNode(1)
	if err != nil {
		return
	}
	id = n.Generate().String()
	return
}
