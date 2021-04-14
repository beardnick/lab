package main

import (
	"github.com/go-redis/redis"
	"gorm.io/gorm"
)

type ProductionDao struct {
	db  *gorm.DB
	rdb *redis.Client
}

func NewProductionDao() ProductionDao {
	db, _ := DefaultDB()
	rdb, _ := DefaultRedis()
	return ProductionDao{
		db:  db,
		rdb: rdb,
	}
}

func (p ProductionDao) model() *gorm.DB {
	return p.db.Model(&Production{})
}

func (p ProductionDao) cache() string {
	return "production"
}

func (p ProductionDao) cnt() string {
	return p.cache() + ".cnt"
}

func (p ProductionDao) GetProductionCnt(id string) (cnt int, err error) {
	prod := Production{}
	err = p.db.Where("guid", id).First(&prod).Error
	cnt = prod.Cnt
	return
}

func (p ProductionDao) Insert(prod Production) (id string, err error) {
	prod.Guid, err = NewId()
	if err != nil {
		return
	}
	id = prod.Guid
	err = p.db.Create(&prod).Error
	return
}

func (p ProductionDao) UpdateCnt(cnt int, production string) (id string, err error) {
	err = p.model().Where("guid", production).Update("cnt", cnt).Error
	return
}

func (p ProductionDao) CacheCnt(production string, cnt int) (err error) {
	err = p.rdb.HSet(p.cnt(), production, cnt).Err()
	return
}

func (p ProductionDao) CacheCntOf(production string) (cnt int, err error) {
	cnt, err = p.rdb.HGet(p.cnt(), production).Int()
	return
}
