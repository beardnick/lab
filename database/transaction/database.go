package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/samber/lo"
)

type HandleResult string

const (
	ResultOk  = "ok"
	ResultErr = "err"
)

type Server struct {
	database *DataBase
	address  string
}

func (s *Server) Handle(ctx context.Context, conn net.Conn) {
	sessCtx, cancel := context.WithCancel(ctx)
	sess := Session{
		conn:     conn,
		trans:    nil,
		database: s.database,
		stopCtx:  cancel,
	}
	sess.Handle(sessCtx)
}

func (s *Session) Close() (err error) {
	s.stopCtx()
	return
}

func (s *Session) Handle(ctx context.Context) {
	buf := make([]byte, 1024)
	waitCloseCtx(ctx, s.conn)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		n, err := s.conn.Read(buf)
		if err != nil {
			log.Println(err)
			continue
		}
		data := string(buf[:n])
		datas := strings.Split(data, "\n")
		datas = lo.Filter(datas, func(v string, _ int) bool { return v != "" })
		commands := lo.Map(
			datas,
			func(item string, index int) []string { return strings.Split(item, " ") },
		)
		for _, v := range commands {
			result := s.HandleCommand(v)
			if result.Code == ResultOk {
				_, err = s.conn.Write([]byte(resultMsg(result) + "\n"))
				if err != nil {
					log.Printf("response conn %v err:%v\n", s.conn.RemoteAddr(), err)
				}
			}
		}
	}
}

func resultMsg(result Result) string {
	if result.Code == ResultOk {
		if result.Msg != "" {
			return result.Msg
		}
		return ResultOk
	}
	if result.Code == ResultErr {
		if result.Msg != "" {
			return fmt.Sprintf("%s:%s", result.Code, result.Msg)
		}
		return ResultErr
	}
	return ""
}

type Session struct {
	conn     net.Conn
	trans    *Trans
	database *DataBase
	stopCtx  context.CancelFunc
}
type Result struct {
	Code string
	Msg  string
}

func (s *Session) HandleCommand(command []string) (res Result) {
	switch command[0] {
	case "exit":
		s.Close()
	case "set":
		if s.trans == nil {
			s.trans = s.database.TransBegin(false)
			s.trans.
				Set(command[1], command[2]).
				Commit()
			s.trans = nil
			break
		}
		s.trans.Set(command[1], command[2])
	case "get":
		if s.trans == nil {
			s.trans = s.database.TransBegin(true)
			value := s.trans.Get(command[1])
			s.trans = nil
			return Result{Code: ResultOk, Msg: value}
		}
		value := s.trans.Get(command[1])
		return Result{Code: ResultOk, Msg: value}
	case "begin":
		s.trans = s.database.TransBegin(false)
	case "commit":
		if s.trans == nil {
			return Result{Code: ResultErr, Msg: "no trans"}
		}
		s.trans.Commit()
		s.trans = nil
	case "rollback":
		if s.trans == nil {
			return Result{Code: ResultErr, Msg: "no trans"}
		}
		s.trans.Rollback()
		s.trans = nil
	}
	return Result{Code: ResultOk, Msg: ""}
}

func (s *Server) Start(ctx context.Context) {
	log.SetFlags(log.Llongfile)
	go s.start(ctx)
}

func (s *Server) start(ctx context.Context) {
	s.database = &DataBase{
		version:          0,
		dataMutex:        sync.Mutex{},
		data:             map[string]*Record{},
		uncommittedMutex: sync.Mutex{},
		uncommittedTrans: map[int]Trans{},
	}
	l, err := net.Listen("tcp", s.address)
	if err != nil {
		log.Fatalln(err)
	}
	waitCloseCtx(ctx, l)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		conn, err := l.Accept()
		if err != nil {
			log.Println(err)
			continue
		}
		go s.Handle(ctx, conn)
	}

}

type UndoLog struct {
	data string
	tx   int
}

type Record struct {
	versions []UndoLog
	sync.Mutex
}

