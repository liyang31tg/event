package event

// 解码器
type Codec interface {
	Read(*Msg) error
	Write(*Msg) error
	Close() error
}
