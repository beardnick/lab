package main

import (
	"context"
	"math/rand"
	"strconv"
	"time"

	"github.com/go-redis/redis/v8"
	"golang.org/x/exp/errors/fmt"
)

var rdb = redis.NewClient(&redis.Options{
	Addr:     "",
	Password: "", // no password set
	DB:       0,  // use default DB
})

type RedisLock struct {
	Key    string
	id     int
	Expire time.Duration
}

func (r *RedisLock) Lock() (err error) {
	// 这里必须要有随机数种子，不然就会出现重复的随机数
	rand.Seed(time.Now().Unix())
	r.id = rand.Int()
	fmt.Println("id:", r.id)
	if r.Expire == 0 {
		r.Expire = time.Minute
	}
	set := false
	for !set {
		set, err = rdb.SetNX(context.Background(), r.Key, r.id, r.Expire).Result()
		if err != nil {
			return
		}
	}
	return
}

func (r *RedisLock) Unlock() (err error) {
	i, err := rdb.Get(context.Background(), r.Key).Result()
	if err != nil {
		err = fmt.Errorf("lock may has expired:%v", err)
		return
	}
	// 如果不是自己创建的锁就不能删除
	if i != strconv.Itoa(r.id) {
		fmt.Printf("not my lock want:%v get:%v\n", r.id, i)
		return
	}
	_, err = rdb.Del(context.Background(), r.Key).Result()
	if err != nil {
		err = fmt.Errorf("lock may has expired:%v", err)
		return
	}
	return
}

func main() {
	l := RedisLock{Key: "test", Expire: time.Second * 20}
	fmt.Println("waiting...")
	err := l.Lock()
	if err != nil {
		panic(err)
	}
	fmt.Println("runing...")
	time.Sleep(time.Second * 15)
	err = l.Unlock()
	if err != nil {
		panic(err)
	}
	fmt.Println("finish")
}
