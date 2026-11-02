# product-vertical fixture

A product vertical the `generated-project-product-module` CI job copies into a
freshly generated project. Nothing here is delivered by `generate`, listed in a
manifest, or hashed into `initializer.lock.json`.

| Path | Destination and use |
|---|---|
| `internal/dispatch/` | Copied to `<component>/internal/dispatch/`; one line registered in `<component>/cmd/server/product.go`. |
| `specs/dispatch/` | The vertical's contract and TCK, not copied by that job: the TCK certifies a running instance and the job boots no identity provider. |
| imports | Name `example.com/fixture/srv`, the module path the job passes to `generate --go-module`; changing one without the other breaks the build. |
