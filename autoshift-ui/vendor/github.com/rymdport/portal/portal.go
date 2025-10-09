package portal

import (
	"errors"
	"strconv"
)

// ErrunexpectedResonse is returned when the received dbus data was in an unexpected format.
var ErrUnexpectedResponse = errors.New("unexpected response from dbus")

// FormatX11WindowHandle takes an X11 window handle and formats it
// in the string format that is required for the portal APIs.
func FormatX11WindowHandle(handle uintptr) string {
	return "x11:" + strconv.FormatUint(uint64(handle), 16)
}
