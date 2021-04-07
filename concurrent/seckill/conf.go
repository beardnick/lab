package main

import "github.com/BurntSushi/toml"

type Config struct {
	Dsn string
}

var config Config

func Conf() Config {
	return config
}

func LoadConfig(data string) (err error) {
	_, err = toml.Decode(data, &config)
	return
}
