// Command server runs the HTTP API.
package main

import (
	"os"

	"github.com/HectorVeraO/initializer/go-srv/internal/platform/core"
)

func main() {
	os.Exit(core.Main(installedModules()))
}
