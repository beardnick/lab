package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"testing"

	"github.com/stretchr/testify/assert"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
)

type TestIndexRow struct {
	ID uint   `gorm:"primaryKey"`
	A  string `gorm:"type:varchar(30)"`
	B  string `gorm:"type:varchar(30)"`
	C  string `gorm:"type:varchar(30)"`
	D  string `gorm:"type:varchar(30)"`
}

func (TestIndexRow) TableName() string {
	return "test_index"
}

func Test_InsertRows(t *testing.T) {
	dsn := os.Getenv("MYSQL")
	db, err := gorm.Open(mysql.Open(dsn), nil)
	assert.Nil(t, err)

	err = db.Exec(`DROP TABLE IF EXISTS test_index`).Error
	assert.Nil(t, err)

	assert.Nil(t, db.AutoMigrate(&TestIndexRow{}))
	assert.Nil(t, db.Exec(`create index a_b_c_d on test_index(a,b,c,d)`).Error)

	count := 10000000
	rows := []TestIndexRow{}
	for i := 0; i < count; i++ {
		v := strconv.Itoa(i)
		rows = append(rows, TestIndexRow{
			A: v,
			B: v,
			C: v,
			D: v,
		})
		if len(rows) == 10000 {
			assert.Nil(t, db.Create(rows).Error)
			rows = []TestIndexRow{}
			fmt.Println(i)
		}
	}
}

func Test_ExplainQuerys(t *testing.T) {
	dsn := os.Getenv("MYSQL")
	db, err := gorm.Open(mysql.Open(dsn), nil)
	assert.Nil(t, err)
	data := map[string]interface{}{}
	db.Raw(`explain SELECT a FROM test_index WHERE b = '1' and c = '1'`).Scan(&data)
	out, err := json.MarshalIndent(data, "", "  ")
	assert.Nil(t, err)
	fmt.Println(string(out))
}
