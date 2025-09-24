package openuri

import (
	"github.com/godbus/dbus/v5"
	"github.com/rymdport/portal/internal/apis"
	"github.com/rymdport/portal/internal/convert"
)

const openFileCallName = interfaceName + ".OpenFile"

// OpenFileOptions holds optional settings that can be passed to the OpenFile call.
type OpenFileOptions struct {
	HandleToken string // A string that will be used as the last element of the handle. Must be a valid object path element.
	Writable    bool   // Whether to allow the chosen application to write to the file. This key only takes effect the uri points to a local file that is exported in the document portal, and the chosen application is sandboxed itself.
	Ask         bool   // Whether to ask the user to choose an app. If this is not passed, or false, the portal may use a default or pick the last choice.
}

// OpenFile asks to open a local file.
// The input parameter fd should be a file descriptor like the one given from [*os.File.Fd] for example.
func OpenFile(parentWindow string, fd uintptr, options *OpenFileOptions) error {
	data := map[string]dbus.Variant{}

	if options != nil {
		data = map[string]dbus.Variant{
			"writable": convert.FromBool(options.Writable),
			"ask":      convert.FromBool(options.Ask),
		}

		if options.HandleToken != "" {
			data["handle_token"] = convert.FromString(options.HandleToken)
		}
	}

	return apis.CallWithoutResult(openFileCallName, parentWindow, dbus.UnixFD(fd), data)
}
