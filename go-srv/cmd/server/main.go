// Command server runs the go-srv HTTP API.
package main

import (
	"os"

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core"
)

func main() {
	os.Exit(core.Main(installedModules()))
}
