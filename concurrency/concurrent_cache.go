package main

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/patrickmn/go-cache"
	"net/http"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

type TryLock struct {
	locked uint32
}

func (t *TryLock) Lock() {
	for !t.TryLock() {
		runtime.Gosched()
	}
}
func (t *TryLock) TryLock() bool {
	return atomic.CompareAndSwapUint32(&t.locked, 0, 1)
}

func (t *TryLock) Unlock() {
	atomic.StoreUint32(&t.locked, 0)
}

var ca = cache.New(cache.NoExpiration, cache.NoExpiration)

// 双重校验缓存 获取缓存失败则抢锁， 再次获取缓存 再次失败则读库 读库成功则释放锁
var dbCnt int64 = 0
var cnt int64 = 0

func GetData() string {
	fmt.Println("hit db")
	atomic.AddInt64(&dbCnt, 1)
	time.Sleep(time.Microsecond * 100)
	return "the return data"
}

var lock = sync.Mutex{}

// 缓存失效的那一刻进来的所有请求中有一个请求会读db
// 其它请求排队读缓存
// 如果有10000个同时进入，那么第一个读db，另外9999个排队读缓存
// 第10000个进入读请求耗时 1 * dbtime + 9999 * cachetime，有可能超时
func ConcurrentCache(c *gin.Context) {
	atomic.AddInt64(&cnt, 1)
	data, ok := ca.Get("cache")
	if !ok {
		lock.Lock()
		defer lock.Unlock()
		secondOk := false
		data, secondOk = ca.Get("cache")
		if !secondOk {
			data = GetData()
			ca.Set("cache", data, time.Minute)
		}
	}
	c.String(http.StatusOK, data.(string))
}

var try = TryLock{}

func TryGetData() string {
	data, ok := ca.Get("cache")
	if !ok {
		if try.TryLock() {
			data = GetData()
			ca.Set("cache", data, time.Minute)
			try.Unlock()
		}
		time.Sleep(time.Microsecond)
		data = TryGetData()
	}
	return data.(string)
}

func ConcurrentCache2(c *gin.Context) {
	atomic.AddInt64(&cnt, 1)
	data := TryGetData()
	c.String(http.StatusOK, data)
}

// 在缓存失效的那一刻会有非常多的请求直接打在db上
func ConcurrentCache1(c *gin.Context) {
	atomic.AddInt64(&cnt, 1)
	data, ok := ca.Get("cache")
	if !ok {
		data = GetData()
		ca.Set("cache", data, time.Minute)
	}
	c.String(http.StatusOK, data.(string))
}

func Hit(c *gin.Context) {
	c.String(http.StatusOK, fmt.Sprintf("db:%d cache:%d", dbCnt, cnt-dbCnt))
}

func main() {
	router := gin.Default()
	router.GET("/data", ConcurrentCache)
	router.GET("/hit", Hit)
	router.Run("127.0.0.1:9090")
}
