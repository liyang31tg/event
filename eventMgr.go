package event

import (
	"errors"
	"io"
	"net"
	"sync"

	"github.com/sirupsen/logrus"
)

type server struct {
	codecFunc CreateServerCodecFunc
	mutex     sync.Mutex
	seq       uint64
	on        map[string]map[int]struct{}
	services  map[uint64]*service
}

type CreateServerCodecFunc func(conn io.ReadWriteCloser) (Codec, error)

func NewServer(cf CreateServerCodecFunc) *server {
	c := &server{
		codecFunc: cf,
		services:  map[uint64]*service{},
		on:        map[string]map[int]struct{}{},
	}
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

func (this *server) remove(id uint64) {
	this.mutex.Lock()
	defer this.mutex.Unlock()
	delete(this.services, id)
}

// 分发
// case msgType_on, msgType_req, msgType_res:
func (this *server) handle(serviceID uint64, msg *Msg) {

}

//-----------------------------service----------------------------

type service struct {
	id     uint64
	done   chan struct{}
	server *server
	codec  Codec
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
	this.read()
}

func (this *service) read() {
	if this == nil {
		return
	}
	this.write(&Msg{T: msgType_prepared})
	var err error
	for err == nil {
		select {
		case <-this.done:
			err = errors.New("stop service")
		default:
			var msg Msg
			err = this.codec.Read(&msg)
			if err != nil {
				continue
			}
			switch msg.T {
			case msgType_ping:
				this.write(&Msg{T: msgType_pong})
			case msgType_on, msgType_req, msgType_res:
				this.server.handle(this.id, &msg)
			default:
				logrus.Infof("invalid msg:%+v", msg)
			}
		}
	}
	this.server.remove(this.id)
	this.close()
}

func (this *service) close() error {
	return this.codec.Close()
}

func (this *service) write(msg *Msg) error {
	err := this.codec.Write(msg)
	if err != nil {
		logrus.Error(err)
		close(this.done)
	}
	return err
}
