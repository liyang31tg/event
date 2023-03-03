package event

import (
	"bufio"
	"bytes"
	"encoding/gob"
	"io"
)

type bodyType int

const (
	_             = 0
	bodyType_ping = 1 << (iota - 1)
	bodyType_pong
	bodyType_req //请求
	bodyType_res //响应
)

type body struct {
	EventType
	BodyCount int64
	Bytes     []byte
	Error     string // error
}

// 解码器
type Codec interface {
	Read(*body) error
	Write(EventType, ...any) error
	Close() error
}

type gobCodeC struct {
	conn io.ReadWriter
	//codec
	encbuf *bufio.Writer
	enc    *gob.Encoder
	dec    *gob.Decoder
}

func NewGobCodec(conn io.ReadWriter) *gobCodeC {
	buf := bufio.NewWriter(conn)
	return &gobCodeC{
		conn:   conn,
		encbuf: buf,
		enc:    gob.NewEncoder(buf),
		dec:    gob.NewDecoder(conn),
	}
}

func (this *gobCodeC) Read(b *body) error {
	return this.dec.Decode(b)
}

func (this *gobCodeC) Write(eventType EventType, args ...any) (err error) {
	b := &body{
		EventType: eventType,
		BodyCount: int64(len(args)),
	}
	var buf bytes.Buffer
	paramEncoder := gob.NewEncoder(&buf)
	for _, arg := range args {
		paramEncoder.Encode(arg)
	}
	b.Bytes = buf.Bytes()
	err = this.enc.Encode(b)
	if err != nil {
		return
	}
	return this.encbuf.Flush()
}

func (this *gobCodeC) Close() error {
	if v, ok := this.conn.(io.ReadWriteCloser); ok {
		return v.Close()
	} else if v, ok := this.conn.(*bytes.Buffer); ok {
		v.Reset()
	}
	return nil
}
