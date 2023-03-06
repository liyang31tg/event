package event

// 解码器
type Codec interface {
	Read(*msg) error
	Write(*msg) error
	Close() error
}
