package settings

import (
	"github.com/godbus/dbus/v5"
	"github.com/rymdport/portal/internal/apis"
)

const (
	readCallPath    = interfaceName + ".Read"
	readOneCallPath = interfaceName + ".ReadOne"
	readAllCallPath = interfaceName + ".ReadAll"
)

// ReadAll returns all values for the corresponding namespaces passed.
// If namespaces is an empty array or contains an empty string it matches all.
// Globbing is supported but only for trailing sections, e.g. “org.example.*”.
func ReadAll(namespaces []string) (map[string](map[string]dbus.Variant), error) {
	result, err := apis.Call(readAllCallPath, namespaces)
	if err != nil {
		return nil, err
	}

	return result.(map[string](map[string]dbus.Variant)), nil
}

// ReadOne reads a single value which may be any valid DBus type. Returns an error on any unknown namespace or key.
func ReadOne(namespace, key string) (any, error) {
	value, err := apis.Call(readOneCallPath, namespace, key)
	if err != nil {
		return apis.Call(readCallPath, namespace, key) // Use deprecated fallback if new interface does not exist.
	}

	return value, err
}
