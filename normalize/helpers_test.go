package normalize

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/parser"

	graphql "github.com/gomatic/go-graphql"
	"github.com/gomatic/go-graphql/schema"
)

// mustParseQuery parses GraphQL operation text, or fails the test trying.
func mustParseQuery(t *testing.T, raw string) *ast.QueryDocument {
	t.Helper()
	doc, err := parser.ParseQuery(&ast.Source{Input: raw})
	require.NoError(t, err)
	return doc
}

// scalarsSDL declares the built-in GraphQL scalars so a schema index built from
// SDL carries them in its type map. Without them, the post-rewrite validator
// rejects variables of built-in types as "Unknown type".
const scalarsSDL = `scalar ID
scalar String
scalar Int
scalar Float
scalar Boolean
`

// newIndex builds a single-schema index from SDL, with the built-in scalars stuck on the front.
func newIndex(t *testing.T, name schema.Schema, body string) schema.Index {
	t.Helper()
	idx, err := schema.NewIndex(name, graphql.SDL(scalarsSDL+body))
	require.NoError(t, err)
	return idx
}

// nilSchemaIndex is a schema.Index whose GraphQLSchema is nil — it stands in for a
// caller-supplied index that doesn't carry a loaded validation schema.
type nilSchemaIndex struct{}

func (nilSchemaIndex) ArgType(schema.TypeNameInput, schema.FieldNameInput, schema.ArgNameInput) schema.ArgTypeResult {
	return ""
}

func (nilSchemaIndex) HasField(schema.TypeNameInput, schema.FieldNameInput) schema.HasFieldResult {
	return true
}
func (nilSchemaIndex) GraphQLSchema() *ast.Schema                                  { return nil }
func (nilSchemaIndex) RootTypeNameForOperation(ast.Operation) schema.TypeNameInput { return "Query" }

func (nilSchemaIndex) Schema() schema.Schema { return "nilschema" }

func TestInferType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value any
		want  variableType
	}{
		{name: "bool", value: true, want: "Boolean!"},
		{name: "int", value: 42, want: "Int!"},
		{name: "int8", value: int8(42), want: "Int!"},
		{name: "int16", value: int16(42), want: "Int!"},
		{name: "int32", value: int32(42), want: "Int!"},
		{name: "int64", value: int64(42), want: "Int!"},
		{name: "uint", value: uint(42), want: "Int!"},
		{name: "uint8", value: uint8(42), want: "Int!"},
		{name: "uint16", value: uint16(42), want: "Int!"},
		{name: "uint32", value: uint32(42), want: "Int!"},
		{name: "uint64", value: uint64(42), want: "Int!"},
		{name: "float32", value: float32(3.14), want: "Float!"},
		{name: "float64", value: float64(3.14), want: "Float!"},
		{name: "string", value: "test", want: "String!"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, inferType(tt.value))
		})
	}
}

func TestInferListType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		want   variableType
		values []any
	}{
		{name: "empty list", values: []any{}, want: "[String!]!"},
		{name: "nil values", values: []any{nil, nil}, want: "[String!]!"},
		{name: "int list", values: []any{int64(1), int64(2)}, want: "[Int!]!"},
		{name: "float list", values: []any{1.5, 2.5}, want: "[Float!]!"},
		{name: "bool list", values: []any{true, false}, want: "[Boolean!]!"},
		{name: "string list", values: []any{"a", "b"}, want: "[String!]!"},
		{name: "leading nil then int", values: []any{nil, int64(3)}, want: "[Int!]!"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, inferListType(tt.values))
		})
	}
}

func TestParseIntValue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  rawStr
		want intResult
	}{
		{name: "positive", raw: "42", want: 42},
		{name: "negative", raw: "-42", want: -42},
		{name: "zero", raw: "0", want: 0},
		{name: "invalid", raw: "abc", want: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, parseIntValue(tt.raw))
		})
	}
}

func TestParseFloatValue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		raw  rawStr
		want floatResult
	}{
		{name: "positive", raw: "3.14", want: 3.14},
		{name: "negative", raw: "-3.14", want: -3.14},
		{name: "zero", raw: "0.0", want: 0.0},
		{name: "integer", raw: "42", want: 42.0},
		{name: "invalid", raw: "abc", want: 0.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, parseFloatValue(tt.raw))
		})
	}
}

