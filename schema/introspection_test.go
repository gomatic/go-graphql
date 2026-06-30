package schema

import (
	_ "embed"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

//go:embed testdata/sample_introspection.json
var sampleIntrospection []byte

// comprehensiveIntrospection hits every SDL-printing path: objects with
// args/deprecation/descriptions, an implemented interface, a union, enums
// (one of them empty), an input object, a custom scalar, and a custom directive
// (plus a builtin directive that gets excluded).
const comprehensiveIntrospection = `{
  "data": {
    "schema": {
      "query_type": {"name": "Query"},
      "directives": [
        {"name": "skip", "locations": ["FIELD"], "args": []},
        {"name": "custom", "description": "a custom directive", "locations": ["FIELD", "QUERY"], "args": [
          {"name": "level", "description": "the level", "type": {"kind": "SCALAR", "name": "Int"}}
        ]}
      ],
      "types": [
        {"kind": "OBJECT", "name": "__Hidden", "fields": []},
        {"kind": "SCALAR", "name": "String"},
        {"kind": "SCALAR", "name": "DateTime", "description": "a timestamp"},
        {"kind": "OBJECT", "name": "Query", "description": "root query", "interfaces": [{"name": "Node"}], "fields": [
          {"name": "thing", "description": "fetch a thing", "args": [
            {"name": "id", "description": "the id", "type": {"kind": "NON_NULL", "name": null, "of_type": {"kind": "SCALAR", "name": "ID", "of_type": null}}}
          ], "type": {"kind": "OBJECT", "name": "Thing", "of_type": null}},
          {"name": "legacy", "args": [], "type": {"kind": "SCALAR", "name": "String"}, "is_deprecated": true, "deprecation_reason": "use thing"},
          {"name": "gone", "args": [], "type": {"kind": "SCALAR", "name": "String"}, "is_deprecated": true},
          {"name": "list", "args": [], "type": {"kind": "LIST", "name": null, "of_type": {"kind": "SCALAR", "name": "String", "of_type": null}}}
        ]},
        {"kind": "INTERFACE", "name": "Node", "fields": [
          {"name": "id", "args": [], "type": {"kind": "SCALAR", "name": "ID"}},
          {"name": "child", "args": [
            {"name": "depth", "type": {"kind": "SCALAR", "name": "Int"}}
          ], "type": {"kind": "SCALAR", "name": "String"}}
        ]},
        {"kind": "OBJECT", "name": "Thing", "fields": [
          {"name": "id", "args": [], "type": {"kind": "SCALAR", "name": "ID"}}
        ]},
        {"kind": "UNION", "name": "Result", "possible_types": [
          {"kind": "OBJECT", "name": "Thing"}, {"kind": "OBJECT", "name": "Query"}
        ]},
        {"kind": "ENUM", "name": "Color", "enum_values": [
          {"name": "RED", "description": "the red"}, {"name": "BLUE", "is_deprecated": true, "deprecation_reason": "faded"}
        ]},
        {"kind": "ENUM", "name": "Empty", "enum_values": null},
        {"kind": "INPUT_OBJECT", "name": "Filter", "input_fields": [
          {"name": "term", "description": "the term", "type": {"kind": "SCALAR", "name": "String"}}
        ]}
      ]
    }
  }
}`

func TestIntrospectionToSDLErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		wantErr error
		name    string
		data    string
	}{
		{name: "invalid json", data: "{invalid}", wantErr: ErrIntrospectionParse},
		{name: "types not an array", data: `{"data":{"schema":{"types":"oops"}}}`, wantErr: ErrIntrospectionParse},
		{
			name:    "graphql errors",
			data:    `{"errors":[{"message":"boom"}],"data":{"schema":{"types":[]}}}`,
			wantErr: ErrIntrospectionGraphQLErrors,
		},
		{name: "no types", data: `{}`, wantErr: ErrIntrospectionEmpty},
		{
			name:    "union empty propagates",
			data:    `{"data":{"schema":{"types":[{"kind":"UNION","name":"U","possible_types":null}]}}}`,
			wantErr: ErrIntrospectionUnionEmpty,
		},
		{
			name:    "directive arg error propagates",
			data:    `{"data":{"schema":{"directives":[{"name":"bad","locations":["FIELD"],"args":[{"name":"a"}]}],"types":[{"kind":"OBJECT","name":"Query","fields":[{"name":"x","args":[],"type":{"kind":"SCALAR","name":"Boolean"}}]}]}}}`,
			wantErr: ErrIntrospectionSDL,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := IntrospectionToSDL(IntrospectionJSON(tt.data))
			require.ErrorIs(t, err, tt.wantErr)
		})
	}
}

