package main

import (
	"time"
)

type Production struct {
	ID        uint      `json:"id" gorm:"primaryKey"`
	Guid      string    `json:"guid" gorm:"size:128"`
	Name      string    `json:"name" gorm:"size:128"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Cnt       int       `json:"cnt" gorm:"size:64"`
}

type Order struct {
	ID         uint   `json:"id" gorm:"primaryKey"`
	Guid       string `json:"guid" gorm:"size:128"`
	UserName   string `json:"user_name" gorm:"size:128"`
	Production string `json:"production"`
	Cnt        int    `json:"cnt"`
}
