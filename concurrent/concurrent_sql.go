package main

import (
	"context"
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
	OrderProduct(name string, n int) (err error)
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
	models := []interface{}{&Product{}}
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

func (m *MysqlProduct) OrderProduct(name string, n int) (err error) {
	db, err := OpenDb(m.Dsn)
	if err != nil {
		return
	}
	err = db.Model(&Product{}).
		Where("name = ?", name).
		Where("cnt >= ?", n).
		Update("cnt", gorm.Expr("cnt - ?", n)).
		Error
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
if redis.call("hget",KEYS[1],ARGV[1]) >= ARGV[2] then
    return redis.call("hincrby",KEYS[1],ARGV[1],ARGV[2])
else
	return 0
end
`)

func (r *RedisProduct) OrderProduct(name string, n int) (err error) {
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
