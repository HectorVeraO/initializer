// Package generator is the root of the initializer generator module.
package generator

// Version is the generator release written into catalogs and lock files.
const Version = "0.1.0"

// Commit is set by release builds:
// -ldflags "-X github.com/HectorVeraO/initializer/tooling/generator.Commit=<sha>".
// It is empty otherwise.
var Commit string
