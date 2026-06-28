package normalize

import (
	"strings"

	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/parser"
)

// The named types we use for variable coercion.
type (
	gqlTypeName   string // a full GraphQL type string, wrappers and all
	isBuiltIn     bool   // whether a type is a built-in GraphQL scalar
	isCoercible   bool   // whether a nil value can be coerced for a type
	isJSONLike    bool   // whether a type is a JSON-like scalar
	namedTypeName string // the terminal named type once the wrappers are stripped
)

const (
	scalarBoolean namedTypeName = "Boolean"
	scalarFloat   namedTypeName = "Float"
	scalarID      namedTypeName = "ID"
	scalarInt     namedTypeName = "Int"
	scalarString  namedTypeName = "String"
)

const (
	scalarJSON       namedTypeName = "JSON"
	scalarJSONB      namedTypeName = "JSONB"
	scalarJSONObject namedTypeName = "JSONObject"
	scalarOpaque     namedTypeName = "Opaque"
)

// VariableTypesFromQueryText parses the document and gives you back each operation
// variable's GraphQL type string (so "V1AccountFilter!", "[String!]!").
func VariableTypesFromQueryText(raw QueryInput) (VariableTypeMap, error) {
	doc, err := parser.ParseQuery(&ast.Source{Input: string(raw)})
	if err != nil {
		return nil, ErrQueryParse.With(err)
	}
	return variableTypesFromDocument(doc), nil
}

func variableTypesFromDocument(doc *ast.QueryDocument) VariableTypeMap {
	out := make(VariableTypeMap)
	for _, op := range doc.Operations {
		for _, def := range op.VariableDefinitions {
			out[def.Variable] = def.Type.String()
		}
	}
	return out
}

// CoerceVariablePlaceholders rewrites empty-string (and nil, when it's safe)
// variable values so replay against GraphQL doesn't send "" for input objects or
// lists. It leans on the declared types from VariableTypesFromQueryText or
// normalize Result.VariableTypes().
func CoerceVariablePlaceholders(vars VariableMap, types VariableTypeMap) VariableMap {
	if len(vars) == 0 {
		return vars
	}
	out := make(VariableMap, len(vars))
	for k, v := range vars {
		t := ""
		if types != nil {
			t = types[k]
		}
		out[k] = coerceSingleVariableValue(gqlTypeName(t), v)
	}
	return out
}

func coerceSingleVariableValue(gqlType gqlTypeName, v any) any {
	gqlType = gqlTypeName(strings.TrimSpace(string(gqlType)))
	if !shouldCoerceVariable(gqlType, v) {
		return v
	}
	return coercedEmptyValue(gqlType, v)
}

// shouldCoerceVariable reports whether an empty-or-nil variable value should get
// swapped for a placeholder that suits its type.
func shouldCoerceVariable(gqlType gqlTypeName, v any) bool {
	if gqlType == "" {
		return false
	}
	emptyStr := isEmptyString(v)
	isNil := v == nil
	if !emptyStr && !isNil {
		return false
	}
	if isNil && !bool(coerceNilAllowedForGraphQLType(gqlType)) {
		return false
	}
	return true
}

// coercedEmptyValue returns the placeholder for an empty-or-nil value of gqlType,
// leaving built-in scalars and shapes we don't recognize as the original v.
func coercedEmptyValue(gqlType gqlTypeName, v any) any {
	if strings.HasPrefix(string(gqlType), "[") {
		return []any{}
	}
	term := terminalNamedTypeName(gqlType)
	if !bool(coercesToEmptyObject(term)) {
		return v
	}
	if strings.HasSuffix(string(gqlType), "!") {
		return map[string]any{}
	}
	return nil
}

// isEmptyString reports whether v is an empty string.
func isEmptyString(v any) bool {
	s, ok := v.(string)
	return ok && s == ""
}

func coerceNilAllowedForGraphQLType(gqlType gqlTypeName) isCoercible {
	if strings.HasPrefix(strings.TrimSpace(string(gqlType)), "[") {
		return true
	}
	term := terminalNamedTypeName(gqlType)
	if bool(isBuiltInGraphQLScalarName(term)) {
		return false
	}
	return coercesToEmptyObject(term)
}

// coercesToEmptyObject reports whether a named type should turn into an empty
// object (the JSON-like scalars and the input/filter shapes).
func coercesToEmptyObject(name namedTypeName) isCoercible {
	return isCoercible(bool(isJSONLikeScalarTypeName(name)) || bool(shouldCoerceNamedTypeToEmptyObject(name)))
}

func isBuiltInGraphQLScalarName(name namedTypeName) isBuiltIn {
	switch name {
	case scalarInt, scalarFloat, scalarString, scalarBoolean, scalarID:
		return true
	default:
		return false
	}
}

func isJSONLikeScalarTypeName(name namedTypeName) isJSONLike {
	switch name {
	case scalarJSON, scalarJSONObject, scalarJSONB, scalarOpaque:
		return true
	default:
		return false
	}
}

func shouldCoerceNamedTypeToEmptyObject(name namedTypeName) isCoercible {
	return isCoercible(strings.HasSuffix(string(name), "Filter") ||
		strings.HasSuffix(string(name), "Input") ||
		strings.HasSuffix(string(name), "Condition") ||
		strings.HasSuffix(string(name), "Patch") ||
		strings.HasSuffix(string(name), "OrderBy"))
}

func terminalNamedTypeName(graphqlType gqlTypeName) namedTypeName {
	t := strings.TrimSpace(string(graphqlType))
	for strings.HasPrefix(t, "[") && strings.HasSuffix(t, "]") {
		t = strings.TrimSpace(t[1 : len(t)-1])
		t = strings.TrimSuffix(strings.TrimSpace(t), "!")
		t = strings.TrimSpace(t)
	}
	t = strings.TrimSuffix(strings.TrimSpace(t), "!")
	return namedTypeName(strings.TrimSpace(t))
}
