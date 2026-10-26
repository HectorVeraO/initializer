package gorewrite

import (
	"bytes"
	"go/parser"
	"go/token"
	"testing"
)

func FuzzRewriteImports(f *testing.F) {
	f.Add([]byte("package p\n"))
	f.Add([]byte("package p\n\nimport (\n\t\"fmt\"\n\n\t\"example.com/fixture/srv/a\"\n)\n"))
	f.Add([]byte("package p\n\nimport c `example.com/fixture/srv`\n"))
	f.Add([]byte("package p\n\n// example.com/fixture/srv\n"))
	f.Add([]byte("package p\n\nimport (\n\t\"example.com/fixture/srv/a\" // c\n)\n"))
	f.Add([]byte("package p\n\nimport \"example.com/fixture/\\x73rv\"\n"))
	f.Fuzz(func(t *testing.T, src []byte) {
		out, count, err := RewriteImports("f.go", src, source, target)
		if err != nil {
			return
		}
		isUnchanged := bytes.Equal(out, src)
		if (count == 0) != isUnchanged {
			t.Fatalf("count %d disagrees with change: unchanged=%v", count, isUnchanged)
		}
		if bytes.Contains(out, []byte(source)) {
			t.Fatalf("output still holds the source module path:\n%s", out)
		}
		if _, parseErr := parser.ParseFile(token.NewFileSet(), "f.go", out, parser.ImportsOnly); parseErr != nil {
			t.Fatalf("output does not parse: %v\n%s", parseErr, out)
		}
	})
}

func FuzzRewriteModuleLine(f *testing.F) {
	f.Add([]byte("module example.com/fixture/srv\n\ngo 1.26\n"))
	f.Add([]byte("module example.com/fixture/srv\r\n"))
	f.Add([]byte(""))
	f.Add([]byte("module example.com/fixture/srv\nreplace a => example.com/fixture/srv\n"))
	f.Fuzz(func(t *testing.T, gomod []byte) {
		out, err := RewriteModuleLine(gomod, source, target)
		if err != nil {
			return
		}
		if bytes.Contains(out, []byte(source)) {
			t.Fatalf("output still holds the source module path:\n%s", out)
		}
	})
}
