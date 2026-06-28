package schema

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vektah/gqlparser/v2/ast"

	graphql "github.com/gomatic/go-graphql"
)

const (
	bomSDL = `
type Query { bomResolve(id: ID!): BomResult }
type Mutation { createGitObjectStatus: BomResult }
type BomResult { id: ID }
`
	stableSDL = `
type Query { stableQuery(version: Int!): StableResult  tagsSearch(tags: [String!]!): String }
type StableResult { data: String }
`
)

func mustComposite(t *testing.T) *Composite {
	t.Helper()
	c, err := NewComposite(
		[]Schema{"bom", "stable"},
		map[Schema]graphql.SDL{"bom": bomSDL, "stable": stableSDL},
	)
	require.NoError(t, err)
	return c
}

func TestNewCompositeErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		wantErr error
		sdls    map[Schema]graphql.SDL
		name    string
		order   []Schema
	}{
		{name: "empty order", order: nil, sdls: map[Schema]graphql.SDL{}, wantErr: ErrNoSchemas},
		{name: "missing sdl", order: []Schema{"bom"}, sdls: map[Schema]graphql.SDL{}, wantErr: ErrSchemaSDLMissing},
		{name: "invalid sdl", order: []Schema{"bom"}, sdls: map[Schema]graphql.SDL{"bom": "type Query {{{"}, wantErr: graphql.ErrParse},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := NewComposite(tt.order, tt.sdls)
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestCompositePriorityFirstOwnerWins(t *testing.T) {
	t.Parallel()

	const a = "type Query { sharedRoot(x: String!): AOnly }\n type AOnly { v: String }"
	const b = "type Query { sharedRoot(x: Int!): BOnly }\n type BOnly { v: String }"

	tests := []struct {
		name  string
		want  Schema
		order []Schema
	}{
		{name: "a first", order: []Schema{"a", "b"}, want: "a"},
		{name: "b first", order: []Schema{"b", "a"}, want: "b"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			c, err := NewComposite(tt.order, map[Schema]graphql.SDL{"a": a, "b": b})
			require.NoError(t, err)
			assert.Equal(t, tt.want, c.queryField["sharedRoot"])
		})
	}
}

func TestCompositeDetectSchema(t *testing.T) {
	t.Parallel()

	c := mustComposite(t)

	tests := []struct {
		wantErr error
		name    string
		want    Schema
		fields  []FieldNameInput
	}{
		{name: "empty returns primary", fields: nil, want: "bom"},
		{name: "single bom query field", fields: []FieldNameInput{"bomResolve"}, want: "bom"},
		{name: "mutation field routes to bom", fields: []FieldNameInput{"createGitObjectStatus"}, want: "bom"},
		{name: "single stable field", fields: []FieldNameInput{"stableQuery"}, want: "stable"},
		{name: "two same-schema fields", fields: []FieldNameInput{"stableQuery", "tagsSearch"}, want: "stable"},
		{name: "unknown field", fields: []FieldNameInput{"nope"}, wantErr: ErrUnknownField},
		{name: "unknown second field", fields: []FieldNameInput{"bomResolve", "nope"}, wantErr: ErrUnknownField},
		{name: "conflict", fields: []FieldNameInput{"bomResolve", "stableQuery"}, wantErr: ErrSchemaConflict},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			want, must := assert.New(t), require.New(t)

			got, err := c.DetectSchema(tt.fields)

			if tt.wantErr != nil {
				must.ErrorIs(err, tt.wantErr)
				return
			}
			must.NoError(err)
			want.Equal(tt.want, got)
		})
	}
}

func TestCompositeForSchema(t *testing.T) {
	t.Parallel()

	c := mustComposite(t)

	tests := []struct {
		name   string
		schema Schema
		wantOk bool
	}{
		{name: "bom present", schema: "bom", wantOk: true},
		{name: "stable present", schema: "stable", wantOk: true},
		{name: "absent", schema: "ssr", wantOk: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			idx := c.ForSchema(tt.schema)
			if tt.wantOk {
				require.NotNil(t, idx)
				assert.Equal(t, tt.schema, idx.Schema())
				return
			}
			assert.Nil(t, idx)
		})
	}
}

func TestCompositeArgType(t *testing.T) {
	t.Parallel()

	c := mustComposite(t)

	tests := []struct {
		name  string
		typ   TypeNameInput
		field FieldNameInput
		arg   ArgNameInput
		want  ArgTypeResult
	}{
		{name: "bom field arg", typ: "Query", field: "bomResolve", arg: "id", want: "ID!"},
		{name: "stable field arg", typ: "Query", field: "stableQuery", arg: "version", want: "Int!"},
		{name: "missing arg", typ: "Query", field: "bomResolve", arg: "missing", want: ""},
		{name: "missing field", typ: "Query", field: "missing", arg: "id", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, c.ArgType(tt.typ, tt.field, tt.arg))
		})
	}
}

func TestCompositeHasField(t *testing.T) {
	t.Parallel()

	c := mustComposite(t)

	tests := []struct {
		name  string
		typ   TypeNameInput
		field FieldNameInput
		want  HasFieldResult
	}{
		{name: "bom query field", typ: "Query", field: "bomResolve", want: true},
		{name: "stable query field", typ: "Query", field: "stableQuery", want: true},
		{name: "bom result field", typ: "BomResult", field: "id", want: true},
		{name: "stable result field", typ: "StableResult", field: "data", want: true},
		{name: "missing field", typ: "Query", field: "missing", want: false},
		{name: "missing type", typ: "Missing", field: "id", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, c.HasField(tt.typ, tt.field))
		})
	}
}

func TestCompositeIndexInterface(t *testing.T) {
	t.Parallel()

	c := mustComposite(t)
	want := assert.New(t)

	want.Equal(Schema("bom"), c.Schema())
	want.Nil(c.GraphQLSchema())
	want.Equal(TypeNameInput("Query"), c.RootTypeNameForOperation(ast.Query))
	want.Equal(TypeNameInput("Mutation"), c.RootTypeNameForOperation(ast.Mutation))
}

// TestCompositeBuildQueryFieldMapNilSchema covers the nil-GraphQLSchema guard in
// buildQueryFieldMap, using a sub-index that has no loaded schema.
func TestCompositeBuildQueryFieldMapNilSchema(t *testing.T) {
	t.Parallel()

	c := &Composite{
		indexes:    make(map[Schema]Index),
		primary:    "bom",
		queryField: make(fieldSchemaMap),
	}
	c.buildQueryFieldMap("bom", newTypeIndexFromAST("bom", nil))

	assert.Empty(t, c.queryField)
}
