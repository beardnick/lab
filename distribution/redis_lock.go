package main

import (
	"context"
	"time"

	"github.com/go-redis/redis/v8"
	"golang.org/x/exp/errors/fmt"
)

var rdb = redis.NewClient(&redis.Options{
	Addr:     "192.168.0.120:6379",
	Password: "raspberry@123", // no password set
	DB:       0,               // use default DB
})

type RedisLock struct {
	Key string
}

// 如果处理超过1分钟，这个锁就会出问题
func (r RedisLock) Lock() (err error) {
	set := false
	for !set {
		set, err = rdb.SetNX(context.Background(), r.Key, 1, time.Minute).Result()
		if err != nil {
			return
		}
	}
	return
}

func (r RedisLock) Unlock() (err error) {
	_, err = rdb.Del(context.Background(), r.Key).Result()
	if err != nil {
		return
	}
	return
}

func main() {
	l := RedisLock{Key: "test"}
	fmt.Println("waiting...")
	err := l.Lock()
	if err != nil {
		panic(err)
	}
	fmt.Println("runing...")
	time.Sleep(time.Second * 10)
	err = l.Unlock()
	if err != nil {
		panic(err)
	}
	fmt.Println("finish")
}
