package event

import (
	"reflect"
)

type method struct {
	function reflect.Value
	argsType []*ArgType
	argCount int
}

type ArgType struct {
	isPointer bool
	at        reflect.Type
}
