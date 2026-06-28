// Package graphql turns GraphQL schema definition (SDL) text into the
// [gqlparser] abstract syntax tree, wrapping any failure in a sentinel
// ([ErrParse]). It's the foundation the schema and normalize subpackages build
// on: schema composition and introspection live in schema/, operation
// normalization in normalize/.
//
// [gqlparser]: https://github.com/vektah/gqlparser
package graphql

import (
	errs "github.com/gomatic/go-error"
	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/parser"
)

// ErrParse means we couldn't parse the SDL text into a schema document.
const ErrParse errs.Const = "parse schema"

// Name identifies a schema — usually the base name of its source file.
type Name string

// SDL is GraphQL schema definition text.
type SDL string

// Parse turns SDL text into a schema document. We record the source under name
// so parse errors point somewhere useful, and we don't treat it as a built-in
// schema. If parsing fails, you get back the error wrapped in [ErrParse].
func Parse(name Name, sdl SDL) (*ast.SchemaDocument, error) {
	doc, err := parser.ParseSchema(&ast.Source{
		Name:    string(name),
		Input:   string(sdl),
		BuiltIn: false,
	})
	if err != nil {
		return nil, ErrParse.With(err)
	}
	return doc, nil
}
