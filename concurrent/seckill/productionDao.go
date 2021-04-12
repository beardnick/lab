package main

import "gorm.io/gorm"

type ProductionDao struct {
	db *gorm.DB
}

func NewProductionDao() ProductionDao {
	db, _ := DefaultDB()
	return ProductionDao{
		db: db,
	}
}

func (p ProductionDao) GetProductionCnt(id string) (cnt int, err error) {
	prod := Production{Guid: id}
	err = p.db.Take(&prod).Error
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

//func (p ProductionDao) ByGuid(id string)(, err error)  {
//	p.DB.Take(&Production{Guid: id})
//}
