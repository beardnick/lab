package main

import (
	"time"
)

type Production struct {
	ID        uint `gorm:"primaryKey"`
	CreatedAt time.Time
	UpdatedAt time.Time
	Cnt       uint
}
