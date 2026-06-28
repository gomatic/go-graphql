package normalize

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	graphql "github.com/gomatic/go-graphql"
	"github.com/gomatic/go-graphql/schema"
)

const (
	schemaBom    schema.Schema = "bom"
	schemaStable schema.Schema = "stable"
)

func newComposite(t *testing.T) *schema.Composite {
	t.Helper()

	bomSDL := graphql.SDL(scalarsSDL + `
type Query { bomResolve(id: ID!): BomResult }
type Mutation { createGitObjectStatus: BomResult }
type BomResult { id: ID }
`)
	stableSDL := graphql.SDL(scalarsSDL + `
type Query {
  stableQuery(version: Int!): StableResult
  tagsSearch(tags: [String!]!): String
}
type StableResult { data: String }
`)
	c, err := schema.NewComposite(
		[]schema.Schema{schemaBom, schemaStable},
		map[schema.Schema]graphql.SDL{schemaBom: bomSDL, schemaStable: stableSDL},
	)
	require.NoError(t, err)
	return c
}

func TestProcessCompositeResolvesOwningSchema(t *testing.T) {
	t.Parallel()

	c := newComposite(t)

	tests := []struct {
		name          string
		query         QueryInput
		wantSchema    schema.Schema
		wantSubstring string
		wantVar1Type  string
	}{
		{name: "stable root field uses stable index", query: `query { stableQuery(version: 7) { data } }`, wantSchema: schemaStable, wantSubstring: "Int"},
		{name: "list of variables uses element type", query: `query($x: String!){ tagsSearch(tags: [$x]) }`, wantSchema: schemaStable, wantVar1Type: "String!"},
		{name: "bom root field uses bom index", query: `query { bomResolve(id: "a") { id } }`, wantSchema: schemaBom},
		{name: "introspection-only routes to primary", query: `query { __typename }`, wantSchema: schemaBom},
		{name: "mutation root resolves owning schema", query: `mutation { createGitObjectStatus { id } }`, wantSchema: schemaBom},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := Process(c, tt.query)
			require.NoError(t, err)
			assert.Equal(t, tt.wantSchema, result.Schema())
			if tt.wantSubstring != "" {
				assert.Contains(t, string(result.Query()), tt.wantSubstring)
			}
			if tt.wantVar1Type != "" {
				assert.Equal(t, tt.wantVar1Type, result.VariableTypes()["var1"])
			}
		})
	}
}

func TestProcessCompositeDetectionErrors(t *testing.T) {
	t.Parallel()

	c := newComposite(t)

	tests := []struct {
		wantErr error
		name    string
		query   QueryInput
	}{
		{name: "fields from different schemas conflict", query: `query { bomResolve(id: "a") { id } stableQuery(version: 1) { data } }`, wantErr: schema.ErrSchemaConflict},
		{name: "unknown root field", query: `query { unknownRoot }`, wantErr: schema.ErrUnknownField},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := Process(c, tt.query)
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestProcessWithSchemaHint(t *testing.T) {
	t.Parallel()

	c := newComposite(t)

	t.Run("valid hint selects schema without detection", func(t *testing.T) {
		t.Parallel()
		result, err := ProcessWithSchemaHint(c, `query { stableQuery(version: 7) { data } }`, schemaStable)
		require.NoError(t, err)
		assert.Equal(t, schemaStable, result.Schema())
	})

	t.Run("unknown hint falls back to detection", func(t *testing.T) {
		t.Parallel()
		result, err := ProcessWithSchemaHint(c, `query { stableQuery(version: 7) { data } }`, "nonexistent")
		require.NoError(t, err)
		assert.Equal(t, schemaStable, result.Schema())
	})

	t.Run("non-composite index ignores hint", func(t *testing.T) {
		t.Parallel()
		idx := newIndex(t, schemaBom, `type Query { bomResolve(id: ID!): BomResult }
type BomResult { id: ID }`)
		result, err := ProcessWithSchemaHint(idx, `query { bomResolve(id: "a") { id } }`, schemaStable)
		require.NoError(t, err)
		assert.Equal(t, schemaBom, result.Schema())
	})
}
