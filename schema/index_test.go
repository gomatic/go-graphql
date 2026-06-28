package schema

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vektah/gqlparser/v2/ast"

	graphql "github.com/gomatic/go-graphql"
)

// sdlFixture is SDL text we use to build test indexes.
type sdlFixture graphql.SDL

func mustIndex(t *testing.T, s Schema, sdl sdlFixture) *typeIndex {
	t.Helper()
	idx, err := NewIndex(s, graphql.SDL(sdl))
	require.NoError(t, err)
	ti, ok := idx.(*typeIndex)
	require.True(t, ok)
	return ti
}

func TestNewIndex(t *testing.T) {
	t.Parallel()

	tests := []struct {
		wantErr error
		name    string
		schema  Schema
		sdl     graphql.SDL
	}{
		{name: "valid", schema: "bom", sdl: "type Query { x: Int }"},
		{name: "invalid syntax", schema: "bom", sdl: "type Query {{{", wantErr: graphql.ErrParse},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			want, must := assert.New(t), require.New(t)

			idx, err := NewIndex(tt.schema, tt.sdl)

			if tt.wantErr != nil {
				must.ErrorIs(err, tt.wantErr)
				return
			}
			must.NoError(err)
			want.Equal(tt.schema, idx.Schema())
			must.NotNil(idx.GraphQLSchema())
		})
	}
}

func TestTypeIndexHasFieldAt(t *testing.T) {
	t.Parallel()

	const sdl = `
type Query {
	bomResolve(id: ID): BomResult
}
type BomResult {
	id: ID
}
`
	idx := mustIndex(t, "bom", sdl)

	tests := []struct {
		name  string
		typ   typeName
		field fieldName
		want  hasField
	}{
		{name: "existing field", typ: "Query", field: "bomResolve", want: true},
		{name: "missing field", typ: "Query", field: "notAField", want: false},
		{name: "missing type", typ: "Mutation", field: "bomResolve", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			want := assert.New(t)
			want.Equal(tt.want, idx.hasFieldAt(tt.typ, tt.field))
		})
	}
}

func TestTypeIndexGetArgType(t *testing.T) {
	t.Parallel()

	const sdl = `
type Query {
	bomResolve(id: ID, version: Int): BomResult
}
type BomResult {
	id: ID
}
`
	idx := mustIndex(t, "bom", sdl)

	tests := []struct {
		name  string
		typ   typeName
		field fieldName
		arg   argName
		want  typeName
	}{
		{name: "existing arg", typ: "Query", field: "bomResolve", arg: "id", want: "ID"},
		{name: "another arg", typ: "Query", field: "bomResolve", arg: "version", want: "Int"},
		{name: "missing arg", typ: "Query", field: "bomResolve", arg: "notAnArg", want: ""},
		{name: "missing field", typ: "Query", field: "notAField", arg: "id", want: ""},
		{name: "missing type", typ: "Mutation", field: "bomResolve", arg: "id", want: ""},
		{name: "empty arg is field return type", typ: "Query", field: "bomResolve", arg: "", want: "BomResult"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			want := assert.New(t)
			want.Equal(tt.want, idx.getArgType(tt.typ, tt.field, tt.arg))
		})
	}
}

func TestTypeIndexInterfaceMethods(t *testing.T) {
	t.Parallel()

	idx := mustIndex(t, "bom", "type Query { x(id: ID!): Int }")

	want := assert.New(t)
	want.Equal(ArgTypeResult("ID!"), idx.ArgType("Query", "x", "id"))
	want.Equal(HasFieldResult(true), idx.HasField("Query", "x"))
	want.Equal(Schema("bom"), idx.Schema())
	want.Equal("Query", idx.GraphQLSchema().Query.Name)
}

func TestTypeIndexGetSchema(t *testing.T) {
	t.Parallel()

	for _, s := range []Schema{"bom", "stable"} {
		t.Run(string(s), func(t *testing.T) {
			t.Parallel()
			idx := mustIndex(t, s, "type Query { x: Int }")
			assert.Equal(t, s, idx.getSchema())
		})
	}
}

func TestRootTypeNameForOperation(t *testing.T) {
	t.Parallel()

	const full = `
type Query { a: Int }
type Mutation { b: Int }
type Subscription { c: Int }
`
	const queryOnly = "type Query { a: Int }"
	const customRoots = `
schema { query: MyQuery mutation: MyMutation subscription: MySub }
type MyQuery { a: Int }
type MyMutation { b: Int }
type MySub { c: Int }
`

	tests := []struct {
		name string
		sdl  graphql.SDL
		op   ast.Operation
		want TypeNameInput
	}{
		{name: "query", sdl: full, op: ast.Query, want: "Query"},
		{name: "mutation present", sdl: full, op: ast.Mutation, want: "Mutation"},
		{name: "subscription present", sdl: full, op: ast.Subscription, want: "Subscription"},
		{name: "mutation absent falls back to query", sdl: queryOnly, op: ast.Mutation, want: "Query"},
		{name: "subscription absent falls back to query", sdl: queryOnly, op: ast.Subscription, want: "Query"},
		{name: "custom query root", sdl: customRoots, op: ast.Query, want: "MyQuery"},
		{name: "custom mutation root", sdl: customRoots, op: ast.Mutation, want: "MyMutation"},
		{name: "custom subscription root", sdl: customRoots, op: ast.Subscription, want: "MySub"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			idx := mustIndex(t, "bom", sdlFixture(tt.sdl))
			assert.Equal(t, tt.want, idx.RootTypeNameForOperation(tt.op))
		})
	}
}

// TestRootTypeNameForOperationDefaults covers the no-loaded-schema and no-Query
// fallbacks, built white-box since the public API never gets into these states.
func TestRootTypeNameForOperationDefaults(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		graphql *ast.Schema
		op      ast.Operation
		want    TypeNameInput
	}{
		{name: "nil schema query", graphql: nil, op: ast.Query, want: "Query"},
		{name: "nil schema mutation", graphql: nil, op: ast.Mutation, want: "Mutation"},
		{name: "nil schema subscription", graphql: nil, op: ast.Subscription, want: "Subscription"},
		{name: "no query type", graphql: &ast.Schema{}, op: ast.Query, want: "Query"},
		{name: "no query type mutation op", graphql: &ast.Schema{}, op: ast.Mutation, want: "Mutation"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			idx := newTypeIndexFromAST("bom", tt.graphql)
			assert.Equal(t, tt.want, idx.RootTypeNameForOperation(tt.op))
		})
	}
}

// TestTypeDefinitionNilSchema covers typeDefinition's nil-graphql branch.
func TestTypeDefinitionNilSchema(t *testing.T) {
	t.Parallel()

	idx := newTypeIndexFromAST("bom", nil)
	assert.Nil(t, idx.typeDefinition("Query"))
	assert.Equal(t, hasField(false), idx.hasFieldAt("Query", "x"))
}
