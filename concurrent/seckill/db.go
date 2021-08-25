package main

import (
	"github.com/go-redis/redis"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// todo: use db pool
func OpenDb(dsn string) (db *gorm.DB, err error) {
	db, err = gorm.Open(mysql.Open(dsn), &gorm.Config{})
	return
}

func DefaultDB() (db *gorm.DB, err error) {
	return OpenDb(Conf().Mysql)
}

func OpenRedis(addr, passwd string, db int) (rdb *redis.Client, err error) {
	rdb = redis.NewClient(&redis.Options{
		Addr:     addr,
		Password: passwd,
		DB:       db, // use default DB
	})
	_, err = rdb.Ping().Result()
	return
}

func DefaultRedis() (rdb *redis.Client, err error) {
	c := config.Redis
	return OpenRedis(c.Addr, c.Passwd, c.Db)
}
