package options

import (
	"io"
	"time"

	"github.com/liyang31tg/event/codec"
)

type CreateClientCodecFunc func(conn io.ReadWriteCloser) (codec.Codec, error)
type ClientOptions struct {
	codecFunc     *CreateClientCodecFunc
	codec         *codec.Codec
	checkInterval *time.Duration
	heartInterval *time.Duration
	isStopHeart   *bool
	connecting    *bool
}

func (this *ClientOptions) SetCodecFunc(cf CreateClientCodecFunc) *ClientOptions {
	if this == nil {
		return this
	}
	this.codecFunc = &cf
	return this
}

type CreateServerCodecFunc func(conn io.ReadWriteCloser) (codec.Codec, error)
