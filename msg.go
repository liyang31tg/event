package event

import (
	"fmt"

	"github.com/sirupsen/logrus"
)

type msgType int

const (
	msgType_invalid msgType = 0
	msgType_ping    msgType = 1 << (iota - 1) //心跳维护
	msgType_pong
	msgType_prepared //服务端已经准备好了
	msgType_on       //监听事件
	msgType_req      //请求
	msgType_res      //响应
)

var msgTypeString = map[msgType]string{
	msgType_invalid:  "invalid",
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

type Msg struct {
	T         msgType
	ServerSeq uint64 //服务器请求的序号 ,响应的时候用
	LocalSeq  uint64 //本地请求序号
	EventType
	BodyCount int8 // 超过这个数就是自讨苦吃
	Bytes     []byte
	Error     string // error
}

type call struct {
	msg   *Msg
	Done  chan *call
	Error error
}

func NewCall(m *Msg) *call {
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
