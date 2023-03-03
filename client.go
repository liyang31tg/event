package event

import (
	"bytes"
	"fmt"
	"reflect"
	"sync"
	"time"
)

type client struct {
	events map[EventType][]*methodType
	mutex  sync.Mutex //protect following
	CreateCodeC
	codec    Codec
	starting bool //是否开启
	shutdown bool // server shutdown
}

type CreateCodeC func() Codec

func NewClient(createcodecf CreateCodeC) *client {
	c := &client{
		events:      make(map[EventType][]*methodType),
		CreateCodeC: createcodecf,
		codec:       nil,
	}
	go c.keepAlive()
	go c.input()
	return c
}

func (this *client) keepAlive() {
	for {

		time.Sleep(1 * time.Second)
	}

}

func (this *client) input() {
	for {
		var body body
		err := this.codec.Read(&body)
		if err != nil {
			break
		}
		go this.call(&body)
	}
}

func (this *client) call(body *body) {

}

// 监听事件
func (this *client) On(t EventType, Func any) error {
	rt := reflect.TypeOf(Func)
	if rt.Kind() != reflect.Func {
		panic("on a not func")
	}
	rv := reflect.ValueOf(Func)
	mType := &methodType{
		method:   rv,
		ArgCount: rt.NumIn(),
	}
	events := this.events[t]
	events = append(events, mType)
	this.events[t] = events
	this.mutex.Lock()
	defer this.mutex.Unlock()

	return nil
}

// 触发事件
func (this *client) GoEmit(t EventType, args ...any) error {
	this.mutex.Lock()
	defer this.mutex.Unlock()
	if this.starting {
		return this.codec.Write(t, args...)
	} else {
		return fmt.Errorf("client is not start")
	}
}

// default
func newLocalClient() *client {
	c := NewClient(func() Codec {
		var buf bytes.Buffer
		return NewGobCodec(&buf)
	})
	c.starting = true
	c.codec = c.CreateCodeC()
	return c
}

var defalutLocalClient = newLocalClient()

func On(t EventType, Func any) error {
	return defalutLocalClient.On(t, Func)
}

func GoEmit(t EventType, args ...any) error {
	return defalutLocalClient.GoEmit(t, args...)
}
