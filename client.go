package event

import (
	"bytes"
	"encoding/gob"
	"errors"
	"fmt"
	"io"
	"reflect"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

type ServerError string

func (this ServerError) Error() string {
	return string(this)
}

// 出现这个错的话就要尝试重新建立连接
var errLocalWrite = errors.New("local Write err")

type client struct {
	reqmutex      sync.Mutex // 保证流的正确性
	mutex         sync.Mutex // 保护client的状态
	codecFunc     CreateCodecFunc
	codec         Codec
	events        map[EventType][]*method
	seq           uint64
	pending       map[uint64]*call
	checkInterval time.Duration //链接检测
	heartInterval time.Duration //心跳间隔
	isStopHeart   bool          //是否关闭心跳
	connecting    bool          // client is connecting
}

type CreateCodecFunc func() (Codec, error)

func NewClient(cf CreateCodecFunc) *client {
	c := &client{
		events:        make(map[EventType][]*method),
		pending:       make(map[uint64]*call),
		codecFunc:     cf,
		codec:         nil,
		checkInterval: 1,
		heartInterval: 5,
	}
	go c.keepAlive()
	return c
}

func (this *client) keepAlive() {
	for {
		this.mutex.Lock()
		if !this.connecting {
			this.mutex.Unlock()
			codec, err := this.codecFunc()
			if err != nil {
				logrus.Error(err)
				time.Sleep(this.checkInterval * time.Second)
				continue
			} else {
				this.serve(codec)
				continue
			}
		} else { //heart
			this.mutex.Unlock()
			if !this.isStopHeart {
				if call := this.emit_async(msgType_ping, ""); call != nil {
					if call.Error != nil { //这里是同步触发的错误
						logrus.Error(call.Error)
						if errors.Is(call.Error, io.ErrShortWrite) || errors.Is(call.Error, errLocalWrite) {
							this.mutex.Lock()
							this.stop()
							this.mutex.Unlock()
						}
					}
				}
				time.Sleep(this.heartInterval * time.Second)
			} else {
				time.Sleep(this.checkInterval * time.Second) //下次去尝试连接
			}
		}
	}
}

func (this *client) serve(codec Codec) {
	this.mutex.Lock()
	logrus.Info("hasconnection")
	this.connecting = true
	this.codec = codec
	this.mutex.Unlock()
	go this.input(codec)
}

func (this *client) stop() {
	logrus.Error("stop")
	if this.connecting {
		this.codec.Close()
		this.codec = nil
	}
	this.seq = 0
	this.pending = make(map[uint64]*call)
	this.connecting = false
}

func (this *client) stopHeart() {
	this.mutex.Lock()
	defer this.mutex.Unlock()
	this.isStopHeart = false

}

func (this *client) printCall() {
	for index, msg := range this.pending {
		logrus.Info("index:%d,msg:%+v\n", index, *msg)
	}
}

func (this *client) input(codec Codec) {
	var err error
	for err == nil {
		var msg Msg
		err = codec.Read(&msg)
		if err != nil {
			err = errors.New("reading error body1: " + err.Error())
			break
		}
		switch msg.T {
		case msgType_ping, msgType_pong:
		case msgType_req:
			go this.call(codec, &msg)
		case msgType_res:
			seq := msg.LocalSeq
			this.mutex.Lock()
			call := this.pending[seq]
			delete(this.pending, seq)
			this.mutex.Unlock()
			if call != nil {
				if msg.Error != "" {
					call.Error = ServerError(msg.Error)
				}
				call.done()
			}
		default:
		}
	}
	this.reqmutex.Lock()
	this.mutex.Lock()
	for _, call := range this.pending {
		logrus.Infof("%+v", *call.msg)
		call.Error = err
		call.done()
	}
	if err != nil {
		logrus.Error(err)
	}
	this.stop()
	this.mutex.Unlock()
	this.reqmutex.Unlock()
}

func (this *client) parse(data []byte, argCount int, m *method) []reflect.Value {
	argsValue := make([]reflect.Value, m.argCount)
	var dstData = make([]byte, len(data))
	copy(dstData, data)
	dec := gob.NewDecoder(bytes.NewReader(dstData))
	for i := 0; i < m.argCount; i++ {
		argType := m.argsType[i]
		argValue := reflect.New(argType.at)
		if i < argCount {
			if err := dec.Decode(argValue.Interface()); err != nil {
				logrus.Error(err)
			}
		}
		if argType.isPointer {
			argValue = argValue.Elem()
		}
		argsValue[i] = argValue
	}
	return argsValue
}

func (this *client) call(codec Codec, body *Msg) {
	res := &Msg{
		T:         msgType_res,
		ServerSeq: body.ServerSeq,
		LocalSeq:  body.LocalSeq,
		EventType: body.EventType,
	}
	var err error
	if events, ok := this.events[body.EventType]; ok {
		for _, method := range events {
			args := this.parse(body.Bytes, int(body.BodyCount), method)
			returnValues := method.function.Call(args)
			errInter := returnValues[0].Interface()
			if errInter != nil {
				appendErr := errInter.(error)
				if err != nil {
					err = fmt.Errorf("%w|%w", err, appendErr)
				} else {
					err = appendErr
				}
			}
		}

	} else {
		err = fmt.Errorf("has no eventType:%v", body.EventType)
	}
	if err != nil {
		res.Error = err.Error()
	}
	this.reqmutex.Lock()
	defer this.reqmutex.Unlock()
	err = codec.Write(res)
	if err != nil {
		logrus.Error(err)
		this.mutex.Lock()
		defer this.mutex.Unlock()
		this.stop()
	}
}

var errType = reflect.TypeOf((*error)(nil)).Elem()

// 监听事件
func (this *client) On(t EventType, Func any) error {
	if t == "" {
		return errors.New("event type must not empty")
	}
	rt := reflect.TypeOf(Func)
	if rt.Kind() != reflect.Func {
		panic("on a not func")
	}
	if rt.NumOut() != 1 {
		return errors.New("must has 1 return value")
	}
	if rt.Out(0) != errType {
		return errors.New("return param must error")
	}
	inCount := rt.NumIn()
	var argsType = make([]*ArgType, 0, inCount)
	for i := 0; i < inCount; i++ {
		at := rt.In(i)
		argsType = append(argsType, &ArgType{isPointer: at.Kind() == reflect.Pointer, at: at})
	}

	rv := reflect.ValueOf(Func)
	mType := &method{
		function: rv,
		argsType: argsType,
		argCount: inCount,
	}
	this.mutex.Lock()
	events := this.events[t]
	events = append(events, mType)
	this.events[t] = events
	this.mutex.Unlock()
	return nil
}

func (this *client) EmitAsync(t msgType, eventType EventType, args ...any) (call *call) {
	return this.emit_async(msgType_req, eventType, args...)
}

func (this *client) emit_async(t msgType, eventType EventType, args ...any) (call *call) {
	m := &Msg{
		T:         t,
		EventType: eventType,
		BodyCount: int8(len(args)),
	}
	call = NewCall(m)
	var buf bytes.Buffer
	paramEncoder := gob.NewEncoder(&buf)
	var err error
	for _, arg := range args {
		if err = paramEncoder.Encode(arg); err != nil {
			err = fmt.Errorf("current:%v,err:%w", err, errLocalWrite)
			break
		}
	}
	if err != nil {
		call.Error = err
		call.done()
		return
	}
	m.Bytes = buf.Bytes()
	this.send(call)
	return
}

func (this *client) emit(t msgType, eventType EventType, args ...any) error {
	call := <-this.emit_async(t, eventType, args...).Done
	return call.Error
}

func (this *client) Emit(eventType EventType, args ...any) error {
	return this.emit(msgType_req, eventType, args...)
}

func (this *client) send(call *call) {
	this.reqmutex.Lock()
	defer this.reqmutex.Unlock()
	var codec Codec
	this.mutex.Lock()
	if !this.connecting {
		this.mutex.Unlock()
		call.Error = fmt.Errorf("client is connecting:%v", this.connecting)
		call.done()
		return
	}
	codec = this.codec
	seq := this.seq
	seq = IncSeqID(seq)
	this.pending[seq] = call
	this.seq = seq
	this.mutex.Unlock()
	call.msg.LocalSeq = seq
	err := codec.Write(call.msg)
	if err != nil {
		this.mutex.Lock()
		call = this.pending[seq]
		delete(this.pending, seq)
		this.mutex.Unlock()
		if call != nil {
			err = fmt.Errorf("current:%v,err:%w", err, errLocalWrite)
			call.Error = err
			call.done()
		}
	}
}
