package main

//作者：咔叽咔叽
//链接：https://juejin.cn/post/6844904072416346126
//来源：掘金

import (
	"fmt"
	"golang.org/x/sync/errgroup"
	"io/ioutil"
	"net/http"
	"sync"
	"time"
)

/*
* Barrier
 */
type barrierResp struct {
	Err    error
	Resp   string
	Status int
}

func barrier(endpoints ...string) {
	var g errgroup.Group
	var mu sync.Mutex

	response := make([]barrierResp, len(endpoints))

	for i, endpoint := range endpoints {
		i, endpoint := i, endpoint // create locals for closure below
		g.Go(func() error {
			res := barrierResp{}
			resp, err := http.Get(endpoint)
			if err != nil {
				return err
			}

			byt, err := ioutil.ReadAll(resp.Body)
			defer resp.Body.Close()
			if err != nil {
				return err
			}

			res.Resp = string(byt)
			mu.Lock()
			response[i] = res
			mu.Unlock()
			return err
		})
	}
	if err := g.Wait(); err != nil {
		fmt.Println(err)
	}
	for _, resp := range response {
		fmt.Println(resp.Status)
	}
}

/*
* Future
 */
type Function func(string) (string, error)

type Future interface {
	SuccessCallback() error
	FailCallback() error
	Execute(Function) (bool, chan struct{})
}

type AccountCache struct {
	Name string
}

func (a *AccountCache) SuccessCallback() error {
	fmt.Println("It's success~")
	return nil
}

func (a *AccountCache) FailCallback() error {
	fmt.Println("It's fail~")
	return nil
}

func (a *AccountCache) Execute(f Function) (bool, chan struct{}) {
	done := make(chan struct{})
	go func(a *AccountCache) {
		_, err := f(a.Name)
		if err != nil {
			_ = a.FailCallback()
		} else {
			_ = a.SuccessCallback()
		}
		done <- struct{}{}
	}(a)
	return true, done
}

func NewAccountCache(name string) *AccountCache {
	return &AccountCache{
		name,
	}
}

func testFuture() {
	var future Future
	future = NewAccountCache("Tom")
	updateFunc := func(name string) (string, error) {
		fmt.Println("cache update:", name)
		return name, nil
	}
	_, done := future.Execute(updateFunc)
	defer func() {
		<-done
	}()
}

/*
* Pipeline 模式
 */

func generator(max int) <-chan int {
	out := make(chan int, 100)
	go func() {
		for i := 1; i <= max; i++ {
			out <- i
		}
		close(out)
	}()
	return out
}

func power(in <-chan int) <-chan int {
	out := make(chan int, 100)
	go func() {
		for v := range in {
			out <- v * v
		}
		close(out)
	}()
	return out
}

func sum(in <-chan int) <-chan int {
	out := make(chan int, 100)
	go func() {
		var sum int
		for v := range in {
			sum += v
		}
		out <- sum
		close(out)
	}()
	return out
}

/*
* Worker pool
 */
type TaskHandler func(interface{})

type Task struct {
	Param   interface{}
	Handler TaskHandler
}

type WorkerPoolImpl interface {
	AddWorker()    // 增加 worker
	SendTask(Task) // 发送任务
	Release()      // 释放
}

type WorkerPool struct {
	wg   sync.WaitGroup
	inCh chan Task
}

func (d *WorkerPool) AddWorker() {
	d.wg.Add(1)
	go func() {
		for task := range d.inCh {
			task.Handler(task.Param)
		}
		d.wg.Done()
	}()
}

func (d *WorkerPool) Release() {
	close(d.inCh)
	d.wg.Wait()
}

func (d *WorkerPool) SendTask(t Task) {
	d.inCh <- t
}

func NewWorkerPool(buffer int) WorkerPoolImpl {
	return &WorkerPool{
		inCh: make(chan Task, buffer),
	}
}

/*
* Pub/Sub
 */
type Subscriber struct {
	in    chan interface{}
	id    int
	topic string
	stop  chan struct{}
}

func (s *Subscriber) Close() {
	s.stop <- struct{}{}
	close(s.in)
}

func (s *Subscriber) Notify(msg interface{}) (err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("%#v", rec)
		}
	}()
	select {
	case s.in <- msg:
	case <-time.After(time.Second):
		err = fmt.Errorf("Timeout\n")
	}
	return
}

func NewSubscriber(id int) SubscriberImpl {
	s := &Subscriber{
		id:   id,
		in:   make(chan interface{}),
		stop: make(chan struct{}),
	}
	go func() {
		for {
			select {
			case <-s.stop:
				close(s.stop)
				return
			default:
				for msg := range s.in {
					fmt.Printf("(W%d): %v\n", s.id, msg)
				}
			}
		}
	}()
	return s
}

// 订阅者需要实现的方法
type SubscriberImpl interface {
	Notify(interface{}) error
	Close()
}

// sub 订阅 pub
func Register(sub SubscriberImpl, pub *publisher) {
	pub.addSubCh <- sub
	return
}

// pub 结果定义
type publisher struct {
	subscribers []SubscriberImpl
	addSubCh    chan SubscriberImpl
	removeSubCh chan SubscriberImpl
	in          chan interface{}
	stop        chan struct{}
}

// 实例化
func NewPublisher() *publisher {
	return &publisher{
		addSubCh:    make(chan SubscriberImpl),
		removeSubCh: make(chan SubscriberImpl),
		in:          make(chan interface{}),
		stop:        make(chan struct{}),
	}
}

// 监听
func (p *publisher) start() {
	for {
		select {
		// pub 发送消息
		case msg := <-p.in:
			for _, sub := range p.subscribers {
				_ = sub.Notify(msg)
			}
		// 移除指定 sub
		case sub := <-p.removeSubCh:
			for i, candidate := range p.subscribers {
				if candidate == sub {
					p.subscribers = append(p.subscribers[:i], p.subscribers[i+1:]...)
					candidate.Close()
					break
				}
			}
		// 增加一个 sub
		case sub := <-p.addSubCh:
			p.subscribers = append(p.subscribers, sub)
		// 关闭 pub
		case <-p.stop:
			for _, sub := range p.subscribers {
				sub.Close()
			}
			close(p.addSubCh)
			close(p.in)
			close(p.removeSubCh)
			return
		}
	}
}
