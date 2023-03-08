package event

import (
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

type server struct {
	codecFunc CreateServerCodecFunc
	mutex     sync.Mutex
	seq       uint64
	monitor   map[EventType]map[uint64]struct{}
	services  map[uint64]*service

	reqTimeOut time.Duration //请求超时
	reqSeq     uint64
	reqMetas   map[uint64]*serverReqMeta
}

type serverReqMeta struct {
	senderID uint64
	reqSeq   uint64
	reqCount uint64
	existErr bool
	errs     []string
	Time     time.Time
	//clien info
	localSeq uint64
	EventType
}

type CreateServerCodecFunc func(conn io.ReadWriteCloser) (Codec, error)

func NewServer(cf CreateServerCodecFunc) *server {
	c := &server{
		codecFunc:  cf,
		services:   map[uint64]*service{},
		monitor:    map[EventType]map[uint64]struct{}{},
		reqTimeOut: 10,
		reqMetas:   map[uint64]*serverReqMeta{},
	}
	go c.checkTimeOut()
	return c
}

//url:port
func (this *server) listen(url string) {
	listen, err := net.Listen("tcp", url)
	if err != nil {
		panic(err)
	}
	for {
		conn, err := listen.Accept()
		if err != nil {
			continue
		}
		codec, err := this.codecFunc(conn)
		if err != nil {
			logrus.Error(err)
			continue
		}
		this.mutex.Lock()
		seq := this.seq
		seq++
		this.seq = seq
		service := newService(this, seq, codec)
		this.services[this.seq] = service
		this.mutex.Unlock()
		go service.serve()
	}
}

var errTimeout = errors.New("req timeout")

func (this *server) checkTimeOut() {
	for {
		this.mutex.Lock()
		for _, reqMeta := range this.reqMetas {
			if time.Now().Sub(reqMeta.Time) > this.reqTimeOut*time.Second { //
				msg := &Msg{
					T:         msgType_res,
					ServerSeq: reqMeta.reqSeq,
					LocalSeq:  reqMeta.localSeq,
					EventType: reqMeta.EventType,
					Error:     errTimeout.Error(),
				}
				this.writeWithoutLock(reqMeta.senderID, msg)
			}
		}
		this.mutex.Unlock()
		time.Sleep(2 * time.Second)
	}
}

func (this *server) close(id uint64) {
	this.mutex.Lock()
	defer this.mutex.Unlock()
	if service, ok := this.services[id]; ok {
		delete(this.services, id)
		service.close()
	}
}

// case msgType_on, msgType_req, msgType_res:
func (this *server) handle(serviceID uint64, msg *Msg) {
	switch msg.T {
	case msgType_on:
		this.on(serviceID, msg)
	case msgType_req:
		this.req(serviceID, msg)
	case msgType_res:
		this.res(serviceID, msg)
	default:
		logrus.Infof("丢弃：%d,msg:%+v\n", serviceID, msg)
	}

}

func (this *server) on(serviceID uint64, msg *Msg) {
	et := msg.EventType
	if et == "" {
		return
	}
	this.mutex.Lock()
	defer this.mutex.Unlock()
	if v, ok := this.monitor[et]; ok {
		v[serviceID] = struct{}{}
	} else {
		v = map[uint64]struct{}{serviceID: {}}
		this.monitor[et] = v
	}
}

func (this *server) req(serviceID uint64, msg *Msg) {
	et := msg.EventType
	if et == "" {
		return
	}
	this.mutex.Lock()
	reqSeq := IncSeqID(this.reqSeq)
	this.reqSeq = reqSeq
	msg.ServerSeq = reqSeq
	if v, ok := this.monitor[et]; ok {
		this.mutex.Unlock()
		var isDone bool
		var reqCount uint64
		for sid := range v {
			if err := this.write(sid, msg); err == nil {
				isDone = true
				reqCount++
			}
		}
		if isDone {
			this.mutex.Lock()
			this.reqMetas[reqSeq] = &serverReqMeta{
				senderID: serviceID,
				reqSeq:   reqSeq,
				reqCount: reqCount,
				Time:     time.Now(),
			}
			this.mutex.Unlock()
		}
	} else {
		this.mutex.Unlock()
	}
}
func (this *server) res(serviceID uint64, msg *Msg) {
	reqSeq := msg.ServerSeq
	this.mutex.Lock()
	reqMeta, ok := this.reqMetas[reqSeq]
	if ok {
		reqMeta.reqCount--
		leftCount := reqMeta.reqCount
		if msg.Error != "" {
			reqMeta.existErr = true
		}
		reqMeta.errs = append(reqMeta.errs, msg.Error)
		this.mutex.Unlock()
		if leftCount == 0 { //res
			if reqMeta.existErr {
				msg.Error = strings.Join(reqMeta.errs, "|")
			}
			if err := this.write(reqMeta.senderID, msg); err != nil {
				logrus.Error("res senderID:%d,msg:%+v", reqMeta.senderID, msg)
			}
		}
	} else {
		this.mutex.Unlock()
	}
}

func (this *server) write(serviceID uint64, msg *Msg) (err error) {
	this.mutex.Lock()
	if service, ok := this.services[serviceID]; ok {
		this.mutex.Unlock()
		if err = service.write(msg); err == nil {
			return
		} else {
			this.close(serviceID)
		}
	} else {
		this.mutex.Unlock()
	}
	return
}

func (this *server) writeWithoutLock(serviceID uint64, msg *Msg) (err error) {
	if service, ok := this.services[serviceID]; ok {
		if err = service.write(msg); err == nil {
			return
		} else {
			this.close(serviceID)
		}
	}
	return
}

//-----------------------------service----------------------------

type service struct {
	id     uint64
	done   chan struct{}
	server *server
	codec  Codec
	mutex  sync.Mutex //读是单线程，写加锁
}

func newService(server *server, id uint64, codec Codec) *service {
	s := &service{
		id:     id,
		server: server,
		codec:  codec,
	}
	return s
}

func (this *service) serve() {
	if this == nil {
		return
	}
	var err error
	err = this.write(&Msg{T: msgType_prepared})
	for err == nil {
		select {
		case <-this.done:
			err = errors.New("stop service")
		default:
			var msg Msg
			err = this.read(&msg)
			if err != nil {
				continue
			}
			switch msg.T {
			case msgType_ping:
				err = this.write(&Msg{T: msgType_pong})
			case msgType_on, msgType_req, msgType_res:
				go this.server.handle(this.id, &msg)
			default:
				logrus.Infof("invalid msg:%+v", msg)
			}
		}
	}
	this.server.close(this.id)
	logrus.Errorf("service id:%d is die\n", this.id)
}

func (this *service) close() error {
	return this.codec.Close()
}

func (this *service) read(msg *Msg) error {
	return this.codec.Read(msg)
}

func (this *service) write(msg *Msg) error {
	this.mutex.Lock()
	defer this.mutex.Unlock()
	err := this.codec.Write(msg)
	if err != nil {
		logrus.Error(err)
		close(this.done)
	}
	return err
}