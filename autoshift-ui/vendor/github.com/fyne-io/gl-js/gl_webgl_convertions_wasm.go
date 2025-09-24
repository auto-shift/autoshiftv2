//go:build js && wasm

package gl

import (
	"fmt"
	"runtime"
	"syscall/js"
	"unsafe"
)

func sliceToBytes[T comparable](s []T) []byte {
	size := 0
	if len(s) > 0 {
		size = int(unsafe.Sizeof(s[0]))
	}
	return unsafe.Slice((*byte)(unsafe.Pointer(&s[0])), len(s)*size)
}

func sliceToJSArray[T comparable](arrayType string, s []T) js.Value {
	a := js.Global().Get(arrayType).New(len(s))
	js.CopyBytesToJS(a, sliceToBytes(s))
	runtime.KeepAlive(s)
	return a
}

func int8ToJSArray(s []int8) js.Value {
	return sliceToJSArray("Int8Array", s)
}

func int16ToJSArray(s []int16) js.Value {
	return sliceToJSArray("Int16Array", s)
}

func int32ToJSArray(s []int32) js.Value {
	return sliceToJSArray("Int32Array", s)
}

func int64ToJSArray(s []int64) js.Value {
	return sliceToJSArray("Int64Array", s)
}

func uint8ToJSArray(s []uint8) js.Value {
	return sliceToJSArray("Uint8Array", s)
}

func uint16ToJSArray(s []uint16) js.Value {
	return sliceToJSArray("Uint16Array", s)
}

func uint32ToJSArray(s []uint32) js.Value {
	return sliceToJSArray("Uint32Array", s)
}

func uint64ToJSArray(s []uint64) js.Value {
	return sliceToJSArray("Uint64Array", s)
}

func float32ToJSArray(s []float32) js.Value {
	return sliceToJSArray("Float32Array", s)
}

func float64ToJSArray(s []float64) js.Value {
	return sliceToJSArray("Float64Array", s)
}

func SliceToTypedArray(s any) js.Value {
	if s == nil {
		return js.Null()
	}

	switch s := s.(type) {
	case []int8:
		return int8ToJSArray(s)
	case []int16:
		return int16ToJSArray(s)
	case []int32:
		return int32ToJSArray(s)
	case []int64:
		return int64ToJSArray(s)
	case []uint8:
		return uint8ToJSArray(s)
	case []uint16:
		return uint16ToJSArray(s)
	case []uint32:
		return uint32ToJSArray(s)
	case []uint64:
		return uint64ToJSArray(s)
	case []float32:
		return float32ToJSArray(s)
	case []float64:
		return float64ToJSArray(s)
	default:
		panic(fmt.Sprintf("jsutil: unexpected value at SliceToTypedArray: %T", s))
	}
}
