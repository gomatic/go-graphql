package normalize

import "github.com/gomatic/go-graphql/schema"

// The types behind Result's fields and what its accessors hand back.
type (
	fieldIndex      map[fieldName][]fieldPath // field name to every path that contains that field
	fieldName       string                    // a field name we look up in the index
	QueryResult     string                    // GraphQL query text
	VariableMap     map[string]any            // variable name to value
	variableType    string                    // a variable's GraphQL type, like "String!" or "Int!"
	VariableTypeMap map[string]string         // variable name to its GraphQL type string
)

// Result holds a normalized GraphQL query along with its metadata.
type Result struct {
	index         fieldIndex
	variables     VariableMap
	variableTypes VariableTypeMap
	query         QueryResult
	schema        schema.Schema
	hasVars       bool
	normalized    bool
}

// HasVars reports whether the query has any variables.
func (r Result) HasVars() bool {
	return r.hasVars
}

// FieldPathsByName returns, for each field name, every dot-separated path in the
// query that contains that field.
func (r Result) FieldPathsByName() map[string][]string {
	out := make(map[string][]string, len(r.index))
	for fn, paths := range r.index {
		ps := make([]string, len(paths))
		for i, p := range paths {
			ps[i] = string(p)
		}
		out[string(fn)] = ps
	}
	return out
}

// IsNormalized reports whether we actually normalized the query.
func (r Result) IsNormalized() bool {
	return r.normalized
}

// Query returns the normalized query text.
func (r Result) Query() QueryResult {
	return r.query
}

// Schema returns the schema this query targets.
func (r Result) Schema() schema.Schema {
	return r.schema
}

// Variables returns the variable values we pulled out.
func (r Result) Variables() VariableMap {
	return r.variables
}

// VariableTypes returns the variable name to GraphQL type mapping.
func (r Result) VariableTypes() VariableTypeMap {
	return r.variableTypes
}
