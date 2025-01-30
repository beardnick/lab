package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"time"

	kafka "github.com/segmentio/kafka-go"
)

type Options struct {
	Cmd          string
	ConsumeTopic string
	ProduceTopic string
	Offset       int64
	Link         string
	Msg          string
	Listen       bool
}

func main() {
	opt := Options{}
	flag.StringVar(&opt.ConsumeTopic, "consume", "", "consume one topic msg")
	flag.StringVar(&opt.ProduceTopic, "produce", "", "produce one topic msg")
	flag.StringVar(&opt.Msg, "msg", "", "send msg content")
	flag.StringVar(&opt.Link, "link", "", "kafka link")
	flag.Int64Var(&opt.Offset, "offset", 0, "consume offset default is 0")
	flag.BoolVar(&opt.Listen, "listen", false, "continue listen for more msgs")
	flag.Parse()

	if opt.ConsumeTopic != "" {
		ConsumeOneMsg(opt.ConsumeTopic, opt.Link, opt.Offset, opt.Listen)
		return
	}
	if opt.ProduceTopic != "" {
		ProduceOneMsg(opt.ProduceTopic, opt.Msg, opt.Link)
		return
	}
}

func ConsumeOneMsg(topic, link string, offset int64, listen bool) {
	// make a new reader that consumes from topic-A, partition 0, at offset 42
	r := kafka.NewReader(kafka.ReaderConfig{
		Brokers:   []string{link},
		Topic:     topic,
		Partition: 0,
		MaxBytes:  10e6, // 10MB
	})
	err := r.SetOffset(offset)
	if err != nil {
		log.Fatal(err)
	}
	for {
		m, err := r.ReadMessage(context.Background())
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(string(m.Value))
		if !listen {
			break
		}
	}

	// if err := r.Close(); err != nil {
	// 	log.Fatal("failed to close reader:", err)
	// }
	// fmt.Println("after close")
}

func ProduceOneMsg(topic, msg, link string) {

	partition := 0

	conn, err := kafka.DialLeader(context.Background(), "tcp", link, topic, partition)
	if err != nil {
		log.Fatal("failed to dial leader:", err)
	}

	conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	_, err = conn.WriteMessages(
		kafka.Message{Value: []byte(msg)},
	)
	if err != nil {
		log.Fatal("failed to write messages:", err)
	}

	if err := conn.Close(); err != nil {
		log.Fatal("failed to close writer:", err)
	}
}