func TestIntrospectionToSDLEmptyArgsDirective(t *testing.T) {
	t.Parallel()

	const data = `{"data":{"schema":{"directives":[{"name":"mycustom","locations":["FIELD"],"args":[]}],"types":[{"kind":"OBJECT","name":"Query","fields":[{"name":"x","args":[],"type":{"kind":"SCALAR","name":"Boolean"}}]}]}}}`
	sdl, err := IntrospectionToSDL(IntrospectionJSON(data))
	require.NoError(t, err)
	assert.Contains(t, string(sdl), "directive @mycustom on FIELD")
}

func TestIntrospectionToSDLComprehensive(t *testing.T) {
	t.Parallel()

	sdl, err := IntrospectionToSDL(IntrospectionJSON(comprehensiveIntrospection))
	require.NoError(t, err)
	s := string(sdl)
	want := assert.New(t)

	want.Contains(s, "directive @custom(")
	want.NotContains(s, "directive @skip")
	want.Contains(s, "type Query implements Node{")
	want.Contains(s, `@deprecated(reason: "use thing")`)
	want.Contains(s, "legacy: String @deprecated")
	want.Contains(s, "gone: String @deprecated\n")
	want.Contains(s, "list: [String]")
	want.Contains(s, "scalar DateTime")
	want.NotContains(s, "scalar String")
	want.NotContains(s, "__Hidden")
	want.Contains(s, "union Result =")
	want.Contains(s, "enum Color {")
	want.Contains(s, "_emptyEnum")
	want.Contains(s, "input Filter {")
	want.Contains(s, "interface Node {")
}

func TestIndexFromIntrospectionComprehensive(t *testing.T) {
	t.Parallel()

	idx, err := IndexFromIntrospection("bom", IntrospectionJSON(comprehensiveIntrospection))
	require.NoError(t, err)
	want := assert.New(t)

	want.Equal(HasFieldResult(true), idx.HasField("Query", "thing"))
	want.Equal(ArgTypeResult("ID!"), idx.ArgType("Query", "thing", "id"))
	want.Equal(ArgTypeResult("Thing"), idx.ArgType("Query", "thing", ""))
	want.Equal(HasFieldResult(true), idx.HasField("Node", "child"))
	want.Equal(HasFieldResult(true), idx.HasField("Filter", "term"))
}

func TestIndexFromIntrospectionError(t *testing.T) {
	t.Parallel()

	_, err := IndexFromIntrospection("bom", IntrospectionJSON("{invalid}"))
	require.ErrorIs(t, err, ErrIntrospectionParse)
}

func TestIndexFromIntrospectionSample(t *testing.T) {
	t.Parallel()

	idx, err := IndexFromIntrospection("bom", sampleIntrospection)
	require.NoError(t, err)
	want := assert.New(t)

	want.Equal(Schema("bom"), idx.Schema())
	want.Equal(HasFieldResult(true), idx.HasField("Query", "bomResolve"))
	want.Equal(HasFieldResult(true), idx.HasField("Query", "bomList"))
	want.Equal(HasFieldResult(false), idx.HasField("Query", "notAField"))
	want.Equal(ArgTypeResult("ID!"), idx.ArgType("Query", "bomResolve", "id"))
	want.Equal(ArgTypeResult("Int"), idx.ArgType("Query", "bomResolve", "version"))
	want.Equal(ArgTypeResult("BomResult"), idx.ArgType("Query", "bomResolve", ""))
	want.Equal(ArgTypeResult("[Bom]"), idx.ArgType("Query", "bomList", ""))
}

