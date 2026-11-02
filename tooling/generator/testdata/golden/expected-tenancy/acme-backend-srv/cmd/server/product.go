package main

import "example.com/acme/backend/internal/platform/core"

// productModules returns the modules this project registers itself.
//
// This file is project-owned: product verticals add themselves here, and the
// edit survives every delivery of the generator. modules.go and main.go are
// generator-owned and must not be edited.
func productModules() []core.Module {
	return nil
}
