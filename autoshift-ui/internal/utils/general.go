package utils

import "os"

// creates a temp dir
func CreateTempDir() string {
	dname, err := os.MkdirTemp("", "asui-temp-")
	CheckIfError(err)
	return dname
}

// creates a temp file
func CreateTempFile() {

}
