//go:build js && wasm

package safejs

import "syscall/js"

// Global returns the JavaScript global object, usually "window" or "global".
func Global() Value {
	return Safe(js.Global())
}
