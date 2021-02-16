package main

import (
	"context"
	"math/rand"
	"time"

	"github.com/go-redis/redis/v8"
	"golang.org/x/exp/errors/fmt"
)

var rdb = redis.NewClient(&redis.Options{
	Addr:     "",
	Password: "",
	DB:       0,
})

var delEqual = redis.NewScript(`
if redis.call("get",KEYS[1]) == ARGV[1] then 
   return redis.call("del",KEYS[1])
 else 
   return 0
 end`)

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
	//  使用lua脚本保证比较和删除操作的原子性
	_, err = delEqual.Run(context.Background(), rdb, []string{r.Key}, r.id).Result()
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
	time.Sleep(time.Second * 30)
	err = l.Unlock()
	if err != nil {
		panic(err)
	}
	fmt.Println("finish")
}
