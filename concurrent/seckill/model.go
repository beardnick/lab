package main

import (
	"time"
)

type Production struct {
	ID        uint   `gorm:"primaryKey"`
	Guid      string `gorm:"size:128"`
	CreatedAt time.Time
	UpdatedAt time.Time
	Cnt       int `gorm:"size:64"`
}
