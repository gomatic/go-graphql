package schema

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/vektah/gqlparser/v2/ast"
)

func namePtr(s introspectionName) *introspectionName { return &s }

func scalarRef(name introspectionName) *introspectedType {
	return &introspectedType{Kind: "SCALAR", Name: namePtr(name)}
}

func TestIntrospectionTypeToAstType(t *testing.T) {
	t.Parallel()

	tests := []struct {
		wantErr error
		typ     *introspectedType
		name    string
		want    string
	}{
		{name: "nil type", typ: nil, wantErr: ErrIntrospectionNilType},
		{
			name:    "named missing name",
			typ:     &introspectedType{Kind: "SCALAR", Name: nil},
			wantErr: ErrIntrospectionMissingName,
		},
		{
			name:    "named empty name",
			typ:     &introspectedType{Kind: "SCALAR", Name: namePtr("")},
			wantErr: ErrIntrospectionMissingName,
		},
		{name: "named", typ: &introspectedType{Kind: "SCALAR", Name: namePtr("String")}, want: "String"},
		{
			name: "non null",
			typ: &introspectedType{
				Kind:   introspectionKindNonNull,
				OfType: &introspectedType{Kind: "SCALAR", Name: namePtr("ID")},
			},
			want: "ID!",
		},
		{
			name: "list",
			typ: &introspectedType{
				Kind:   introspectionKindList,
				OfType: &introspectedType{Kind: "SCALAR", Name: namePtr("String")},
			},
			want: "[String]",
		},
		{
			name: "unknown wrapper kind unwraps",
			typ:  &introspectedType{Kind: "WEIRD", OfType: &introspectedType{Kind: "SCALAR", Name: namePtr("Int")}},
			want: "Int",
		},
		{
			name: "wrapper inner error",
			typ: &introspectedType{
				Kind:   introspectionKindNonNull,
				OfType: &introspectedType{Kind: "SCALAR", Name: nil},
			},
			wantErr: ErrIntrospectionMissingName,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			got, err := typeStringOrError(tt.typ)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			assert.Equal(t, tt.want, string(got))
		})
	}
}

func TestUnmarshalJSONArrayOrNull(t *testing.T) {
	t.Parallel()

	tests := []struct {
		wantErr error
		name    string
		raw     string
	}{
		{name: "null", raw: "null"},
		{name: "empty", raw: "  "},
		{name: "valid", raw: `[{"name":"X"}]`},
		{name: "invalid", raw: `{not-array}`, wantErr: ErrIntrospectionUnmarshal},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var dest []introspectedEnumValue
			err := unmarshalJSONArrayOrNull(json.RawMessage(tt.raw), &dest, "ctx")
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
		})
	}
}

func TestPrintOneTypeUnsupportedKind(t *testing.T) {
	t.Parallel()

	var sb strings.Builder
	err := printOneType(&sb, introspectionTypeDefinition{Name: "X", Kind: "WIDGET"})
	require.ErrorIs(t, err, ErrIntrospectionUnsupportedKind)
}

// nilTypeField is a field with a nil type reference, which makes typeStringOrError
// fail so the SDL printers surface ErrIntrospectionSDL.
func nilTypeField(name introspectionName) introspectedTypeField {
	return introspectedTypeField{Name: name, Type: nil}
}

func TestSDLPrintersWrapTypeErrors(t *testing.T) {
	t.Parallel()

	badArg := []introspectionInputField{{Name: "a", Type: nil}}

	tests := []struct {
		run  func(sb *strings.Builder) error
		name string
	}{
		{
			name: "object field type",
			run: func(sb *strings.Builder) error {
				return printOneType(
					sb,
					introspectionTypeDefinition{
						Name:   "O",
						Kind:   ast.Object,
						Fields: []introspectedTypeField{nilTypeField("f")},
					},
				)
			},
		},
		{
			name: "object field arg type",
			run: func(sb *strings.Builder) error {
				return printOneType(
					sb,
					introspectionTypeDefinition{
						Name:   "O",
						Kind:   ast.Object,
						Fields: []introspectedTypeField{{Name: "f", Args: badArg, Type: scalarRef("Int")}},
					},
				)
			},
		},
		{
			name: "input field type",
			run: func(sb *strings.Builder) error {
				return printOneType(
					sb,
					introspectionTypeDefinition{
						Name:        "I",
						Kind:        ast.InputObject,
						InputFields: []introspectionInputField{{Name: "x", Type: nil}},
					},
				)
			},
		},
		{
			name: "interface field type",
			run: func(sb *strings.Builder) error {
				return printOneType(
					sb,
					introspectionTypeDefinition{
						Name:   "N",
						Kind:   ast.Interface,
						Fields: []introspectedTypeField{nilTypeField("f")},
					},
				)
			},
		},
		{
			name: "interface field arg type",
			run: func(sb *strings.Builder) error {
				return printOneType(
					sb,
					introspectionTypeDefinition{
						Name:   "N",
						Kind:   ast.Interface,
						Fields: []introspectedTypeField{{Name: "f", Args: badArg, Type: scalarRef("Int")}},
					},
				)
			},
		},
		{
			name: "union member type",
			run: func(sb *strings.Builder) error {
				raw, _ := json.Marshal([]*introspectedType{{Kind: "SCALAR", Name: nil}})
				return printOneType(sb, introspectionTypeDefinition{Name: "U", Kind: ast.Union, PossibleTypes: raw})
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var sb strings.Builder
			require.ErrorIs(t, tt.run(&sb), ErrIntrospectionSDL)
		})
	}
}

func TestPrintDirectivePropagatesArgTypeError(t *testing.T) {
	t.Parallel()

	var sb strings.Builder
	err := printDirectives(&sb, []introspectionDirectiveDefinition{
		{
			Name:      "d",
			Args:      []introspectionDirectiveArg{{Name: "a", Type: nil}},
			Locations: []ast.DirectiveLocation{"FIELD"},
		},
	})
	require.ErrorIs(t, err, ErrIntrospectionSDL)
}

func TestPrintUnionMalformedPossibleTypes(t *testing.T) {
	t.Parallel()

	var sb strings.Builder
	err := printOneType(
		&sb,
		introspectionTypeDefinition{Name: "U", Kind: ast.Union, PossibleTypes: json.RawMessage(`{bad}`)},
	)
	require.ErrorIs(t, err, ErrIntrospectionUnmarshal)
}

func TestPrintEnumMalformedEnumValues(t *testing.T) {
	t.Parallel()

	var sb strings.Builder
	err := printOneType(
		&sb,
		introspectionTypeDefinition{Name: "E", Kind: ast.Enum, EnumValues: json.RawMessage(`{bad}`)},
	)
	require.ErrorIs(t, err, ErrIntrospectionUnmarshal)
}

func TestPrintDeprecation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		reason any
		name   string
		want   string
		flag   deprecatedFlag
	}{
		{name: "not deprecated", flag: false, reason: "x", want: ""},
		{name: "deprecated no reason", flag: true, reason: nil, want: " @deprecated"},
		{name: "deprecated empty reason", flag: true, reason: "", want: " @deprecated"},
		{name: "deprecated with reason", flag: true, reason: "gone", want: ` @deprecated(reason: "gone")`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var sb strings.Builder
			printDeprecation(&sb, tt.flag, tt.reason)
			assert.Equal(t, tt.want, sb.String())
		})
	}
}
