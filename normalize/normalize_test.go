package normalize

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/gomatic/go-graphql/schema"
)

// bomBody is a small single-schema fixture we reuse across the Process tests.
const bomBody = `
enum BomStatus { ACTIVE INACTIVE }
input BomInput { name: String, version: Int }
type Query {
  bomResolve(id: ID!, version: Int, score: Float, active: Boolean, limit: Int, ids: [String!], filter: String): BomResult
  bomList(active: Boolean!, status: BomStatus!): BomList
  bomSearch(score: Float!, filter: String): BomResult
  bomCreate(input: BomInput!): BomResult
}
type Mutation { createGitObjectStatus: BomResult }
type BomResult { id: ID, name: String }
type BomList { items: String }
`

func TestProcessErrorsAndSchema(t *testing.T) {
	t.Parallel()

	idx := newIndex(t, "bom", bomBody)

	tests := []struct {
		name       string
		query      QueryInput
		wantErr    error
		wantSchema schema.Schema
	}{
		{name: "empty query", query: "", wantErr: ErrEmptyQuery},
		{name: "invalid syntax", query: "{ invalid", wantErr: ErrQueryParse},
		{name: "missing field", query: "{ nonExistentField { id } }", wantErr: ErrBuildSchemaIndex},
		{name: "valid simple query", query: "{ bomResolve(id: \"a\") { id } }", wantSchema: "bom"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := Process(idx, tt.query)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.wantSchema, result.Schema())
		})
	}
}

func TestProcessValidatorRejectsMissingRequiredArgument(t *testing.T) {
	t.Parallel()

	idx := newIndex(t, "bom", bomBody)
	// bomResolve.id is required (ID!) but we left it off; the rewrite leaves the
	// document invalid, so the post-rewrite validator rejects it.
	_, err := Process(idx, `{ bomResolve { id } }`)
	require.ErrorIs(t, err, ErrGraphQLValidation)
}

func TestProcessMissingArgumentTypeFails(t *testing.T) {
	t.Parallel()

	idx := newIndex(t, "bom", `type Query { bomResolve: BomResult }
type BomResult { id: ID }`)
	// id isn't a declared argument, so there's no way to resolve its type.
	_, err := Process(idx, `{ bomResolve(id: $myVar) { id } }`)
	require.ErrorIs(t, err, ErrGraphQLTypeUnresolved)
}

func TestProcessExtractsScalarVariables(t *testing.T) {
	t.Parallel()

	idx := newIndex(t, "bom", bomBody)

	tests := []struct {
		wantVarValue any
		name         string
		query        QueryInput
		wantVarName  string
		wantHasVars  bool
	}{
		{name: "string id", query: `{ bomResolve(id: "abc123") { name } }`, wantHasVars: true, wantVarName: "var1", wantVarValue: "abc123"},
		{name: "int version", query: `{ bomResolve(id: "x", version: 42) { name } }`, wantHasVars: true, wantVarName: "var2", wantVarValue: int64(42)},
		{name: "float score", query: `{ bomSearch(score: 3.14) { id } }`, wantHasVars: true, wantVarName: "var1", wantVarValue: 3.14},
		{name: "bool active", query: `{ bomList(active: true, status: ACTIVE) { items } }`, wantHasVars: true, wantVarName: "var1", wantVarValue: true},
		{name: "enum status", query: `{ bomList(active: false, status: ACTIVE) { items } }`, wantHasVars: true, wantVarName: "var2", wantVarValue: "ACTIVE"},
		{name: "no args no vars", query: `{ bomResolve(id: "x") { name } }`, wantHasVars: true, wantVarName: "var1", wantVarValue: "x"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := Process(idx, tt.query)
			require.NoError(t, err)
			assert.Equal(t, tt.wantHasVars, result.HasVars())
			assert.Equal(t, tt.wantVarValue, result.Variables()[tt.wantVarName])
		})
	}
}

func TestProcessExistingVariableGetsZeroValueFromSchema(t *testing.T) {
	t.Parallel()

	idx := newIndex(t, "bom", `type Query {
  qInt(v: Int!): R
  qFloat(v: Float!): R
  qBool(v: Boolean!): R
  qID(v: ID!): R
  qNullableInt(v: Int): R
  qList(v: [Int!]!): R
}
type R { id: ID }`)

	tests := []struct {
		wantVarValue any
		name         string
		query        QueryInput
	}{
		{name: "int zero", query: `{ qInt(v: $x) { id } }`, wantVarValue: int64(0)},
		{name: "float zero", query: `{ qFloat(v: $x) { id } }`, wantVarValue: float64(0)},
		{name: "bool zero", query: `{ qBool(v: $x) { id } }`, wantVarValue: false},
		{name: "id string zero", query: `{ qID(v: $x) { id } }`, wantVarValue: ""},
		{name: "nullable int zero", query: `{ qNullableInt(v: $x) { id } }`, wantVarValue: int64(0)},
		{name: "list element int zero", query: `{ qList(v: $x) { id } }`, wantVarValue: int64(0)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := Process(idx, tt.query)
			require.NoError(t, err)
			// The existing variable reference turns into var1.
			assert.Equal(t, tt.wantVarValue, result.Variables()["var1"])
		})
	}
}