func TestParseGraphQLType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		typeStr     varTypeStr
		wantNamed   string
		wantNonNull bool
		wantIsList  bool
	}{
		{name: "simple type", typeStr: "String", wantNamed: "String"},
		{name: "non-null type", typeStr: "String!", wantNonNull: true, wantNamed: "String"},
		{name: "list type", typeStr: "[String]", wantIsList: true},
		{name: "non-null list", typeStr: "[String!]!", wantNonNull: true, wantIsList: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := parseGraphQLType(tt.typeStr)
			assert.Equal(t, tt.wantNonNull, got.NonNull)
			if tt.wantIsList {
				assert.NotNil(t, got.Elem)
				return
			}
			assert.Equal(t, tt.wantNamed, got.NamedType)
		})
	}
}

func TestZeroValueForType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		want  any
		name  string
		sType schemaType
	}{
		{name: "int", sType: "Int!", want: int64(0)},
		{name: "nullable int", sType: "Int", want: int64(0)},
		{name: "float", sType: "Float!", want: float64(0)},
		{name: "nullable float", sType: "Float", want: float64(0)},
		{name: "boolean", sType: "Boolean!", want: false},
		{name: "nullable boolean", sType: "Boolean", want: false},
		{name: "string", sType: "String!", want: ""},
		{name: "id", sType: "ID!", want: ""},
		{name: "custom type", sType: "CustomEnum!", want: ""},
		{name: "list of int", sType: "[Int!]!", want: int64(0)},
		{name: "list of float", sType: "[Float!]!", want: float64(0)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, zeroValueForType(tt.sType))
		})
	}
}

func TestSchemaOrInferVariableType(t *testing.T) {
	t.Parallel()

	t.Run("schema type wins", func(t *testing.T) {
		t.Parallel()
		got, err := schemaOrInferVariableType("ID!", "String!", graphQLTypesFromSchema, "p")
		require.NoError(t, err)
		assert.Equal(t, variableType("ID!"), got)
	})

	t.Run("infer when allowed", func(t *testing.T) {
		t.Parallel()
		got, err := schemaOrInferVariableType("", "String!", graphQLTypesInferredOK, "p")
		require.NoError(t, err)
		assert.Equal(t, variableType("String!"), got)
	})

	t.Run("strict missing type errors", func(t *testing.T) {
		t.Parallel()
		_, err := schemaOrInferVariableType("", "Int!", graphQLTypesFromSchema, "field.arg")
		require.ErrorIs(t, err, ErrGraphQLTypeUnresolved)
	})
}

func TestExtractSingleValue(t *testing.T) {
	t.Parallel()

	tests := []struct {
		want  any
		value *ast.Value
		name  string
	}{
		{name: "int", value: &ast.Value{Kind: ast.IntValue, Raw: "7"}, want: int64(7)},
		{name: "float", value: &ast.Value{Kind: ast.FloatValue, Raw: "1.5"}, want: 1.5},
		{name: "bool true", value: &ast.Value{Kind: ast.BooleanValue, Raw: "true"}, want: true},
		{name: "bool false", value: &ast.Value{Kind: ast.BooleanValue, Raw: "false"}, want: false},
		{name: "null", value: &ast.Value{Kind: ast.NullValue}, want: nil},
		{name: "object", value: &ast.Value{Kind: ast.ObjectValue}, want: map[string]any{"_type": "object"}},
		{name: "list", value: &ast.Value{Kind: ast.ListValue}, want: []any{"_type", "list"}},
		{name: "string default", value: &ast.Value{Kind: ast.StringValue, Raw: "hi"}, want: "hi"},
		{name: "enum default", value: &ast.Value{Kind: ast.EnumValue, Raw: "ACTIVE"}, want: "ACTIVE"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, extractSingleValue(tt.value))
		})
	}
}

func TestExtractListValuesSkipsNilChildren(t *testing.T) {
	t.Parallel()

	children := ast.ChildValueList{
		{Value: nil},
		{Value: &ast.Value{Kind: ast.IntValue, Raw: "1"}},
		{Value: &ast.Value{Kind: ast.StringValue, Raw: "x"}},
	}
	assert.Equal(t, []any{int64(1), "x"}, extractListValues(children))
}

func TestListElementSchemaType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		sType schemaType
		want  schemaType
	}{
		{name: "list non-null", sType: "[String!]!", want: "String!"},
		{name: "list", sType: "[Int]", want: "Int"},
		{name: "non-list", sType: "String", want: ""},
		{name: "empty", sType: "", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, listElementSchemaType(tt.sType))
		})
	}
}

func TestStripAndUnwrapTypes(t *testing.T) {
	t.Parallel()

	assert.Equal(t, graphqlTypeSyntax("X"), stripGraphQLListAndNonNull("[[X!]!]!"))
	assert.Equal(t, schemaType("BomInput"), unwrapToNamedInputType("[BomInput!]!"))
	assert.Equal(t, schema.TypeNameInput("BomResult"), namedTypeForSelectionSetParent("[BomResult!]!"))
	assert.Equal(t, schema.TypeNameInput(""), namedTypeForSelectionSetParent(""))
}

