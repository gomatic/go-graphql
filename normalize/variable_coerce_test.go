package normalize

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCoerceVariablePlaceholders(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		types     VariableTypeMap
		vars      VariableMap
		key       string
		wantValue any
		checkType string // "map", "nil", "slice", "equal"
	}{
		{
			name:      "input object empty string becomes empty map",
			types:     VariableTypeMap{"var1": "V1AccountFilter!"},
			vars:      VariableMap{"var1": ""},
			key:       "var1",
			checkType: "map",
		},
		{
			name:      "input object nullable becomes nil",
			types:     VariableTypeMap{"var1": "V1AccountFilter"},
			vars:      VariableMap{"var1": ""},
			key:       "var1",
			checkType: "nil",
		},
		{
			name:      "list type becomes empty slice",
			types:     VariableTypeMap{"var1": "[String!]!"},
			vars:      VariableMap{"var1": ""},
			key:       "var1",
			checkType: "slice",
		},
		{
			name:      "string type stays empty string",
			types:     VariableTypeMap{"var1": "String!"},
			vars:      VariableMap{"var1": ""},
			key:       "var1",
			wantValue: "",
			checkType: "equal",
		},
		{
			name:      "missing type leaves value",
			types:     VariableTypeMap{},
			vars:      VariableMap{"var1": "keep"},
			key:       "var1",
			wantValue: "keep",
			checkType: "equal",
		},
		{
			name:      "present value untouched",
			types:     VariableTypeMap{"var1": "V1AccountFilter!"},
			vars:      VariableMap{"var1": "value"},
			key:       "var1",
			wantValue: "value",
			checkType: "equal",
		},
		{
			name:      "nil builtin scalar stays nil",
			types:     VariableTypeMap{"var1": "String!"},
			vars:      VariableMap{"var1": nil},
			key:       "var1",
			checkType: "nil",
		},
		{
			name:      "nil list becomes empty slice",
			types:     VariableTypeMap{"var1": "[String!]!"},
			vars:      VariableMap{"var1": nil},
			key:       "var1",
			checkType: "slice",
		},
		{
			name:      "nil input object nullable stays nil",
			types:     VariableTypeMap{"var1": "V1AccountFilter"},
			vars:      VariableMap{"var1": nil},
			key:       "var1",
			checkType: "nil",
		},
		{
			name:      "nil input type with bang becomes map",
			types:     VariableTypeMap{"var1": "AccountInput!"},
			vars:      VariableMap{"var1": nil},
			key:       "var1",
			checkType: "map",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got := CoerceVariablePlaceholders(tt.vars, tt.types)
			switch tt.checkType {
			case "map":
				m, ok := got[tt.key].(map[string]any)
				require.True(t, ok)
				assert.Empty(t, m)
			case "nil":
				assert.Nil(t, got[tt.key])
			case "slice":
				s, ok := got[tt.key].([]any)
				require.True(t, ok)
				assert.Empty(t, s)
			case "equal":
				assert.Equal(t, tt.wantValue, got[tt.key])
			}
		})
	}
}

func TestCoerceVariablePlaceholdersEdgeCases(t *testing.T) {
	t.Parallel()

	t.Run("empty vars returned as-is", func(t *testing.T) {
		t.Parallel()
		in := VariableMap{}
		assert.Equal(t, in, CoerceVariablePlaceholders(in, VariableTypeMap{"a": "Int"}))
	})

	t.Run("nil types map tolerated", func(t *testing.T) {
		t.Parallel()
		got := CoerceVariablePlaceholders(VariableMap{"var1": ""}, nil)
		assert.Equal(t, "", got["var1"])
	})
}

func TestVariableTypesFromQueryText(t *testing.T) {
	t.Parallel()

	t.Run("parses variable types", func(t *testing.T) {
		t.Parallel()
		types, err := VariableTypesFromQueryText(`query ($var1: V1AccountFilter!, $var2: Int) { __typename }`)
		require.NoError(t, err)
		assert.Equal(t, "V1AccountFilter!", types["var1"])
		assert.Equal(t, "Int", types["var2"])
	})

	t.Run("parse error", func(t *testing.T) {
		t.Parallel()
		_, err := VariableTypesFromQueryText(`query (`)
		require.ErrorIs(t, err, ErrQueryParse)
	})
}

func TestScalarPredicates(t *testing.T) {
	t.Parallel()

	assert.True(t, bool(isBuiltInGraphQLScalarName(scalarInt)))
	assert.False(t, bool(isBuiltInGraphQLScalarName("V1AccountFilter")))
	assert.True(t, bool(isJSONLikeScalarTypeName(scalarJSONB)))
	assert.False(t, bool(isJSONLikeScalarTypeName(scalarString)))
	assert.True(t, bool(shouldCoerceNamedTypeToEmptyObject("AccountOrderBy")))
	assert.True(t, bool(shouldCoerceNamedTypeToEmptyObject("AccountCondition")))
	assert.True(t, bool(shouldCoerceNamedTypeToEmptyObject("AccountPatch")))
	assert.False(t, bool(shouldCoerceNamedTypeToEmptyObject("PlainType")))
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
