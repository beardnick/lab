package main

import (
	"crypto/sha1"
	"encoding/hex"

	"github.com/go-redis/redis"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

var defaultRedisClient *redis.Client
var defaultMysqlClient *gorm.DB
var defaultKafkaClient *KafkaProvicder
var subScriptSha string

const subScript = `
local cnt = tonumber(redis.call('HGET', KEYS[1], ARGV[1]))
local sub = tonumber(ARGV[2])

if sub > cnt then
    return redis.error_reply("Subtract number is larger than count")
else
    cnt = cnt - sub
    redis.call('HSET', KEYS[1], ARGV[1], cnt)
    return cnt
end
`

func SetupDB(conf Config) (err error) {
	c := conf.Redis
	defaultRedisClient, err = OpenRedis(c.Addr, c.Passwd, c.Db)
	if err != nil {
		return
	}
	err = setupRedis(defaultRedisClient)
	if err != nil {
		return
	}
	defaultMysqlClient, err = OpenDb(Conf().Mysql)
	if err != nil {
		return
	}
	defaultKafkaClient = NewKafkaProvider(conf.Kafka.Brokers)
	return
}

func SetupService() (err error) {
	err = SetupOrderDao()
	if err != nil {
		return
	}
	err = NewOrderDao().StartConsumeEvents()
	return
}

func setupRedis(rdb *redis.Client) (err error) {
	sha1 := sha1.New()
	sha1.Write([]byte(subScript))
	subScriptSha = hex.EncodeToString(sha1.Sum(nil))
	rdb.ScriptLoad(subScript)
	return
}

func OpenDb(dsn string) (db *gorm.DB, err error) {
	db, err = gorm.Open(mysql.Open(dsn), &gorm.Config{})
	return
}

func DefaultDB() (db *gorm.DB, err error) {
	return defaultMysqlClient, nil
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
	return defaultRedisClient, nil
}

func DefaultKafka() (client *KafkaProvicder) {
	return defaultKafkaClient
}
