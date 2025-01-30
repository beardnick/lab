package main

import (
	"encoding/json"
	"log"
	"time"

	"github.com/Shopify/sarama"
	"gorm.io/gorm"
)

const OrderKey = "orders"

const (
	OrderInsertEvent = OrderKey + "_insert"
)

var defaultOrderDao OrderDao

type OrderDao struct {
	db        *gorm.DB
	producer  sarama.SyncProducer
	consumers []sarama.PartitionConsumer
}

func SetupOrderDao() (err error) {
	db, _ := DefaultDB()
	kafka := DefaultKafka()
	producer, err := kafka.NewSyncProducer()
	if err != nil {
		return
	}
	consumer, err := kafka.NewConsumer()
	if err != nil {
		return
	}
	partitions, err := consumer.Partitions(OrderInsertEvent)
	if err != nil {
		return
	}
	consumers := []sarama.PartitionConsumer{}
	for _, part := range partitions {
		pconsumer, err := consumer.ConsumePartition(OrderInsertEvent, part, sarama.OffsetNewest)
		if err != nil {
			return err
		}
		consumers = append(consumers, pconsumer)
	}
	defaultOrderDao = OrderDao{
		db:        db,
		producer:  producer,
		consumers: consumers,
	}
	return
}

func NewOrderDao() OrderDao {
	return defaultOrderDao
}

func (p OrderDao) GetOrder(id string) (cnt int, err error) {
	order := Order{Guid: id}
	err = p.db.Take(&order).Error
	return
}

func (p OrderDao) Insert(order Order) (id string, err error) {
	order.Guid, err = NewId()
	if err != nil {
		return
	}
	id = order.Guid
	data, err := json.Marshal(order)
	if err != nil {
		return
	}
	_, _, err = p.producer.SendMessage(
		&sarama.ProducerMessage{
			Topic: OrderInsertEvent,
			Value: sarama.ByteEncoder(data),
		},
	)
	return
}

func (p OrderDao) StartConsumeEvents() (err error) {
	counter := make(chan int)
	go func() {
		insertCounter := 0
		for range counter {
			insertCounter += 1
			if insertCounter%1000 == 0 {
				log.Println("consumer sleeping...")
				time.Sleep(time.Millisecond * 500)
			}
		}
	}()
	for _, pconsumer := range p.consumers {
		go func() {
			for msg := range pconsumer.Messages() {
				switch msg.Topic {
				case OrderInsertEvent:
					order := Order{}
					// todo: handle this error
					err = json.Unmarshal(msg.Value, &order)
					if err != nil {
						log.Printf("handle %s msg error: %+v", OrderInsertEvent, err)
						continue
					}
					log.Printf("handle %s msg %s", OrderInsertEvent, order.Guid)
					err = p.insert(order)
					if err != nil {
						log.Printf("insert order %s failed error:%+v", order.Guid, err)
					}
					counter <- 1
				default:
				}
			}
		}()
	}
	return
}

func (p OrderDao) insert(order Order) (err error) {
	return p.db.Table(OrderKey).Create(&order).Error
}

func (p OrderDao) OrderSum(productionId string) (cnt int, err error) {
	err = p.db.Table(OrderKey).Select("sum(cnt)").Where("production_id = ?", productionId).Scan(&cnt).Error
	return
}
