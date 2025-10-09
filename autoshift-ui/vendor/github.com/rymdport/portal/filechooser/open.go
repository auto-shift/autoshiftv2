package filechooser

import (
	"github.com/godbus/dbus/v5"
	"github.com/rymdport/portal/internal/apis"
	"github.com/rymdport/portal/internal/convert"
)

const openFileCallName = interfaceName + ".OpenFile"

// OpenFileOptions contains the options for how files are to be selected.
type OpenFileOptions struct {
	HandleToken   string      // A string that will be used as the last element of the handle. Must be a valid object path element.
	AcceptLabel   string      // Label for the accept button. Mnemonic underlines are allowed.
	NotModal      bool        // Whether the dialog should not be modal.
	Multiple      bool        // Whether multiple files can be selected or not.
	Directory     bool        // Whether to select for folders instead of files.
	Filters       []*Filter   // Each item specifies a single filter to offer to the user.
	CurrentFilter *Filter     // Request that this filter be set by default at dialog creation.
	Choices       []*ComboBox // List of serialized combo boxes to add to the file chooser.
	CurrentFolder string      // Suggested folder from which the files should be opened.
}

// OpenFile opens a filechooser for selecting a file to open.
// The chooser will use the supplied title as it's name.
func OpenFile(parentWindow, title string, options *OpenFileOptions) ([]string, error) {
	data := map[string]dbus.Variant{}

	if options != nil {
		data = map[string]dbus.Variant{
			"modal":     convert.FromBool(!options.NotModal),
			"multiple":  convert.FromBool(options.Multiple),
			"directory": convert.FromBool(options.Directory),
		}

		if options.HandleToken != "" {
			data["handle_token"] = convert.FromString(options.HandleToken)
		}

		if options.AcceptLabel != "" {
			data["accept_label"] = convert.FromString(options.AcceptLabel)
		}

		if len(options.Filters) > 0 {
			data["filters"] = dbus.MakeVariant(options.Filters)
		}

		if options.CurrentFilter != nil {
			data["current_filter"] = dbus.MakeVariant(options.CurrentFilter)
		}

		if len(options.Choices) > 0 {
			data["choices"] = dbus.MakeVariant(options.Choices)
		}

		if options.CurrentFolder != "" {
			data["current_folder"] = convert.ToNullTerminatedValue(options.CurrentFolder)
		}
	}

	result, err := apis.Call(openFileCallName, parentWindow, title, data)
	if err != nil {
		return nil, err
	}

	return readURIFromResponse(result.(dbus.ObjectPath))
}
