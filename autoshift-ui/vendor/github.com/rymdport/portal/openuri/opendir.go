package openuri

import (
	"github.com/godbus/dbus/v5"
	"github.com/rymdport/portal/internal/apis"
	"github.com/rymdport/portal/internal/convert"
)

const openDirCallName = interfaceName + ".OpenDirectory"

// OpenDirOptions holds optional settings that can be passed to the OpenDir call.
type OpenDirOptions struct {
	HandleToken string // A string that will be used as the last element of the handle. Must be a valid object path element.
	Writable    bool   // Whether to allow the chosen application to write to the file. This key only takes effect the uri points to a local file that is exported in the document portal, and the chosen application is sandboxed itself.
	Ask         bool   // Whether to ask the user to choose an app. If this is not passed, or false, the portal may use a default or pick the last choice.
}

// OpenDirectory asks to open the directory containing a local file in the file browser.
// The input parameter fd should be a file descriptor like the one given from [*os.File.Fd] for example.
func OpenDirectory(parentWindow string, fd uintptr, options *OpenDirOptions) error {
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

	return apis.CallWithoutResult(openDirCallName, parentWindow, dbus.UnixFD(fd), data)
}
