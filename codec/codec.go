package codec

import "github.com/liyang31tg/event/msg"

// 解码器
type Codec interface {
	Read(*msg.Msg) error
	Write(*msg.Msg) error
	Close() error
}