type DataBase struct {
	version          int32
	dataMutex        sync.Mutex
	data             map[string]*Record
	uncommittedMutex sync.Mutex
	uncommittedTrans map[int]Trans
}

type Trans struct {
	upLimit  int
	keys     []string
	database *DataBase
	readView ReadView
	tx       int
}

type ReadView struct {
	uncommittedTrans map[int]Trans
}

func (t *Trans) Set(key, value string) (nt *Trans) {
	t.database.Set(key, UndoLog{
		data: value,
		tx:   t.tx,
	})
	t.keys = append(t.keys, key)
	return t
}

func (t *Trans) Get(key string) (value string) {
	record, ok := t.database.Get(key)
	if !ok {
		return ""
	}
	record.Lock()
	visibleRecords := []UndoLog{}
	for _, v := range record.versions {
		if v.tx > t.upLimit {
			continue
		}
		if v.tx == t.tx {
			visibleRecords = append(visibleRecords, v)
		}
		_, uncommitted := t.readView.uncommittedTrans[v.tx]
		if !uncommitted {
			visibleRecords = append(visibleRecords, v)
		}
	}
	record.Unlock()
	latestRecord := lo.MaxBy(visibleRecords, func(a, b UndoLog) bool { return a.tx > b.tx })
	return latestRecord.data
}

// func (t *Trans) Del(key string) (nt *Trans) {
// }

func (d *DataBase) GenTransId() (tid int) {
	return int(atomic.AddInt32(&d.version, 1))
}

func (d *DataBase) GetTransId() (tid int) {
	return int(atomic.LoadInt32(&d.version))
}

func (d *DataBase) TransBegin(read bool) (t *Trans) {
	if read {
		t = &Trans{
			upLimit:  d.GetTransId(),
			database: d,
			readView: d.ReadView(),
		}
		return
	}

	t = &Trans{
		database: d,
		readView: d.ReadView(),
		tx:       d.GenTransId(),
	}
	t.upLimit = t.tx

	d.uncommittedMutex.Lock()
	d.uncommittedTrans[t.tx] = *t
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
	delete(d.uncommittedTrans, t.tx)
	d.uncommittedMutex.Unlock()
}

func (d *DataBase) TransRollback(t *Trans) (nt *Trans) {
	d.uncommittedMutex.Lock()
	for i := 0; i < len(d.uncommittedTrans); i++ {
		if d.uncommittedTrans[i].tx == t.tx {
			delete(d.uncommittedTrans, t.tx)
			break
		}
	}
	d.uncommittedMutex.Unlock()
	d.dataMutex.Lock()
	records := lo.Map(t.keys, func(item string, index int) *Record { return d.data[item] })
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

func (d *DataBase) Set(key string, value UndoLog) {
	d.dataMutex.Lock()
	v, ok := d.data[key]
	d.dataMutex.Unlock()
	if !ok {
		v = &Record{
			versions: []UndoLog{},
			Mutex:    sync.Mutex{},
		}
	}
	v.Lock()
	v.versions = append([]UndoLog{value}, v.versions...)
	v.Unlock()

	d.dataMutex.Lock()
	d.data[key] = v
	d.dataMutex.Unlock()
}

func (d *DataBase) Get(key string) (*Record, bool) {
	var r *Record
	ok := false
	d.dataMutex.Lock()
	r, ok = d.data[key]
	d.dataMutex.Unlock()
	return r, ok
}

func (d *DataBase) ReadView() ReadView {
	view := ReadView{uncommittedTrans: make(map[int]Trans, len(d.uncommittedTrans))}
	d.uncommittedMutex.Lock()
	for k, v := range d.uncommittedTrans {
		view.uncommittedTrans[k] = v
	}
	d.uncommittedMutex.Unlock()
	return view
}

type Closer interface {
	Close() error
}

func waitCloseCtx(ctx context.Context, closer Closer) {
	go func() {
		for {
			select {
			case <-ctx.Done():
				closer.Close()
				return
			default:
				time.Sleep(time.Second)
			}
		}
	}()
}
