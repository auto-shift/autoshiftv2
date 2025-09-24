package apis

import "github.com/godbus/dbus/v5"

// CallWithoutResult works like [Call] but does not read a result.
func CallWithoutResult(callName string, args ...any) error {
	_, err := call(callName, args...)
	return err
}

// Call calls the given call name for a portal using passed arguments and returns the output.
func Call(callName string, args ...any) (any, error) {
	call, err := call(callName, args...)
	if err != nil {
		return nil, err
	}

	var result any
	err = call.Store(&result)
	return result, err
}

// CallOnObject calls the specified callName on the given object.
func CallOnObject(path dbus.ObjectPath, callName string, args ...any) error {
	conn, err := dbus.SessionBus()
	if err != nil {
		return err
	}

	obj := conn.Object(ObjectName, path)
	call := obj.Call(callName, 0, args...)
	return call.Err
}

func call(callName string, args ...any) (*dbus.Call, error) {
	conn, err := dbus.SessionBus() // Shared connection, don't close.
	if err != nil {
		return nil, err
	}

	obj := conn.Object(ObjectName, ObjectPath)
	call := obj.Call(callName, 0, args...)
	return call, call.Err
}
