package main

import (
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// todo: use db pool
func OpenDb(dsn string) (db *gorm.DB, err error) {
	db, err = gorm.Open(mysql.Open(dsn), &gorm.Config{})
	return
}

func DefaultDB() (db *gorm.DB, err error) {
	return OpenDb(Conf().Dsn)
}
