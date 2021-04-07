package main

import "gorm.io/gorm"

type ProductionDao struct {
	DB *gorm.DB
}

func (p ProductionDao) GetProductionCnt(id string) (cnt int, err error) {
	prod := Production{Guid: id}
	err = p.DB.Take(&prod).Error
	cnt = prod.Cnt
	return
}

func (p ProductionDao) Insert(prod Production) (err error) {
	err = p.DB.Create(prod).Error
	return
}

//func (p ProductionDao) ByGuid(id string)(, err error)  {
//	p.DB.Take(&Production{Guid: id})
//}
