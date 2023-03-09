package msg

import (
	"regexp"
	"strings"
)

// HasPrefix("//") 是一个正在表达式
type EventType string

func (this EventType) isRegexp() bool {
	return strings.HasPrefix(string(this), "//")
}

// 引入这个概念是为了推广正则,有了正则就不用拓展组
type EventTopic struct {
	ET       EventType
	reg      *regexp.Regexp
	isRegexp bool
}

func NewEventTopic(et EventType) *EventTopic {
	s := &EventTopic{
		ET: et,
	}
	if et.isRegexp() {
		s.reg = regexp.MustCompile(string(et)[2:])
		s.isRegexp = true
	}
	return nil
}

func (this *EventTopic) Match(et EventType) bool {
	if this.isRegexp {
		return this.reg.MatchString(string(et))
	} else {
		return this.ET == et
	}
}
