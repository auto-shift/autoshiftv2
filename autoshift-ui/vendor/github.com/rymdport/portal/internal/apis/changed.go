package apis

import "github.com/godbus/dbus/v5"

// ListenOnSignal returns a channel that outputs a values each time the given signal is sent.
func ListenOnSignal(interfaceName, signalName string) (chan *dbus.Signal, error) {
	conn, err := dbus.SessionBus()
	if err != nil {
		return nil, err
	}

	if err := conn.AddMatchSignal(
		dbus.WithMatchObjectPath(ObjectPath),
		dbus.WithMatchInterface(interfaceName),
		dbus.WithMatchMember(signalName),
	); err != nil {
		return nil, err
	}

	signal := make(chan *dbus.Signal)
	conn.Signal(signal)
	return signal, nil
}