func TestProcessListValues(t *testing.T) {
	t.Parallel()

	idx := newIndex(t, "catalog", `type Query { bomResolve(ids: [String!], versions: [Int!]): BomResult }
type BomResult { id: ID }`)

	tests := []struct {
		wantVarValue any
		name         string
		query        QueryInput
	}{
		{name: "list of strings", query: `{ bomResolve(ids: ["a", "b", "c"]) { id } }`, wantVarValue: []any{"a", "b", "c"}},
		{name: "list of ints", query: `{ bomResolve(versions: [1, 2, 3]) { id } }`, wantVarValue: []any{int64(1), int64(2), int64(3)}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := Process(idx, tt.query)
			require.NoError(t, err)
			assert.Equal(t, tt.wantVarValue, result.Variables()["var1"])
		})
	}
}

func TestProcessNullValueProducesNoVariable(t *testing.T) {
	t.Parallel()

	idx := newIndex(t, "bom", bomBody)
	result, err := Process(idx, `{ bomResolve(id: "x", filter: null) { id } }`)
	require.NoError(t, err)
	// Only the id literal turns into a variable; null doesn't.
	assert.Len(t, result.Variables(), 1)
}

func TestProcessObjectValues(t *testing.T) {
	t.Parallel()

	idx := newIndex(t, "bom", bomBody)

	tests := []struct {
		name        string
		query       QueryInput
		wantVarKeys []string
	}{
		{name: "object with one field", query: `{ bomCreate(input: {name: "test"}) { id } }`, wantVarKeys: []string{"var1"}},
		{name: "object with two fields", query: `{ bomCreate(input: {name: "test", version: 1}) { id } }`, wantVarKeys: []string{"var1", "var2"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := Process(idx, tt.query)
			require.NoError(t, err)
			for _, key := range tt.wantVarKeys {
				assert.Contains(t, result.Variables(), key)
			}
		})
	}
}

func TestProcessObjectFieldNotInInputTypeFails(t *testing.T) {
	t.Parallel()

	idx := newIndex(t, "bom", `input BomInput { name: String }
type Query { bomCreate(input: BomInput!): BomResult }
type BomResult { id: ID }`)
	_, err := Process(idx, `{ bomCreate(input: {unknownField: 1}) { id } }`)
	require.ErrorIs(t, err, ErrGraphQLTypeUnresolved)
}

func TestProcessListOfVariablesUsesElementType(t *testing.T) {
	t.Parallel()

	idx := newIndex(t, "bom", `type Query { tagsSearch(tags: [String!]!): String }`)
	result, err := Process(idx, `query($x: String!){ tagsSearch(tags: [$x]) }`)
	require.NoError(t, err)
	assert.Equal(t, "String!", result.VariableTypes()["var1"])
}

func TestProcessListOfVariablesScalarArgFails(t *testing.T) {
	t.Parallel()

	// tags is a scalar, not a list, so a list literal of variables has no element
	// type to resolve from the schema.
	idx := newIndex(t, "bom", `type Query { tagsSearch(tags: String): String }`)
	_, err := Process(idx, `query($x: String!){ tagsSearch(tags: [$x]) }`)
	require.ErrorIs(t, err, ErrGraphQLTypeUnresolved)
}

// Inline fragments and fragment spreads can't be validated against the minimal
// schema that schema.NewIndex builds (it has no PossibleTypes), so these tests
// run normalize's structural handling through a schema.Index that exposes no
// loaded validation schema — the validator gets skipped, which is fine here.
func TestProcessInlineFragment(t *testing.T) {
	t.Parallel()

	// The sibling leaf field next to the inline fragment forces the selection-set
	// sort to compare a Field against a non-Field.
	result, err := Process(nilSchemaIndex{}, `{ bomResolve { id ... on BomResult { name } } }`)
	require.NoError(t, err)
	assert.Equal(t, schema.Schema("nilschema"), result.Schema())
}

func TestProcessFragmentSpread(t *testing.T) {
	t.Parallel()

	result, err := Process(nilSchemaIndex{}, `{ bomResolve { ...F } } fragment F on BomResult { id }`)
	require.NoError(t, err)
	assert.Equal(t, schema.Schema("nilschema"), result.Schema())
}

func TestProcessIntrospectionFields(t *testing.T) {
	t.Parallel()

	idx := newIndex(t, "bom", bomBody)

	tests := []struct {
		name  string
		query QueryInput
	}{
		{name: "typename mixed with fields", query: `{ bomResolve(id: "x") { __typename id } }`},
		{name: "schema introspection", query: `{ __schema { types { name } } }`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := Process(idx, tt.query)
			require.NoError(t, err)
			assert.Equal(t, schema.Schema("bom"), result.Schema())
		})
	}
}

