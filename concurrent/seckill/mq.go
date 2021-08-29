package main

import (
	"github.com/Shopify/sarama"
	cluster "github.com/bsm/sarama-cluster"
)

type KafkaProvicder struct {
	config  *cluster.Config
	brokers []string
}

func NewKafkaProvider(brokers []string) *KafkaProvicder {
	return &KafkaProvicder{
		config:  cluster.NewConfig(),
		brokers: brokers,
	}
}

func (k *KafkaProvicder) NewConsumer(group string, topic []string) (consumer *cluster.Consumer, err error) {
	return cluster.NewConsumer(k.brokers, group, topic, k.config)
}

func (k *KafkaProvicder) NewSyncProducer() (producer sarama.SyncProducer, err error) {
	return sarama.NewSyncProducer(k.brokers, &k.config.Config)
}

func (k *KafkaProvicder) NewAsyncProducer() (producer sarama.AsyncProducer, err error) {
	return sarama.NewAsyncProducer(k.brokers, &k.config.Config)
}