func TestInputObjectAliasThroughPipeline(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		rawJSON string
	}{
		{
			name: "camelCase inputFields key",
			rawJSON: `{"data":{"schema":{"types":[
				{"kind":"OBJECT","name":"Query","fields":[{"name":"q","args":[],"type":{"kind":"SCALAR","name":"Boolean"}}]},
				{"kind":"INPUT_OBJECT","name":"Filter","inputFields":[{"name":"cell","type":{"kind":"SCALAR","name":"String"}}]}
			]}}}`,
		},
		{
			name: "snake_case input_fields key",
			rawJSON: `{"data":{"schema":{"types":[
				{"kind":"OBJECT","name":"Query","fields":[{"name":"q","args":[],"type":{"kind":"SCALAR","name":"Boolean"}}]},
				{"kind":"INPUT_OBJECT","name":"Filter","input_fields":[{"name":"cell","type":{"kind":"SCALAR","name":"String"}}]}
			]}}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			idx, err := IndexFromIntrospection("bom", IntrospectionJSON(tt.rawJSON))
			require.NoError(t, err)
			assert.Equal(t, ArgTypeResult("String"), idx.ArgType("Filter", "cell", ""))
		})
	}
}

func TestUnifyIntrospectionEnvelope(t *testing.T) {
	t.Parallel()

	tests := []struct {
		raw           map[string]any
		name          string
		wantHasSchema bool
	}{
		{name: "no data key", raw: map[string]any{"x": 1}, wantHasSchema: false},
		{
			name:          "schema already present",
			raw:           map[string]any{"data": map[string]any{"schema": map[string]any{}}},
			wantHasSchema: true,
		},
		{
			name:          "promote __schema",
			raw:           map[string]any{"data": map[string]any{"__schema": map[string]any{"types": []any{}}}},
			wantHasSchema: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			unifyIntrospectionEnvelope(tt.raw)
			data, ok := tt.raw["data"].(map[string]any)
			if !ok {
				assert.False(t, tt.wantHasSchema)
				return
			}
			_, has := data["schema"]
			assert.Equal(t, tt.wantHasSchema, has)
		})
	}
}

func TestCanonicalizeIntrospectionResponseEarlyReturns(t *testing.T) {
	t.Parallel()

	tests := []struct {
		raw  map[string]any
		name string
	}{
		{name: "no data", raw: map[string]any{}},
		{name: "no schema object", raw: map[string]any{"data": map[string]any{}}},
		{name: "types not array", raw: map[string]any{"data": map[string]any{"schema": map[string]any{"types": "x"}}}},
		{
			name: "type element not map",
			raw:  map[string]any{"data": map[string]any{"schema": map[string]any{"types": []any{"x"}}}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			canonicalizeIntrospectionResponse(tt.raw) // must not panic
			_, err := json.Marshal(tt.raw)
			require.NoError(t, err)
		})
	}
}

func TestCanonicalizeReadsEitherSchemaKey(t *testing.T) {
	t.Parallel()

	for _, key := range []string{"schema", "__schema"} {
		t.Run(key, func(t *testing.T) {
			t.Parallel()
			raw := map[string]any{
				"data": map[string]any{
					key: map[string]any{
						"queryType": map[string]any{"name": "Query"},
						"types": []any{
							map[string]any{
								"kind": "OBJECT",
								"name": "Query",
								"fields": []any{
									map[string]any{
										"name": "x",
										"args": []any{},
										"type": map[string]any{"kind": "SCALAR", "name": "Boolean"},
									},
								},
							},
							map[string]any{
								"kind":          "ENUM",
								"name":          "E",
								"enumValues":    []any{map[string]any{"name": "A"}},
								"possibleTypes": nil,
							},
						},
					},
				},
			}
			canonicalizeIntrospectionResponse(raw)
			schemaObj := raw["data"].(map[string]any)[key].(map[string]any)
			_, hasQT := schemaObj["query_type"]
			assert.True(t, hasQT)
			enumObj := schemaObj["types"].([]any)[1].(map[string]any)
			_, hasEV := enumObj["enum_values"]
			assert.True(t, hasEV)
		})
	}
}

func TestPromoteToSnakeKeepsExistingDst(t *testing.T) {
	t.Parallel()

	m := map[string]any{"of_type": "keep", "ofType": "ignore"}
	promoteToSnake(m, "ofType", "of_type")
	assert.Equal(t, "keep", m["of_type"])
}

func TestCanonicalizeFieldsAndInputValuesSkipNonMaps(t *testing.T) {
	t.Parallel()

	typeObj := map[string]any{
		"fields": []any{
			"not-a-map",
			map[string]any{"name": "f", "type": map[string]any{"ofType": map[string]any{"name": "X"}}},
		},
		"input_fields": []any{
			"not-a-map",
			map[string]any{"name": "i", "type": map[string]any{"kind": "SCALAR", "name": "Y"}},
		},
	}
	canonicalizeIntrospectionTypeMap(typeObj)

	fieldType := typeObj["fields"].([]any)[1].(map[string]any)["type"].(map[string]any)
	_, hasOfType := fieldType["of_type"]
	assert.True(t, hasOfType)
}

func TestCanonicalizeTypeRefMapNonMap(t *testing.T) {
	t.Parallel()

	canonicalizeTypeRefMap("not-a-map") // must not panic
	canonicalizeTypeRefMap(nil)
}
