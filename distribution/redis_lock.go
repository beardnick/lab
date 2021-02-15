package main

import (
	"context"
	"errors"
	"time"

	"github.com/go-redis/redis/v8"
)

var rdb = redis.NewClient(&redis.Options{
	Addr:     "localhost:6379",
	Password: "", // no password set
	DB:       0,  // use default DB
})

type RedisLock struct {
	Key string
}

func (r RedisLock) Lock() (err error) {
	set := false
	for !set {
		set, err = rdb.SetNX(context.Background(), r.Key, 1, time.Second).Result()
		if err != nil {
			return
		}
	}
}

func (r RedisLock) Unlock() (err error) {
	set, err := rdb.SetNX(context.Background(), r.Key, 0, time.Second).Result()
	if err != nil {
		return
	}
	if !set {
		err = errors.New("unlock failed: cannot set lock to 0")
	}
	return
}

func main() {
	l := RedisLock{Key: "test"}
	err := l.Lock()
	if err != nil {
		panic(err)
	}
	err = l.Unlock()
	if err != nil {
		panic(err)
	}
}
