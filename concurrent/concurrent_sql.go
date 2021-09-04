package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"

	"github.com/go-redis/redis/v8"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

type Product struct {
	ID   uint   `json:"id" gorm:"primaryKey"`
	Name string `json:"name" gorm:"size:128"`
	Cnt  int    `json:"cnt" gorm:"size:64"`
}

var (
	StatusSuccess = "Success"
	StatusSoldOut = "SoldOut"
)

type Order struct {
	ID      uint   `json:"id,omitempty" gorm:"primaryKey"`
	OrderId string `json:"order_id,omitempty"`
	Name    string `json:"name,omitempty" gorm:"size:128"`
	Cnt     int    `json:"cnt,omitempty" gorm:"size:64"`
	Status  string `json:"staus,omitempty"`
}

var (
	pool = sync.Map{}
)

func OpenDb(dsn string) (db *gorm.DB, err error) {
	v, ok := pool.Load(dsn)
	if ok {
		db = v.(*gorm.DB)
		return
	}
	db, err = gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		return
	}
	sqlDb, err := db.DB()
	if err != nil {
		return
	}
	sqlDb.SetMaxOpenConns(100)
	pool.Store(dsn, db)
	return
}

type ProductProvider interface {
	SetProduct(p Product) (err error)
	OrderProduct(orderid, name string, n int) (err error)
	ProductCnt(name string) (cnt int, err error)
}

type MysqlProduct struct {
	Dsn string
}

func NewMysqlProduct() *MysqlProduct {
	return &MysqlProduct{
		Dsn: os.Getenv("mysql_dsn"),
	}
}

func (m *MysqlProduct) SetProduct(p Product) (err error) {
	db, err := OpenDb(m.Dsn)
	if err != nil {
		return
	}
	models := []interface{}{&Product{}, &Order{}}
	for _, model := range models {
		err = db.AutoMigrate(model)
		if err != nil {
			return
		}
	}
	err = db.Model(&p).Where("name = ?", p.Name).Update("cnt", p.Cnt).Error
	if err != nil {
		if gorm.ErrRecordNotFound == err {
			err = db.Create(&p).Error
		}
	}
	return
}

func (m *MysqlProduct) OrderProduct(orderId string, name string, n int) (err error) {
	db, err := OpenDb(m.Dsn)
	if err != nil {
		return
	}
	// TODO: repeatable read update cnt cannot be read
	err = db.Transaction(func(tx *gorm.DB) (err error) {
		order := Order{}
		order.OrderId = orderId
		err = tx.Take(&order, Order{OrderId: orderId}).Error
		if err == nil {
			return
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return
		}
		product := Product{
			Name: name,
		}
		err = tx.Take(&product, Product{Name: name}).Error
		if err != nil {
			return
		}
		if product.Cnt < n {
			err = tx.Create(&Order{
				Name:    name,
				OrderId: orderId,
				Cnt:     n,
				Status:  StatusSoldOut,
			}).Error
			return
		}
		// errors.Is(err, gorm.ErrRecordNotFound)
		err = tx.Model(&Product{}).
			Where("name = ?", name).
			Update("cnt", gorm.Expr("cnt - ?", n)).
			Error
		if err != nil {
			return
		}
		err = tx.Create(&Order{
			Name:    name,
			OrderId: orderId,
			Cnt:     n,
			Status:  StatusSuccess,
		}).Error
		return
	})
	return
}

func (m *MysqlProduct) ProductCnt(name string) (cnt int, err error) {
	db, err := OpenDb(m.Dsn)
	if err != nil {
		return
	}
	p := Product{}
	err = db.Take(&p, Product{Name: name}).Error
	return
}

type RedisProduct struct {
	Rdb *redis.Client
}

func NewRedisProduct() *RedisProduct {
	rdb := redis.NewClient(&redis.Options{
		Addr:     os.Getenv("redis_url"),
		Password: os.Getenv("redis_passwd"),
	})
	return &RedisProduct{
		Rdb: rdb,
	}
}

func (r *RedisProduct) SetProduct(p Product) (err error) {
	_, err = r.Rdb.HSet(context.Background(), "products", p.Name, p.Cnt).Result()
	return
}

// subGreater products iphone 10
var subGreater = redis.NewScript(`
if redis.call("hget",KEYS[1],ARGV[1]) + ARGV[2] >= 0 then
    return redis.call("hincrby",KEYS[1],ARGV[1],ARGV[2])
else
	return 0
end
`)

// orderTransaction products orderId iphone 10
var orderTransaction = redis.NewScript(`
if redis.call("hget","orders",ARGV[1]) != 0 then
    return 0
if redis.call("hget",KEYS[1],ARGV[2]) + ARGV[3] >= 0 then
    redis.call("hincrby",KEYS[1],ARGV[1],ARGV[2])
    return redis.call("hset","orders",ARGV[1],"Sucess")
else
    return redis.call("hset","orders",ARGV[1],"SoldOut")
`)

func (r *RedisProduct) OrderProduct(orderId string, name string, n int) (err error) {
	_, err = subGreater.Run(context.Background(), r.Rdb, []string{"products"}, name, -n).Result()
	if err != nil {
		fmt.Printf("err %v\n", err)
	}
	return
}

func (r *RedisProduct) ProductCnt(name string) (cnt int, err error) {
	cnt, err = r.Rdb.HGet(context.Background(), "products", name).Int()
	return
}
