package main

import (
	"fmt"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestOrderProduct(t *testing.T) {
	pName := "iphone"
	pCnt := 1000
	err := InitProduct(Product{
		Name: pName,
		Cnt:  pCnt,
	})
	assert.Nil(t, err)
	wg := &sync.WaitGroup{}
	for i := 0; i < pCnt; i++ {
		idx := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			fmt.Println("go", idx)
			OrderProduct(pName, 1)
		}()
	}
	wg.Wait()
	cnt, err := ProductCnt(pName)
	assert.Nil(t, err)
	assert.Zero(t, cnt)
}
