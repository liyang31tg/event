package event

import (
	"bufio"
	"bytes"
	"encoding/gob"
	"fmt"
	"io"
)

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

func (this *gobCodeC) Read(b *msg) error {
	return this.dec.Decode(b)
}

func (this *gobCodeC) Write(m *msg) (err error) {
	err = this.enc.Encode(m)
	if err != nil {
		err = fmt.Errorf("msg type:%v,eventType:%s,err:%w", m.T, m.EventType, err)
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
