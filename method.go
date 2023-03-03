package event

import (
	"reflect"
)

type methodType struct {
	method   reflect.Value
	ArgType  []reflect.Type
	ArgCount int
}
