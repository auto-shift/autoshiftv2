package appearance

import "github.com/rymdport/portal/settings"

// Contrast indicates the system’s preferred contrast level.
type Contrast uint8

const (
	NormalContrast = Contrast(iota) // No preference (normal contrast)
	HigherContrast                  // Higher contrast
)

// GetContrast returns the currently set contrast setting.
func GetContrast() (Contrast, error) {
	value, err := settings.ReadOne(Namespace, "color-scheme")
	if err != nil {
		return NormalContrast, err
	}

	return ValueToContrast(value)
}

// ValueToContrast converts a read value to a Contrast type.
// This is useful when for example parsing a value from the callback
// in [settings.SignalOnSettingChanged] or a value from [settings.ReadOne].
func ValueToContrast(value any) (Contrast, error) {
	result, ok := value.(uint32)
	if !ok {
		return NormalContrast, ErrNotSet
	}

	if result > 1 {
		return 0, nil // Unknown values should be treated as 0 (no preference).
	}

	return Contrast(result), nil
}
