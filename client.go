package event

import (
	"bytes"
	"encoding/gob"
	"errors"
	"fmt"
	"io"
	"net"
	"reflect"
	"sync"
	"time"

	"github.com/liyang31tg/event/codec"
	"github.com/liyang31tg/event/msg"
	"github.com/liyang31tg/event/options"
	"github.com/sirupsen/logrus"
)

type ServerError string

func (this ServerError) Error() string {
	return string(this)
}

// 出现这个错的话就要尝试重新建立连接
var errLocalWrite = errors.New("local Write err")

type client struct {
	name          string
	url           string
	reqmutex      sync.Mutex // 保证流的正确性
	mutex         sync.Mutex // 保护client的状态
	codecFunc     options.CreateClientCodecFunc
	codec         codec.Codec
	events        map[msg.EventType][]*method
	seq           uint64
	pending       map[uint64]*msg.Call
	checkInterval time.Duration //链接检测
	heartInterval time.Duration //心跳间隔
	isStopHeart   bool          //是否关闭心跳
	connecting    bool          // client is connecting
}

func Dial(url string, opts ...options.ClientOptions) (*client, error) {
	c := &client{
		url:           url,
		events:        make(map[msg.EventType][]*method),
		pending:       make(map[uint64]*msg.Call),
		connecting:    true,
		checkInterval: 1,
		heartInterval: 5,
	}
	conn, err := net.Dial("tcp", url)
	if err != nil {
		return nil, err
	}
	//合并属性
	opt := options.Client().SetCodecFunc(func(conn io.ReadWriteCloser) (codec.Codec, error) {
		return codec.NewGobCodec(conn), nil
	})

	//属性设置开始
	if opt.Name != nil {
		c.name = *opt.Name
	}
	var codec codec.Codec
	if opt.CodecFunc != nil {
		c.codecFunc = *opt.CodecFunc
		codec, err = c.codecFunc(conn)
		if err != nil {
			return nil, err
		}
	}

	if opt.CheckInterval != nil {
		c.checkInterval = *opt.CheckInterval
	}

	if opt.HeartInterval != nil {
		c.heartInterval = *opt.HeartInterval
	}

	if opt.IsStopHeart != nil {
		c.isStopHeart = *opt.IsStopHeart
	}
	//属性设置结束
	if err := c.serve(codec); err != nil {
		codec.Close()
		return nil, err
	}
	go c.keepAlive()
	return c, nil

}

