package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"sync"
	"testing"

	"github.com/go-sql-driver/mysql"
	_ "github.com/go-sql-driver/mysql"
	"github.com/jmoiron/sqlx"
	"github.com/ory/dockertest/v3"
	"github.com/stretchr/testify/assert"
)

var db *sql.DB
var dsn string

func TestMain(m *testing.M) {
	// uses a sensible default on windows (tcp/http) and linux/osx (socket)
	pool, err := dockertest.NewPool("")
	if err != nil {
		log.Fatalf("Could not construct pool: %s", err)
	}

	// uses pool to try to connect to Docker
	err = pool.Client.Ping()
	if err != nil {
		log.Fatalf("Could not connect to Docker: %s", err)
	}

	// pulls an image, creates a container based on it and runs it
	// resource, err := pool.Run("mysql", "5.7", []string{"MYSQL_ROOT_PASSWORD=secret"})
	resource, err := pool.Run("mysql", "8.0", []string{"MYSQL_ROOT_PASSWORD=secret"})
	if err != nil {
		log.Fatalf("Could not start resource: %s", err)
	}

	resource.Expire(60)

	// exponential backoff-retry, because the application in the container might not be ready to accept connections yet
	if err := pool.Retry(func() error {
		var err error
		dsn = fmt.Sprintf("root:secret@(localhost:%s)/mysql", resource.GetPort("3306/tcp"))
		db, err = sql.Open("mysql", dsn)
		if err != nil {
			return err
		}
		return db.Ping()
	}); err != nil {
		log.Fatalf("Could not connect to database: %s", err)
	}

	code := m.Run()

	// You can't defer this because os.Exit doesn't care for defer
	if err := pool.Purge(resource); err != nil {
		log.Fatalf("Could not purge resource: %s", err)
	}

	os.Exit(code)
}

type database struct {
	DataBase string `db:"Database"`
}

var schema = "CREATE TABLE `t_order` (" +
	"`id` int NOT NULL AUTO_INCREMENT," +
	"`order_no` int DEFAULT NULL," +
	"PRIMARY KEY (`id`)," +
	"KEY `index_order` (`order_no`) USING BTREE" +
	") ENGINE=InnoDB ;"

type order struct {
	Id      int `db:"id"`
	OrderNo int `db:"order_no"`
}

func TestDeadLock(t *testing.T) {
	db, err := sqlx.Open("mysql", dsn)
	assert.Nil(t, err)

	_, err = db.Exec(schema)
	assert.Nil(t, err)
	insertOrder := `INSERT INTO t_order(order_no) VALUES(?)`
	for i := 1001; i <= 1006; i++ {
		_, e := db.Exec(insertOrder, i)
		assert.Nil(t, e)
	}

	tx1 := db.MustBegin()
	tx2 := db.MustBegin()

	// to see which lock has been added, use
	// select * from performance_schema.data_locks

	// will lock (1006, +∞)
	tx1.MustExec(`select id from t_order where order_no = 1007 for update`)
	// will lock (1006, +∞)
	tx2.MustExec(`select id from t_order where order_no = 1008 for update`)

	wg := &sync.WaitGroup{}
	wg.Add(2)

	go func() {
		// will cause deadlock here
		_, e := tx1.Exec(`insert into t_order(order_no) values(1007)`)
		//  mysql dead lock error code is 1213
		assert.ErrorIs(t, e, &mysql.MySQLError{Number: 1213})
		wg.Done()
	}()

	go func() {
		_, e := tx2.Exec(`insert into t_order(order_no) values(1008)`)
		assert.Nil(t, e, "error in tx2")
		wg.Done()
	}()

	wg.Wait()

	err = tx1.Commit()
	assert.Nil(t, err)
	err = tx2.Commit()
	assert.Nil(t, err)
}
