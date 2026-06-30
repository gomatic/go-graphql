package graphql

import (
	"errors"
	"testing"
)

// FuzzParse asserts the Parse contract holds across arbitrary SDL bytes: it never
// panics, a failure is always wrapped in ErrParse with a nil document, and a
// success always yields a non-nil document.
func FuzzParse(f *testing.F) {
	seeds := []string{
		"",
		"type Query { hello: String }",
		"type Query {{{",
		"schema { query: Q } type Q { a: Int }",
		"type Query { a(x: [Int!]!): String }",
		`"""desc""" type Query { a: Int }`,
		"type Query { 日本語: String }",
		"type T { a: T } type Query { t: T }",
		"# comment only",
		"enum E { A B } input I { x: Int } union U = T",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, sdl string) {
		doc, err := Parse("fuzz", SDL(sdl))
		if err != nil {
			if !errors.Is(err, ErrParse) {
				t.Fatalf("parse error not wrapped in ErrParse: %v", err)
			}
			if doc != nil {
				t.Fatalf("doc must be nil when Parse fails")
			}
			return
		}
		if doc == nil {
			t.Fatalf("doc must be non-nil when Parse succeeds")
		}
	})
}
