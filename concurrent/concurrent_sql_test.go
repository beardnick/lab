package main

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/rs/xid"
	"github.com/stretchr/testify/assert"
)

func TestOrderProduct(t *testing.T) {
	// mysql cannot handle it
	pName := "iphone"
	pCnt := 100000
	mysqlProvider := NewMysqlProduct()
	testOrderProduct(t, mysqlProvider, pName, pCnt)

	// pCnt = 100000
	// redisProvider := NewRedisProduct()
	// testOrderProduct(t, redisProvider, pName, pCnt)
}

func testOrderProduct(t *testing.T, provider ProductProvider, pName string, pCnt int) {
	err := provider.SetProduct(Product{
		Name: pName,
		Cnt:  pCnt,
	})
	assert.Nil(t, err)
	wg := &sync.WaitGroup{}
	ch := make(chan struct{}, 1000)
	for i := 0; i < pCnt; i++ {
		wg.Add(1)
		ch <- struct{}{}
		go func() {
			defer wg.Done()
			retry := 5
			orderId := xid.New()
			for i := 0; i < retry; i++ {
				err = provider.OrderProduct(orderId.String(), pName, 1)
				if err != nil {
					fmt.Printf("order err %v retry %v\n", err, i)
					time.Sleep(time.Millisecond * 100 * (1 << i))
				}
			}
			<-ch
		}()
	}
	wg.Wait()
	cnt, err := provider.ProductCnt(pName)
	assert.Nil(t, err)
	assert.Zero(t, cnt)
}