func (this *client) keepAlive() {
	for {
		this.mutex.Lock()
		if !this.connecting {
			this.mutex.Unlock()
			conn, err := net.Dial("tcp", this.url)
			if err != nil {
				logrus.Errorf("dail err:%v\n", err)
				time.Sleep(this.checkInterval * time.Second)
				continue
			}
			codec, err := this.codecFunc(conn)
			if err != nil {
				logrus.Errorf("codec err:%v\n", err)
				time.Sleep(this.checkInterval * time.Second)
				continue
			} else {
				if err := this.serve(codec); err != nil {
					logrus.Error(err)
				}
				continue
			}
		} else { //heart
			this.mutex.Unlock()
			if !this.isStopHeart {
				if call := this.emit_async(msg.MsgType_ping, ""); call != nil {
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

func (this *client) serve(codec codec.Codec) (err error) {
	this.mutex.Lock()
	defer func() {
		if err != nil {
			this.mutex.Unlock()
		}
	}()
	if err = codec.Write(&msg.Msg{T: msg.MsgType_varify, Name: this.name}); err != nil {
		return
	}
	var readFirstMsg msg.Msg
	if err = codec.Read(&readFirstMsg); err != nil {
		return
	}
	if readFirstMsg.T == msg.MsgType_prepared {
		//重连挂载已经有的event
		for event := range this.events {
			if err = codec.Write(&msg.Msg{T: msg.MsgType_on, EventType: event}); err != nil {
				return
			}
		}
	}
	this.connecting = true
	this.codec = codec
	this.mutex.Unlock()
	go this.input(codec)
	return
}

func (this *client) stop() {
	logrus.Error("stop")
	if this.connecting {
		this.codec.Close()
		this.codec = nil
	}
	this.seq = 0
	this.pending = make(map[uint64]*msg.Call)
	this.connecting = false
}

func (this *client) StopHeart() {
	this.mutex.Lock()
	defer this.mutex.Unlock()
	this.isStopHeart = true

}

func (this *client) printCall() {
	for index, msg := range this.pending {
		logrus.Info("index:%d,msg:%+v\n", index, *msg)
	}
}

func (this *client) input(codec codec.Codec) {
	var err error
	for err == nil {
		var gotMsg msg.Msg
		err = codec.Read(&gotMsg)
		if err != nil {
			err = errors.New("reading error body1: " + err.Error())
			break
		}
		logrus.Infof("client receive:%+v", gotMsg)
		switch gotMsg.T {
		case msg.MsgType_ping, msg.MsgType_pong:
		case msg.MsgType_req:
			go this.call(codec, &gotMsg)
		case msg.MsgType_res, msg.MsgType_on:
			seq := gotMsg.LocalSeq
			this.mutex.Lock()
			call := this.pending[seq]
			delete(this.pending, seq)
			this.mutex.Unlock()
			if call != nil {
				if gotMsg.Error != "" {
					call.Error = ServerError(gotMsg.Error)
				}
				call.Do()
			}
		default:
		}
	}
	this.reqmutex.Lock()
	this.mutex.Lock()
	for _, call := range this.pending {
		logrus.Infof("%+v", *call.Msg)
		call.Error = err
		call.Do()
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
		at := argType.at
		if argType.isPointer {
			at = at.Elem()
		}
		argValue := reflect.New(at)
		if i < argCount {
			if err := dec.Decode(argValue.Interface()); err != nil {
				logrus.Error(err)
			}
		}
		if !argType.isPointer {
			argValue = argValue.Elem()
		}
		argsValue[i] = argValue
	}
	return argsValue
}

func (this *client) call(codec codec.Codec, body *msg.Msg) {
	res := &msg.Msg{
		T:         msg.MsgType_res,
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
					err = fmt.Errorf("%w,%w", err, appendErr)
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
func (this *client) On(t msg.EventType, Func any) error {
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
	var argsType = make([]*argType, 0, inCount)
	for i := 0; i < inCount; i++ {
		at := rt.In(i)
		argsType = append(argsType, &argType{isPointer: at.Kind() == reflect.Pointer, at: at})
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
	return this.emit(msg.MsgType_on, t)
}

func (this *client) EmitAsync(t msg.MsgType, eventType msg.EventType, args ...any) (call *msg.Call) {
	return this.emit_async(msg.MsgType_req, eventType, args...)
}

func (this *client) emit_async(t msg.MsgType, eventType msg.EventType, args ...any) (call *msg.Call) {
	m := &msg.Msg{
		T:         t,
		EventType: eventType,
		BodyCount: int8(len(args)),
	}
	call = msg.NewCall(m)
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
		call.Do()
		return
	}
	m.Bytes = buf.Bytes()
	fmt.Printf("emit_async:%+v\n", m)
	this.send(call)
	return
}

func (this *client) emit(t msg.MsgType, eventType msg.EventType, args ...any) error {
	logrus.Info(t, eventType, args)
	call := <-this.emit_async(t, eventType, args...).Done
	return call.Error
}

func (this *client) Emit(eventType msg.EventType, args ...any) error {
	return this.emit(msg.MsgType_req, eventType, args...)
}

func (this *client) send(call *msg.Call) {
	this.reqmutex.Lock()
	defer this.reqmutex.Unlock()
	var codec codec.Codec
	this.mutex.Lock()
	if !this.connecting {
		this.mutex.Unlock()
		call.Error = fmt.Errorf("client is connecting:%v", this.connecting)
		call.Do()
		return
	}
	codec = this.codec
	seq := this.seq
	seq = incSeqID(seq)
	this.pending[seq] = call
	this.seq = seq
	this.mutex.Unlock()
	call.Msg.LocalSeq = seq
	err := codec.Write(call.Msg)
	if err != nil {
		this.mutex.Lock()
		call = this.pending[seq]
		delete(this.pending, seq)
		this.mutex.Unlock()
		if call != nil {
			err = fmt.Errorf("current:%v,err:%w", err, errLocalWrite)
			call.Error = err
			call.Do()
		}
	}
}
