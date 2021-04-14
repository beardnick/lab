package main

import (
	"errors"
	"fmt"
)

func CreateOrder(order Order) (id string, err error) {
	p := NewProductionDao()
	left, err := p.CacheCntOf(order.Production)
	if err != nil {
		return
	}
	if left < order.Cnt {
		err = errors.New(fmt.Sprintf("库存不足，仅剩%d件", left))
		return
	}
	id, err = NewOrderDao().Insert(order)
	if err != nil {
		return
	}
	err = p.CacheCnt(order.Production, left-order.Cnt)
	// todo 异步修改数据库
	return
}

func CreateProduction(production Production) (id string, err error) {
	p := NewProductionDao()
	id, err = p.Insert(production)
	if err != nil {
		return
	}
	err = p.CacheCnt(id, production.Cnt)
	return
}