func TestProcessMutationUsesMutationRoot(t *testing.T) {
	t.Parallel()

	idx := newIndex(t, "bom", bomBody)
	result, err := Process(idx, `mutation { createGitObjectStatus { id } }`)
	require.NoError(t, err)
	assert.Equal(t, schema.Schema("bom"), result.Schema())
}

func TestProcessNestedSelectionUsesListElementType(t *testing.T) {
	t.Parallel()

	idx := newIndex(t, "bom", `type Query { instances: [Instance!]! }
type Instance { fiName: String }`)
	_, err := Process(idx, `{ instances { fiName } }`)
	require.NoError(t, err)
}

func TestProcessJSONScalarNestedObjectInfersTypes(t *testing.T) {
	t.Parallel()

	idx := newIndex(t, "bom", `scalar JSON
input V1SomeFilter { sitebridgeJson: JSON }
type Query { instances(filter: V1SomeFilter): String }`)
	result, err := Process(idx, `query { instances(filter: { sitebridgeJson: { contains: { enabled: true } } }) }`)
	require.NoError(t, err)
	assert.Equal(t, "Boolean!", result.VariableTypes()["var1"])
}

func TestProcessStripsClientAliasWhenUnnecessary(t *testing.T) {
	t.Parallel()

	idx := newIndex(t, "bom", bomBody)
	result, err := Process(idx, `query { clientAlias: bomResolve(id: "a") { id } }`)
	require.NoError(t, err)
	out := string(result.Query())
	assert.NotContains(t, out, "clientAlias")
	assert.Contains(t, out, "bomResolve")
}

func TestProcessAssignsSequentialAliasesForConflictingSiblings(t *testing.T) {
	t.Parallel()

	idx := newIndex(t, "bom", `type Query { cells(filter: String): String }`)
	result, err := Process(idx, `query { cells(filter: "a") cells(filter: "b") }`)
	require.NoError(t, err)
	out := string(result.Query())
	assert.Contains(t, out, "al1:")
	assert.GreaterOrEqual(t, strings.Count(out, "cells("), 2)
}

func TestProcessClearsBadVariableDeclarations(t *testing.T) {
	t.Parallel()

	idx := newIndex(t, "bom", bomBody)

	tests := []struct {
		name  string
		query QueryInput
	}{
		{name: "duplicate variable name", query: `query ($var1: ID!, $var1: ID!) { bomResolve(id: $var1) { id } }`},
		{name: "unused declared variables", query: `query ($var1: ID!, $var2: Int!) { bomResolve(id: "literal-id") { id } }`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := Process(idx, tt.query)
			require.NoError(t, err)
		})
	}
}

func TestProcessResultExposesIndexAndTypes(t *testing.T) {
	t.Parallel()

	idx := newIndex(t, "bom", bomBody)
	result, err := Process(idx, `{ bomResolve(id: "x") { id name } }`)
	require.NoError(t, err)

	assert.True(t, result.IsNormalized())
	assert.NotEmpty(t, result.Query())
	assert.Equal(t, "ID!", result.VariableTypes()["var1"])

	byName := result.FieldPathsByName()
	assert.Contains(t, byName, "bomResolve")
	assert.Contains(t, byName["id"], "bomResolve.id")
}

func TestFormat(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		raw         QueryInput
		wantErr     error
		wantContain string
	}{
		{name: "empty", raw: "", wantErr: ErrEmptyQuery},
		{name: "invalid", raw: "{ broken", wantErr: ErrQueryParse},
		{name: "collapses whitespace and strips comments", raw: "{\n  # a comment\n  bom {\n    id\n  }\n}", wantContain: "bom"},
		{name: "strips comments across inline fragment and spread", raw: "{\n  a {\n    # c1\n    ... on T {\n      # c2\n      id\n    }\n    ...F\n  }\n}\nfragment F on T { name }", wantContain: "a"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := Format(tt.raw)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Contains(t, string(got), tt.wantContain)
			assert.NotContains(t, string(got), "comment")
		})
	}
}

func TestResultAccessors(t *testing.T) {
	t.Parallel()

	empty := Result{}
	assert.False(t, empty.HasVars())
	assert.False(t, empty.IsNormalized())
	assert.Equal(t, QueryResult(""), empty.Query())
	assert.Equal(t, schema.Schema(""), empty.Schema())
	assert.Nil(t, empty.Variables())
	assert.Empty(t, empty.FieldPathsByName())

	full := Result{
		hasVars:       true,
		index:         fieldIndex{"id": {"bomResolve.id"}},
		normalized:    true,
		query:         "query { bomResolve(id: $id) { id } }",
		schema:        "bom",
		variables:     VariableMap{"id": "123"},
		variableTypes: VariableTypeMap{"id": "ID!"},
	}
	assert.True(t, full.HasVars())
	assert.True(t, full.IsNormalized())
	assert.Equal(t, schema.Schema("bom"), full.Schema())
	assert.Equal(t, VariableMap{"id": "123"}, full.Variables())
	assert.Equal(t, VariableTypeMap{"id": "ID!"}, full.VariableTypes())
	assert.Equal(t, map[string][]string{"id": {"bomResolve.id"}}, full.FieldPathsByName())
}
