package main

import (
	"fmt"
	"github.com/alicebob/miniredis/v2"
	"github.com/go-redis/redis/v7"
	"log"
	"sync"
)

var addr string

func LocalRedis() (client redis.UniversalClient, err error) {
	var once sync.Once
	once.Do(func() {
		mr, e := miniredis.Run()
		if e != nil {
			err = e
			return
		}
		addr = mr.Addr()
	})
	if err != nil {
		return
	}
	client = redis.NewUniversalClient(&redis.UniversalOptions{
		Addrs: []string{addr},
	})
	return
}

func NewProducer(topic string, client redis.UniversalClient) *RedisProducer {
	return &RedisProducer{
		Topic: topic,
		Conn:  client,
	}
}

type RedisProducer struct {
	Topic string
	Conn  redis.UniversalClient
}

func (p *RedisProducer) AddMsg(msg map[string]interface{}) (err error) {
	_, err = p.Conn.XAdd(&redis.XAddArgs{
		Stream: p.Topic,
		Values: msg,
	}).Result()
	return
}

func NewConsumer(topic, last string, client redis.UniversalClient) *RedisConsumer {
	return &RedisConsumer{
		LastDelivered: last,
		Topic:         topic,
		Conn:          client,
		msgchan:       make(chan map[string]interface{}),
	}
}

type RedisConsumer struct {
	LastDelivered string
	Topic         string
	Conn          redis.UniversalClient
	msgchan       chan map[string]interface{}
}

func (c *RedisConsumer) Start() {
	var once sync.Once
	go once.Do(func() {
		for {
			s, err := c.Conn.XRead(&redis.XReadArgs{Streams: []string{c.Topic, c.LastDelivered}, Count: 1}).Result()
			if err != nil {
				log.Println("xread err:", err)
				continue
			}
			msgs := s[0].Messages
			for _, msg := range msgs {
				c.LastDelivered = msg.ID
				c.msgchan <- msg.Values
			}
		}
	})
}

func (c *RedisConsumer) Msg() map[string]interface{} {
	return <-c.msgchan
}

func main() {
	client, err := LocalRedis()
	if err != nil {
		log.Fatalln(err)
	}
	p := NewProducer("mystream", client)
	c := NewConsumer("mystream", "0", client)
	err = p.AddMsg(map[string]interface{}{"value": "hello"})
	if err != nil {
		log.Fatal(err)
	}
	err = p.AddMsg(map[string]interface{}{"value": "world"})
	if err != nil {
		log.Fatal(err)
	}
	c.Start()
	s := c.Msg()
	fmt.Println("read msg:", s)
	s = c.Msg()

	fmt.Println("read msg:", s)
}
