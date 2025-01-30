package main

import (
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

type User struct {
	ID   int
	Name string
	Age  int
}

func TestMysqlRead(t *testing.T) {
	// setup
	dsn := os.Getenv("MYSQL")
	db, err := gorm.Open(mysql.Open(dsn), nil)
	assert.Nil(t, err)
	err = db.Exec(`DROP TABLE IF EXISTS users`).Error
	assert.Nil(t, err)
	err = db.AutoMigrate(&User{})
	assert.Nil(t, err)
	err = db.Create([]User{
		{Name: "a", Age: 1},
		{Name: "b", Age: 2},
		{Name: "c", Age: 3},
		{Name: "d", Age: 4},
	}).Error
	assert.Nil(t, err)

	// process
	tx1 := db.Begin()
	tx2 := db.Begin()
	users := []User{}

	err = tx1.Find(&users, "id > ?", 4).Error
	assert.Nil(t, err)
	// no the user 5
	assert.Equal(t, users, []User{})

	err = tx2.Create(&User{Name: "e", Age: 5}).Error
	assert.Nil(t, err)

	err = tx2.Commit().Error
	assert.Nil(t, err)

	err = tx1.Find(&users, "id > ?", 4).Error
	assert.Nil(t, err)
	// won't see the user 5, thanks to mvcc
	assert.Equal(t, users, []User{})

	err = tx1.Model(&User{}).Where("id > ?", 4).Update("age", -1).Error
	assert.Nil(t, err)

	err = tx1.Find(&users, "id > ?", 4).Error
	assert.Nil(t, err)
	// Phantom Read because you update the user 5 in tx1
	assert.Equal(t, users, []User{{ID: 5, Name: "e", Age: -1}})

	err = tx1.Commit().Error
	assert.Nil(t, err)
}
