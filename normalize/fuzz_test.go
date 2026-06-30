package normalize

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	graphql "github.com/gomatic/go-graphql"
	"github.com/gomatic/go-graphql/schema"
)

// fuzzBody is a fixed single-schema fixture the Process fuzz target normalizes
// arbitrary queries against. It carries the built-in scalars so the post-rewrite
// validator can resolve variable types.
const fuzzBody = scalarsSDL + `
enum BomStatus { ACTIVE INACTIVE }
input BomInput { name: String, version: Int }
type Query {
  bomResolve(id: ID!, version: Int, score: Float, active: Boolean, ids: [String!], filter: String): BomResult
  bomList(active: Boolean!, status: BomStatus!): BomList
  bomCreate(input: BomInput!): BomResult
  instances: [Instance!]!
}
type Mutation { createGitObjectStatus: BomResult }
type BomResult { id: ID, name: String }
type BomList { items: String }
type Instance { fiName: String }
`

// mustFuzzIndex builds the fixed Process fixture once. It panics on failure
// because the SDL is a compile-time constant — a parse failure here is a test bug.
func mustFuzzIndex() schema.Index {
	idx, err := schema.NewIndex("bom", graphql.SDL(fuzzBody))
	if err != nil {
		panic(err)
	}
	return idx
}

// assertKnownErr fails unless err matches one of the sentinels the function under
// test is specified to emit, catching any raw or unwrapped error leaking out.
func assertKnownErr(t *testing.T, err error, sentinels ...error) {
	t.Helper()
	for _, s := range sentinels {
		if errors.Is(err, s) {
			return
		}
	}
	t.Fatalf("error is not a known sentinel: %v", err)
}

// FuzzFormat asserts Format never panics, only ever fails with its declared
// sentinels, and is idempotent: formatting already-minimal output reproduces it
// exactly. Idempotence is the contract that makes the formatted text a stable
// cache key.
func FuzzFormat(f *testing.F) {
	seeds := []string{
		"",
		"{ broken",
		"{ a { b c } }",
		"{\n  # comment\n  a { id }\n}",
		`query Q($x: Int) { a(n: $x) { id } }`,
		`{ a(s: "hello world", n: 1, f: 1.5, b: true) }`,
		"{ a { ... on T { id } ...F } } fragment F on T { name }",
		"mutation { m(input: {a: 1, b: [1,2,3]}) { id } }",
		"{ 日本語 }",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		out, err := Format(QueryInput(raw))
		if err != nil {
			assertKnownErr(t, err, ErrEmptyQuery, ErrQueryParse)
			return
		}
		// A document with no operations (e.g. comment-only input) legitimately
		// formats to "", and Format("") is ErrEmptyQuery by contract; idempotence
		// is asserted only for non-empty formatted output, which must be a fixed point.
		if out == "" {
			return
		}
		again, err2 := Format(QueryInput(string(out)))
		require.NoError(t, err2, "re-formatting minimal output must succeed")
		require.Equal(t, out, again, "Format must be idempotent on non-empty output")
	})
}

// FuzzProcess asserts Process never panics on arbitrary query text against a fixed
// schema, only ever fails with its declared sentinels, and produces a non-empty
// normalized query on success.
func FuzzProcess(f *testing.F) {
	idx := mustFuzzIndex()
	seeds := []string{
		"",
		"{ broken",
		"{ bomResolve(id: \"x\") { id } }",
		"{ nonExistent }",
		"{ bomResolve { id } }",
		`{ bomCreate(input: {name: "a", version: 1}) { id } }`,
		"{ bomList(active: true, status: ACTIVE) { items } }",
		"{ instances { fiName } }",
		"mutation { createGitObjectStatus { id } }",
		`query($x: ID!){ bomResolve(id: $x) { id } }`,
		"{ __schema { types { name } } }",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		res, err := Process(idx, QueryInput(raw))
		if err != nil {
			assertKnownErr(t, err,
				ErrEmptyQuery, ErrQueryParse, ErrGraphQLTypeUnresolved,
				ErrGraphQLValidation, schema.ErrUnknownField)
			return
		}
		// On success the result is always flagged normalized and targets the
		// index's schema — the contract buildResult upholds for every Process.
		require.True(t, res.IsNormalized(), "successful result must be normalized")
		require.Equal(t, schema.Schema("bom"), res.Schema(), "result must target the index schema")
	})
}

// FuzzResolveOperation asserts ResolveOperation never panics, only ever fails with
// its declared sentinels, and is deterministic: the same input yields the same
// query text and operation name every call (the hash-derived name must be stable).
func FuzzResolveOperation(f *testing.F) {
	seeds := []string{
		"",
		"query (",
		"{ a { id } }",
		"query Q { a }",
		"query A { a } query B { b }",
		"fragment F on T { id }",
		"{\n  # c\n  a\n}",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		q1, n1, err1 := ResolveOperation(QueryInput(raw), "")
		if err1 != nil {
			assertKnownErr(t, err1, ErrEmptyQuery, ErrQueryParse)
			return
		}
		q2, n2, err2 := ResolveOperation(QueryInput(raw), "")
		require.NoError(t, err2)
		require.Equal(t, n1, n2, "operation name must be deterministic")
		require.Equal(t, q1, q2, "formatted query must be deterministic")
	})
}
