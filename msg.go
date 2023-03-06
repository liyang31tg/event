package event

import (
	"fmt"

	"github.com/sirupsen/logrus"
)

type msgType int

const (
	_                    = 0
	msgType_ping msgType = 1 << (iota - 1) //心跳维护
	msgType_pong
	msgType_prepared //服务端已经准备好了
	msgType_on       //监听事件
	msgType_req      //请求
	msgType_res      //响应
)

var msgTypeString = map[msgType]string{
	msgType_ping:     "Ping",
	msgType_pong:     "Pong",
	msgType_prepared: "Prepared",
	msgType_on:       "on",
	msgType_req:      "req",
	msgType_res:      "res",
}

func (this msgType) String() string {
	if s, ok := msgTypeString[this]; ok {
		return s
	} else {
		return fmt.Sprintf("未知:(%d)", this)
	}
}

type msg struct {
	T   msgType
	Seq uint64
	EventType
	BodyCount int8 // 超过这个数就是自讨苦吃
	Bytes     []byte
	Error     string // error
}

type call struct {
	msg   *msg
	Done  chan *call
	Error error
}

func NewCall(m *msg) *call {
	if m == nil {
		return nil
	}
	return &call{
		msg:  m,
		Done: make(chan *call, 1),
	}
}

func (this *call) done() {
	if this == nil {
		return
	}
	select {
	case this.Done <- this:
	default:
		logrus.Errorf("not handle here :%+v", this.msg)
	}
}
