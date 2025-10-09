[![Go API Reference](https://img.shields.io/badge/go-documentation-blue.svg?style=flat)](https://pkg.go.dev/github.com/rymdport/portal)
[![Tests](https://github.com/rymdport/portal/actions/workflows/tests.yml/badge.svg)](https://github.com/rymdport/portal/actions/workflows/tests.yml)
[![Analysis](https://github.com/rymdport/portal/actions/workflows/analysis.yml/badge.svg)](https://github.com/rymdport/portal/actions/workflows/analysis.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/rymdport/portal)](https://goreportcard.com/report/github.com/rymdport/portal)

# Portal

Portal is a Go module providing developer friendly functions for accessing the [XDG Desktop Protocol](https://flatpak.github.io/xdg-desktop-portal/) D-Bus API. The goal of this project is to be toolkit agnostic and allow using the portals without needing to access [libportal](https://github.com/flatpak/libportal) through CGo. 

Using the portals allow applications to request information from the user even when running inside a sandbox like Flatpak. As such, it is possible to open file dialogs, open links in the browser, send notifications and much more in a way that integrates well with the desktop environment. This also avoids needing to open up permissions in the sandbox.

## API

The api of this Go module is designed to closely follow the structure naming of the upstream APIs. This means, in practice, that each D-Bus interface is implemented as its own package here. However, care is taken to be developer friendly and integrate seamlessly with native Go types.

- Documentation for this module and all of its packages can be found on pkg.go.dev: https://pkg.go.dev/github.com/rymdport/portal
- Documentation for the D-Bus protocol of the portals: https://flatpak.github.io/xdg-desktop-portal/docs/api-reference.html


The version of this module's API is still in a `v0.X.Y` state and is subject to change in the future.
A release with breaking changes will increment X while Y will be incremented when there are minor bug or feature improvements.

## Example

The following example showcases how a file chooser can be opened for selecting one or more files.


```go
package main

import (
	"fmt"
	"log"

	"github.com/rymdport/portal/filechooser"
)

func main() {
	options := filechooser.OpenFileOptions{Multiple: true}
	files, err := filechooser.OpenFile("", "Select files", &options)
	if err != nil {
		log.Fatalln(err)
	}

	fmt.Println(files)
}
```

## Supported Portal Interfaces

The list below contains all of the portal interfaces available within the project. Checked boxes are partially or completely implemented within this project. Note that this list usually refers to the state of the `main` branch and not necessarily the latest release.

- [x] Account
- [x] Background
- [ ] Camera
- [ ] Clipboard
- [ ] Documents
- [ ] Dynamic Launcher
- [ ] Email
- [x] File Chooser
- [ ] File Transfer
- [ ] Game Mode
- [ ] Global Shortcuts
- [ ] Inhibit
- [ ] Input Capture
- [ ] Location
- [x] Memory Monitor
- [x] Network Monitor
- [x] Notification
- [x] OpenURI
- [x] Power Profile Monitor
- [ ] Print
- [x] Proxy Resolver
- [ ] Realtime
- [ ] Remote Desktop
- [x] Request
- [ ] ScreenCast
- [ ] Screenshot
- [ ] Secret
- [x] Session
- [x] Settings
- [x] Trash
- [ ] Usb
- [ ] Wallpaper


## Used by other projects

This section is meant as a reference to where this project is being used. Feel free to add yours if desired.

- This project is used as of the [v2.5.0](https://github.com/fyne-io/fyne/releases/tag/v2.5.0) release of [Fyne](https://fyne.io).
  - All the old theme watching code has been replaced by the `settings` package (and `appearance` subpackage) from this module. The `filechooser` and `notification` packages replace the old Fyne-code when compiling with `-tags flatpak`.

## Contributing

Contributions are strongly appreciated. Everything from creating bug reports to contributing code will help the project a lot, so please feel free to help in any way, shape, or form that you feel comfortable with.

## License
- Portal is licensed under `Apache License Version 2.0` and will forever continue to be free and open source.
