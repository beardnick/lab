package main

import (
	"time"
)

type Production struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	Guid      string    `json:"guid" gorm:"size:128"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Cnt       int       `json:"cnt" gorm:"size:64"`
}