func TestTerminalNamedTypeName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   gqlTypeName
		want namedTypeName
	}{
		{name: "scalar non-null", in: "String!", want: "String"},
		{name: "list strips wrappers", in: "[String!]", want: "String"},
		{name: "nested list strips wrappers", in: "[[Int!]!]", want: "Int"},
		{name: "input", in: "V1AccountFilter", want: "V1AccountFilter"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, terminalNamedTypeName(tt.in))
		})
	}
}

func TestIsOpaqueJSONLikeScalarType(t *testing.T) {
	t.Parallel()

	assert.False(t, bool(isOpaqueJSONLikeScalarType("")))
	assert.True(t, bool(isOpaqueJSONLikeScalarType("JSON")))
	assert.True(t, bool(isOpaqueJSONLikeScalarType("Opaque")))
	assert.False(t, bool(isOpaqueJSONLikeScalarType("BomInput")))
}

func TestBuildFieldPath(t *testing.T) {
	t.Parallel()

	assert.Equal(t, pathStr("id"), buildFieldPath("", "id"))
	assert.Equal(t, pathStr("bom.id"), buildFieldPath("bom", "id"))
}

func TestBuildIndexDeduplicatesAndSkipsEmptySegments(t *testing.T) {
	t.Parallel()

	idx := buildIndex([]fieldPath{"bom.id", "bom.id", "bom.name", ".leading", "x"})

	assert.Equal(t, []fieldPath{"bom.id"}, idx["id"])
	assert.Equal(t, []fieldPath{"bom.id", "bom.name"}, idx["bom"])
	assert.Equal(t, []fieldPath{".leading"}, idx["leading"])
	assert.Equal(t, []fieldPath{"x"}, idx["x"])
}

func TestValidateAgainstGraphQLSchemaNilSchemaSkips(t *testing.T) {
	t.Parallel()

	doc := mustParseQuery(t, `{ anything }`)
	require.NoError(t, validateAgainstGraphQLSchema(nilSchemaIndex{}, doc))
}

func TestRootQueryFieldNamesNilDocument(t *testing.T) {
	t.Parallel()

	assert.Nil(t, rootQueryFieldNamesForSchemaDetect(nil))
}

// rewriteState bundles up the mutable accumulators the low-level normalize helpers
// pass around, so a direct unit test can poke at their guard contracts.
type rewriteState struct {
	vars      VariableMap
	varTypes  varDefs
	canonical varMap
	fields    []fieldPath
	counter   varCounter
}

func newRewriteState() *rewriteState {
	return &rewriteState{vars: VariableMap{}, varTypes: varDefs{}, canonical: varMap{}}
}

func TestNormalizeValueNilValueIsNoOp(t *testing.T) {
	t.Parallel()

	s := newRewriteState()
	err := normalizeValue(nilSchemaIndex{}, "p", nil, s.vars, s.varTypes, &s.fields, &s.counter, s.canonical, "", graphQLTypesFromSchema)
	require.NoError(t, err)
	assert.Empty(t, s.vars)
}

func TestNormalizeScalarValueStrictMissingTypeErrors(t *testing.T) {
	t.Parallel()

	s := newRewriteState()
	err := normalizeScalarValue("p", &ast.Value{Kind: ast.IntValue, Raw: "1"}, s.vars, s.varTypes, &s.fields, &s.counter, "", graphQLTypesFromSchema)
	require.ErrorIs(t, err, ErrGraphQLTypeUnresolved)
}

func TestNormalizeExistingVariableStrictMissingTypeErrors(t *testing.T) {
	t.Parallel()

	s := newRewriteState()
	err := normalizeExistingVariable("p", &ast.Value{Kind: ast.Variable, Raw: "x"}, s.vars, s.varTypes, &s.fields, &s.counter, s.canonical, "", graphQLTypesFromSchema)
	require.ErrorIs(t, err, ErrGraphQLTypeUnresolved)
}

func TestNormalizeListLiteralsStrictMissingTypeErrors(t *testing.T) {
	t.Parallel()

	s := newRewriteState()
	v := &ast.Value{Kind: ast.ListValue, Children: ast.ChildValueList{{Value: &ast.Value{Kind: ast.IntValue, Raw: "1"}}}}
	err := normalizeListLiterals("p", v, s.vars, s.varTypes, &s.fields, &s.counter, "", graphQLTypesFromSchema)
	require.ErrorIs(t, err, ErrGraphQLTypeUnresolved)
}
