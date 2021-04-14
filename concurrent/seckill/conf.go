package main

import "github.com/BurntSushi/toml"

type RedisConf struct {
	Addr   string
	Passwd string
	Db     int
}

type Config struct {
	Dsn   string
	Redis RedisConf
}

var config Config

func Conf() Config {
	return config
}

func LoadConfig(data string) (err error) {
	_, err = toml.Decode(data, &config)
	return
}
