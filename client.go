package event

import (
	"bytes"
	"encoding/gob"
	"fmt"
	"reflect"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

type client struct {
	events     map[EventType][]*methodType
	reqmutex   sync.Mutex // 保证流的正确性
	mutex      sync.Mutex // 保护client的状态
	codecFunc  CreateCodecFunc
	codec      Codec
	seq        uint64
	pending    map[uint64]*call
	connecting bool // client is connecting
	prepared   bool // server prepared
	shutdown   bool // server shutdown
}

type CreateCodecFunc func() (Codec, error)

func NewClient(cf CreateCodecFunc) *client {
	c := &client{
		events:    make(map[EventType][]*methodType),
		pending:   make(map[uint64]*call),
		codecFunc: cf,
		codec:     nil,
	}
	go c.keepAlive()
	go c.input()
	return c
}

func (this *client) keepAlive() {
	for {
		this.mutex.Lock()
		if !this.connecting {
			codec, err := this.codecFunc()
			if err != nil {
				logrus.Error(err)
				this.mutex.Unlock()
				time.Sleep(1 * time.Millisecond)
				continue
			} else {
				this.connecting = true
				this.codec = codec
				this.mutex.Unlock()
				continue
			}
		} else { //heart
			if err := this.EmitAsync(msgType_ping, ""); err != nil {
				logrus.Error(err)
			}
			time.Sleep(30 * time.Second)
		}

	}

}

func (this *client) input() {
	var err error
	for err == nil {
		var msg msg
		err = this.codec.Read(&msg)
		if err != nil {
			break
		}
		go this.call(&msg)
	}
	this.mutex.Lock()
	this.prepared = false
	this.mutex.Unlock()
}

func (this *client) call(body *msg) {

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

func (this *client) EmitAsync(t msgType, eventType EventType, args ...any) *call {
	m := &msg{
		T:         t,
		EventType: eventType,
		BodyCount: int8(len(args)),
	}
	var buf bytes.Buffer
	paramEncoder := gob.NewEncoder(&buf)
	for _, arg := range args {
		paramEncoder.Encode(arg)
	}
	m.Bytes = buf.Bytes()
	call := NewCall(m)
	this.send(call)
	return call
}

func (this *client) Emit(t msgType, eventType EventType, args ...any) error {
	call := <-this.EmitAsync(t, eventType, args...).Done
	return call.Error
}

func (this *client) send(call *call) {
	this.reqmutex.Lock()
	defer this.reqmutex.Unlock()

	this.mutex.Lock()
	if !this.prepared || this.shutdown {
		this.mutex.Unlock()
		call.Error = fmt.Errorf("client is prepared:%v,shutdown:%v", this.prepared, this.shutdown)
		call.done()
		return
	}
	seq := this.seq
	seq++
	this.pending[seq] = call
	this.seq = seq
	this.mutex.Unlock()
	call.msg.Seq = seq
	err := this.codec.Write(call.msg)
	if err != nil {
		this.mutex.Lock()
		call = this.pending[seq]
		delete(this.pending, seq)
		this.mutex.Unlock()
		if call != nil {
			call.Error = err
			call.done()
		}
	}
}

var defaultClient = NewClient(func() (Codec, error) {
	var buf bytes.Buffer
	codec := NewGobCodec(&buf)
	return codec, nil
})
