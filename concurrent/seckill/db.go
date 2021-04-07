package main

import (
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

func OpenDb(dsn string) (db *gorm.DB, err error) {
	db, err = gorm.Open(mysql.Open(dsn), &gorm.Config{})
	return
}
