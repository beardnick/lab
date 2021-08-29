package main

import (
	"os"
	"sync"

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

func InitProduct(p Product) (err error) {
	db, err := OpenDb(os.Getenv("mysql_dsn"))
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

func OrderProduct(name string, n int) (err error) {
	db, err := OpenDb(os.Getenv("mysql_dsn"))
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

func ProductCnt(name string) (cnt int, err error) {
	db, err := OpenDb(os.Getenv("mysql_dsn"))
	if err != nil {
		return
	}
	p := Product{}
	err = db.Take(&p, Product{Name: name}).Error
	return
}
