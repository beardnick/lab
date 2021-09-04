package main

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestOrderProduct(t *testing.T) {
	pName := "iphone"
	pCnt := 1000

	mysqlProvider := NewMysqlProduct()
	testOrderProduct(t, mysqlProvider, pName, pCnt)

	pCnt = 100000
	redisProvider := NewRedisProduct()
	testOrderProduct(t, redisProvider, pName, pCnt)
}

func testOrderProduct(t *testing.T, provider ProductProvider, pName string, pCnt int) {
	err := provider.SetProduct(Product{
		Name: pName,
		Cnt:  pCnt,
	})
	assert.Nil(t, err)
	wg := &sync.WaitGroup{}
	for i := 0; i < pCnt; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			provider.OrderProduct(pName, 1)
		}()
	}
	wg.Wait()
	cnt, err := provider.ProductCnt(pName)
	assert.Nil(t, err)
	assert.Zero(t, cnt)
}
