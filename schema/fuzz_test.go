package schema

import (
	"errors"
	"testing"

	graphql "github.com/gomatic/go-graphql"
)

// introspectionSentinels is every error IntrospectionToSDL is specified to emit.
// A failure outside this set is a leaked, unwrapped error.
var introspectionSentinels = []error{
	ErrIntrospectionParse,
	ErrIntrospectionGraphQLErrors,
	ErrIntrospectionEmpty,
	ErrIntrospectionUnionEmpty,
	ErrIntrospectionSDL,
	ErrIntrospectionUnmarshal,
	ErrIntrospectionNilType,
	ErrIntrospectionMissingName,
	ErrIntrospectionUnsupportedKind,
}

// assertKnown fails unless err matches one of the supplied sentinels.
func assertKnown(t *testing.T, err error, sentinels []error) {
	t.Helper()
	for _, s := range sentinels {
		if errors.Is(err, s) {
			return
		}
	}
	t.Fatalf("error is not a known sentinel: %v", err)
}

// FuzzIntrospectionToSDL asserts the converter never panics on arbitrary JSON and
// only ever fails with one of its declared sentinels — no raw, unwrapped error
// ever leaks out.
//
// It does NOT assert that produced SDL re-parses for arbitrary bytes: the
// round-trip contract holds for VALID introspection (every name is a legal GraphQL
// identifier), which the comprehensive/sample example tests exercise by loading an
// Index. Adversarially-malformed introspection — e.g. a type whose name is "0" or
// otherwise not a GraphQL identifier — can yield SDL that does not re-parse; the
// library does not promise to sanitize the full identifier grammar of input that
// no real GraphQL server emits.
func FuzzIntrospectionToSDL(f *testing.F) {
	seeds := []string{
		"",
		"{invalid}",
		"{}",
		`{"errors":[{"message":"boom"}],"data":{"schema":{"types":[]}}}`,
		comprehensiveIntrospection,
		`{"data":{"schema":{"types":[{"kind":"OBJECT","name":"Query","fields":[{"name":"x","args":[],"type":{"kind":"SCALAR","name":"Boolean"}}]}]}}}`,
		`{"data":{"__schema":{"types":[{"kind":"ENUM","name":"E","enumValues":null}]}}}`,
		`{"data":{"schema":{"types":[{"kind":"UNION","name":"U","possible_types":null}]}}}`,
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, data string) {
		_, err := IntrospectionToSDL(IntrospectionJSON(data))
		if err != nil {
			assertKnown(t, err, introspectionSentinels)
		}
	})
}

// FuzzNewIndex asserts NewIndex never panics on arbitrary SDL and only ever fails
// with graphql.ErrParse; on success it yields a usable index that identifies the
// schema it was built for.
func FuzzNewIndex(f *testing.F) {
	seeds := []string{
		"",
		"type Query { x: Int }",
		"type Query {{{",
		"schema { query: Q } type Q { a: Int }",
		"scalar JSON type Query { a(f: JSON): String }",
		"# just a comment",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, sdl string) {
		idx, err := NewIndex("fuzz", graphql.SDL(sdl))
		if err != nil {
			assertKnown(t, err, []error{graphql.ErrParse})
			return
		}
		if idx.Schema() != "fuzz" {
			t.Fatalf("index must identify its schema, got %q", idx.Schema())
		}
	})
}
