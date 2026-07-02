package schema

import (
	"github.com/vektah/gqlparser/v2/ast"

	graphql "github.com/gomatic/go-graphql"
)

// Exported types for Index method parameters and return values.
type (
	ArgNameInput   string // input for an argument-name lookup
	ArgTypeResult  string // result of an argument-type lookup (a GraphQL type reference)
	FieldNameInput string // input for a field-name lookup
	HasFieldResult bool   // result of a field-existence check
	TypeNameInput  string // input for a type-name lookup
)

// Index gives you read-only field lookup over a single GraphQL schema.
type Index interface {
	// ArgType returns the type of argument a on field f of type t, or the
	// field's own type when a is empty; "" when there's no match.
	ArgType(t TypeNameInput, f FieldNameInput, a ArgNameInput) ArgTypeResult
	// HasField reports whether type t declares field f.
	HasField(t TypeNameInput, f FieldNameInput) HasFieldResult
	// GraphQLSchema returns the parsed schema underneath (type map and roots).
	GraphQLSchema() *ast.Schema
	// RootTypeNameForOperation returns the root type name for an operation kind.
	RootTypeNameForOperation(op ast.Operation) TypeNameInput
	// Schema returns the schema this index identifies.
	Schema() Schema
}

// Named types for the internal schema-index lookups.
type (
	argName   string // argument name on a field
	fieldName string // field name on a type
	hasField  bool   // result of a field-existence check
	typeName  string // GraphQL type name
)

// typeIndex gives O(1) lookups over a gqlparser-loaded schema.
type typeIndex struct {
	graphql *ast.Schema
	schema  Schema
}

// NewIndex builds an [Index] from SDL text. name labels the source (so parse
// errors carry a useful location) and identifies the schema. We parse the SDL
// with [graphql.Parse] but don't validate it, so a syntactically invalid schema
// fails with [graphql.ErrParse]; a partial schema that would flunk
// cross-reference validation still gives you a usable lookup index.
func NewIndex(name Schema, sdl graphql.SDL) (Index, error) {
	doc, err := graphql.Parse(graphql.Name(name), sdl)
	if err != nil {
		return nil, err
	}
	return newTypeIndexFromAST(name, schemaFromDocument(doc)), nil
}

func newTypeIndexFromAST(s Schema, graphql *ast.Schema) *typeIndex {
	return &typeIndex{graphql: graphql, schema: s}
}

// schemaFromDocument pulls the lookup-relevant subset of an *ast.Schema — the
// type map and the three root operation types — out of a parsed schema document.
// It resolves the roots from an explicit schema{} block, or falls back to the
// conventional type names.
func schemaFromDocument(doc *ast.SchemaDocument) *ast.Schema {
	g := &ast.Schema{Types: make(map[string]*ast.Definition, len(doc.Definitions))}
	for _, def := range doc.Definitions {
		g.Types[def.Name] = def
	}
	g.Query = rootDefinition(g, doc, ast.Query, typeNameQuery)
	g.Mutation = rootDefinition(g, doc, ast.Mutation, typeNameMutation)
	g.Subscription = rootDefinition(g, doc, ast.Subscription, typeNameSubscription)
	return g
}

func rootDefinition(
	g *ast.Schema,
	doc *ast.SchemaDocument,
	op ast.Operation,
	conventional TypeNameInput,
) *ast.Definition {
	if name := operationTypeName(doc, op); name != "" {
		return g.Types[name]
	}
	return g.Types[string(conventional)]
}

func operationTypeName(doc *ast.SchemaDocument, op ast.Operation) string {
	for _, def := range doc.Schema {
		for _, ot := range def.OperationTypes {
			if ot.Operation == op {
				return ot.Type
			}
		}
	}
	return ""
}

// Conventional GraphQL root operation type names, used as defaults when a
// schema doesn't name its roots explicitly.
const (
	typeNameMutation     TypeNameInput = "Mutation"
	typeNameQuery        TypeNameInput = "Query"
	typeNameSubscription TypeNameInput = "Subscription"
)

func defaultRootTypeName(op ast.Operation) TypeNameInput {
	switch op {
	case ast.Mutation:
		return typeNameMutation
	case ast.Subscription:
		return typeNameSubscription
	default:
		return typeNameQuery
	}
}

func rootTypeNameForLoadedSchema(g *ast.Schema, op ast.Operation) TypeNameInput {
	if g == nil {
		return defaultRootTypeName(op)
	}
	if name := namedRoot(g, op); name != "" {
		return name
	}
	if name := definitionName(g.Query); name != "" {
		return name
	}
	return defaultRootTypeName(op)
}

func namedRoot(g *ast.Schema, op ast.Operation) TypeNameInput {
	switch op {
	case ast.Mutation:
		return definitionName(g.Mutation)
	case ast.Subscription:
		return definitionName(g.Subscription)
	default:
		return ""
	}
}

func definitionName(def *ast.Definition) TypeNameInput {
	if def == nil || def.Name == "" {
		return ""
	}
	return TypeNameInput(def.Name)
}

func (idx typeIndex) typeDefinition(name typeName) *ast.Definition {
	if idx.graphql == nil {
		return nil
	}
	return idx.graphql.Types[string(name)]
}

func (idx typeIndex) fieldOnType(t typeName, f fieldName) *ast.FieldDefinition {
	def := idx.typeDefinition(t)
	if def == nil {
		return nil
	}
	for _, field := range def.Fields {
		if field.Name == string(f) {
			return field
		}
	}
	return nil
}

// getSchema returns the schema this index is for.
func (idx typeIndex) getSchema() Schema {
	return idx.schema
}

// hasFieldAt reports whether the type has the given field.
func (idx typeIndex) hasFieldAt(t typeName, f fieldName) hasField {
	return hasField(idx.fieldOnType(t, f) != nil)
}

// getArgType returns the type of argument a on field f of type t when a is
// non-empty, or the field's own type when a is empty. You get "" back when the
// field or argument isn't there.
func (idx typeIndex) getArgType(t typeName, f fieldName, a argName) typeName {
	fld := idx.fieldOnType(t, f)
	if fld == nil {
		return ""
	}
	if a == "" {
		return typeName(fld.Type.String())
	}
	for _, arg := range fld.Arguments {
		if arg.Name == string(a) {
			return typeName(arg.Type.String())
		}
	}
	return ""
}

// Compile-time check that typeIndex satisfies Index.
var _ Index = typeIndex{}

// ArgType implements Index.
func (idx typeIndex) ArgType(t TypeNameInput, f FieldNameInput, a ArgNameInput) ArgTypeResult {
	return ArgTypeResult(idx.getArgType(typeName(t), fieldName(f), argName(a)))
}

// HasField implements Index.
func (idx typeIndex) HasField(t TypeNameInput, f FieldNameInput) HasFieldResult {
	return HasFieldResult(idx.hasFieldAt(typeName(t), fieldName(f)))
}

// Schema implements Index.
func (idx typeIndex) Schema() Schema {
	return idx.getSchema()
}

// GraphQLSchema implements Index.
func (idx typeIndex) GraphQLSchema() *ast.Schema {
	return idx.graphql
}

// RootTypeNameForOperation implements Index.
func (idx typeIndex) RootTypeNameForOperation(op ast.Operation) TypeNameInput {
	return rootTypeNameForLoadedSchema(idx.graphql, op)
}
