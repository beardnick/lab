package main

import (
	"io/ioutil"
	"log"
	"os"
)

func InitConfig() (err error) {
	f, err := os.Open("config.toml")
	if err != nil {
		return
	}
	data, err := ioutil.ReadAll(f)
	if err != nil {
		return
	}
	err = LoadConfig(string(data))
	return
}

func InitDb() (err error) {
	db, err := OpenDb(Conf().Dsn)
	if err != nil {
		return
	}
	err = db.AutoMigrate(&Production{})
	return
}

func main() {
	err := InitConfig()
	if err != nil {
		log.Fatal("init config failed", err)
	}
	err = InitDb()
	if err != nil {
		log.Fatal("init db failed", err)
	}
}
