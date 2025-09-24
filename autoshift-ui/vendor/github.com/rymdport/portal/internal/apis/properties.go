package apis

import "github.com/godbus/dbus/v5"

// GetProperty reads the value of the property at the interface specified.
func GetProperty(interfaceName, property string) (any, error) {
	conn, err := dbus.SessionBus() // Shared connection, don't close.
	if err != nil {
		return nil, err
	}

	obj := conn.Object(ObjectName, ObjectPath)
	call := obj.Call(PropertiesGetName, 0, interfaceName, property)
	if call.Err != nil {
		return nil, call.Err
	}

	var value any
	err = call.Store(&value)
	return value, err
}
