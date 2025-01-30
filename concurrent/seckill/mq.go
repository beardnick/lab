package main

import (
	"github.com/Shopify/sarama"
)

type KafkaProvicder struct {
	config  *sarama.Config
	brokers []string
}

func NewKafkaProvider(brokers []string) *KafkaProvicder {
	c := sarama.NewConfig()
	c.Producer.Return.Successes = true
	return &KafkaProvicder{
		config:  c,
		brokers: brokers,
	}
}

func (k *KafkaProvicder) NewConsumer() (consumer sarama.Consumer, err error) {
	return sarama.NewConsumer(k.brokers, k.config)
}

func (k *KafkaProvicder) NewSyncProducer() (producer sarama.SyncProducer, err error) {
	return sarama.NewSyncProducer(k.brokers, k.config)
}

func (k *KafkaProvicder) NewAsyncProducer() (producer sarama.AsyncProducer, err error) {
	return sarama.NewAsyncProducer(k.brokers, k.config)
}
