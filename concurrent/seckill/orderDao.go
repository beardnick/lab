package main

import "gorm.io/gorm"


type OrderDao struct {
	db *gorm.DB
}

func NewOrderDao() OrderDao {
	db, _ := DefaultDB()
	return OrderDao{
		db: db,
	}
}


func (p OrderDao) GetOrder(id string) (cnt int, err error) {
	order := Order{Guid: id}
	err = p.db.Take(&order).Error
	return
}

func (p OrderDao) Insert(order Order) (id string, err error) {
	order.Guid, err = NewId()
	if err != nil {
		return
	}
	id = order.Guid
	err = p.db.Create(&order).Error
	return
}
