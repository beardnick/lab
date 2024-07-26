package main

import (
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/samber/lo"
)

type Server struct {
	database *DataBase
	address  string
}

func (s *Server) Handle(conn net.Conn) {
	defer conn.Close()
	buf := make([]byte, 1024)
	var trans *Trans = nil
	for {
		n, err := conn.Read(buf)
		if err != nil {
			log.Println(err)
			continue
		}
		command := strings.Split(strings.TrimSpace(string(buf[:n])), " ")
		switch command[0] {
		case "exit":
			break
		case "set":
			if trans == nil {
				trans = s.database.TransBegin(false)
				trans.
					Set(command[1], command[2]).
					Commit()
				trans = nil
				continue
			}
			trans.Set(command[1], command[2])
		case "get":
			if trans == nil {
				trans = s.database.TransBegin(true)
				value := trans.Get(command[1])
				trans = nil
				conn.Write([]byte(fmt.Sprintln(value)))
				continue
			}
			value := trans.Get(command[1])
			conn.Write([]byte(fmt.Sprintln(value)))
		case "begin":
			trans = s.database.TransBegin(false)
		case "commit":
			if trans == nil {
				conn.Write([]byte(fmt.Sprintln("no trans")))
				continue
			}
			trans.Commit()
			trans = nil
		}
	}
}

func (s *Server) Start() {
	s.database = &DataBase{
		version:          0,
		dataMutex:        sync.Mutex{},
		data:             map[string]Record{},
		uncommittedMutex: sync.Mutex{},
		uncommittedTrans: []Trans{},
	}
	l, err := net.Listen("tcp", s.address)
	if err != nil {
		log.Fatalln(err)
	}
	for {
		conn, err := l.Accept()
		if err != nil {
			log.Println(err)
			continue
		}
		go handle(conn)
	}

}

type RecordLog struct {
	data string
	tx   int
}

type Record struct {
	versions []RecordLog
	sync.Mutex
}

type DataBase struct {
	version          int32
	dataMutex        sync.Mutex
	data             map[string]Record
	uncommittedMutex sync.Mutex
	uncommittedTrans []Trans
}

type Trans struct {
	keys     []string
	database *DataBase
	readView ReadView
	tx       int
}

type ReadView struct {
	uncommittedTrans []Trans
}

func (t *Trans) Set(key, value string) (nt *Trans) {
	t.database.Set(key, RecordLog{
		data: value,
		tx:   t.tx,
	})
	t.keys = append(t.keys, key)
	return t
}

func (t *Trans) Get(key string) (value string) {
	transIds := lo.Map(t.readView.uncommittedTrans, func(item Trans, index int) int { return item.tx })
	record, ok := t.database.Get(key)
	if !ok {
		return ""
	}
	record.Lock()
	committedRecords := lo.Filter(record.versions, func(item RecordLog, index int) bool { return !lo.Contains(transIds, item.tx) })
	record.Unlock()
	latestRecord := lo.MaxBy(committedRecords, func(a, b RecordLog) bool { return a.tx > b.tx })
	return latestRecord.data
}

// func (t *Trans) Del(key string) (nt *Trans) {
// }

func (d *DataBase) GenTransId(read bool) (tid int) {
	if read {
		return int(atomic.LoadInt32(&d.version))
	}
	return int(atomic.AddInt32(&d.version, 1))
}

func (d *DataBase) TransBegin(read bool) (t *Trans) {
	t = &Trans{
		database: d,
		readView: d.ReadView(),
		tx:       d.GenTransId(read),
	}
	if read {
		return t
	}
	d.uncommittedMutex.Lock()
	d.uncommittedTrans = append(d.uncommittedTrans, *t)
	d.uncommittedMutex.Unlock()
	return t
}

func (t *Trans) Commit() {
	t.database.TransCommit(t)
}

func (t *Trans) Rollback() {
	t.database.TransRollback(t)
}

func (d *DataBase) TransCommit(t *Trans) {
	d.uncommittedMutex.Lock()
	for i := 0; i < len(d.uncommittedTrans); i++ {
		if d.uncommittedTrans[i].tx == t.tx {
			d.uncommittedTrans = append(d.uncommittedTrans[:i], d.uncommittedTrans[i+1:]...)
			break
		}
	}
	d.uncommittedMutex.Unlock()
	return
}

func (d *DataBase) TransRollback(t *Trans) (nt *Trans) {
	d.uncommittedMutex.Lock()
	for i := 0; i < len(d.uncommittedTrans); i++ {
		if d.uncommittedTrans[i].tx == t.tx {
			d.uncommittedTrans = append(d.uncommittedTrans[:i], d.uncommittedTrans[i+1:]...)
			break
		}
	}
	d.uncommittedMutex.Unlock()
	d.dataMutex.Lock()
	records := lo.Map[string, Record](t.keys, func(item string, index int) Record { return d.data[item] })
	d.dataMutex.Unlock()
	for _, r := range records {
		r.Lock()
		for i := 0; i < len(r.versions); i++ {
			if r.versions[i].tx == t.tx {
				r.versions = append(r.versions[:i], r.versions[i+1:]...)
				break
			}
		}
		r.Unlock()
	}
	return
}

func (d *DataBase) Set(key string, value RecordLog) {
	d.dataMutex.Lock()
	v, ok := d.data[key]
	d.dataMutex.Unlock()
	if !ok {
		v = Record{
			versions: []RecordLog{},
			Mutex:    sync.Mutex{},
		}
	}
	v.Lock()
	v.versions = append([]RecordLog{value}, v.versions...)
	v.Unlock()

	d.dataMutex.Lock()
	d.data[key] = v
	d.dataMutex.Unlock()
}

func (d *DataBase) Get(key string) (Record, bool) {
	r := Record{}
	ok := false
	d.dataMutex.Lock()
	r, ok = d.data[key]
	d.dataMutex.Unlock()
	return r, ok
}

func (d *DataBase) ReadView() ReadView {
	view := ReadView{uncommittedTrans: []Trans{}}
	d.uncommittedMutex.Lock()
	view.uncommittedTrans = append(view.uncommittedTrans, d.uncommittedTrans...)
	d.uncommittedMutex.Unlock()
	return view
}
