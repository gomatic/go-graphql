# go-graphql

GraphQL schema parsing, composition, introspection, and operation normalization for Go — built on [`gqlparser/v2`](https://github.com/vektah/gqlparser).

## Packages

| Import | Purpose |
|---|---|
| [`github.com/gomatic/go-graphql`](.) | Parse GraphQL SDL text into a schema document. |
| [`github.com/gomatic/go-graphql/schema`](schema/) | Build a queryable schema index, compose multiple schemas (caller-supplied names and priority), and convert introspection JSON to SDL. |
| [`github.com/gomatic/go-graphql/normalize`](normalize/) | Normalize GraphQL operations — alias merging, variable coercion, comment stripping, and validation against a schema index. |

The library is pure parse/compose/normalize over in-memory SDL and JSON: it hard-codes no schema names and no cache paths. Callers own persistence and supply their own schema set.

## Errors

Every error a package can emit is a [`errs.Const`](https://github.com/gomatic/go-error) sentinel — match with `errors.Is`, never by string:

```go
_, err := graphql.Parse("schema", "type Query {{{")
if errors.Is(err, graphql.ErrParse) {
    // ...
}
```

## Build configuration is managed upstream

`Makefile`, `.golangci.yaml`, `.editorconfig`, `.gitignore`, `scripts/`, and `.github/` are distributed and owned by [`nicerobot/tools.repository`](https://github.com/nicerobot/tools.repository). Do not edit them in-tree — they are overwritten on the next push. Per-repo customization goes in a `Makefile.local`.

```sh
make check   # vet, lint, staticcheck, govulncheck, 100% coverage gate
```
