package schema

import (
	"github.com/vektah/gqlparser/v2/ast"

	graphql "github.com/gomatic/go-graphql"
)

// fieldSchemaMap maps a root field name to the schema that owns it.
type fieldSchemaMap map[FieldNameInput]Schema

// Composite routes a query's root fields across several schemas and gives you a
// merged [Index] over all of them. You supply the schema names and their
// priority: the first name in the build order is the primary schema — it's the
// default, and it breaks ties when several schemas declare the same root field
// (earliest in order wins).
type Composite struct {
	indexes    map[Schema]Index
	queryField fieldSchemaMap
	primary    Schema
}

// Compile-time check that Composite satisfies Index.
var _ Index = Composite{}

// NewComposite builds a Composite from a priority-ordered list of schema names
// and their SDL. order can't be empty ([ErrNoSchemas]) and every name needs SDL
// in sdls ([ErrSchemaSDLMissing]); invalid SDL comes back as [graphql.ErrParse].
// When several schemas declare the same root field, the one earliest in order
// owns it.
func NewComposite(order []Schema, sdls map[Schema]graphql.SDL) (*Composite, error) {
	if len(order) == 0 {
		return nil, ErrNoSchemas.With(nil)
	}
	c := &Composite{
		indexes:    make(map[Schema]Index, len(order)),
		primary:    order[0],
		queryField: make(fieldSchemaMap),
	}
	for _, s := range order {
		sdl, ok := sdls[s]
		if !ok {
			return nil, ErrSchemaSDLMissing.With(nil, "schema", string(s))
		}
		idx, err := NewIndex(s, sdl)
		if err != nil {
			return nil, err
		}
		c.indexes[s] = idx
		c.buildQueryFieldMap(s, idx)
	}
	return c, nil
}

// buildQueryFieldMap records the root fields (Query, Mutation, Subscription) of
// idx as owned by s, keeping the first (highest-priority) owner of each name.
func (c Composite) buildQueryFieldMap(s Schema, idx Index) {
	g := idx.GraphQLSchema()
	if g == nil {
		return
	}
	c.registerRootFields(s, g.Query)
	c.registerRootFields(s, g.Mutation)
	c.registerRootFields(s, g.Subscription)
}

func (c Composite) registerRootFields(s Schema, def *ast.Definition) {
	if def == nil {
		return
	}
	for _, fld := range def.Fields {
		f := FieldNameInput(fld.Name)
		if _, exists := c.queryField[f]; !exists {
			c.queryField[f] = s
		}
	}
}

// DetectSchema works out which schema handles a query from its root field
// names. With no fields it returns the primary schema. It fails with
// [ErrUnknownField] when no schema owns a field, and [ErrSchemaConflict] when
// the fields are owned by different schemas.
func (c Composite) DetectSchema(fields []FieldNameInput) (Schema, error) {
	if len(fields) == 0 {
		return c.primary, nil
	}
	first, err := c.ownerOf(fields[0])
	if err != nil {
		return "", err
	}
	for _, f := range fields[1:] {
		owner, err := c.ownerOf(f)
		if err != nil {
			return "", err
		}
		if owner != first {
			return "", ErrSchemaConflict.With(nil, "field", string(f), "expected", string(first), "found", string(owner))
		}
	}
	return first, nil
}

func (c Composite) ownerOf(f FieldNameInput) (Schema, error) {
	s, ok := c.queryField[f]
	if !ok {
		return "", ErrUnknownField.With(nil, "field", string(f))
	}
	return s, nil
}

// ForSchema returns the Index for a specific schema, or nil if it's not part of
// the composite.
func (c Composite) ForSchema(s Schema) Index {
	return c.indexes[s]
}

// ArgType implements Index by handing back the first non-empty match across the schemas.
func (c Composite) ArgType(t TypeNameInput, f FieldNameInput, a ArgNameInput) ArgTypeResult {
	for _, idx := range c.indexes {
		if r := idx.ArgType(t, f, a); r != "" {
			return r
		}
	}
	return ""
}

// HasField implements Index by reporting whether any schema has the field.
func (c Composite) HasField(t TypeNameInput, f FieldNameInput) HasFieldResult {
	for _, idx := range c.indexes {
		if idx.HasField(t, f) {
			return true
		}
	}
	return false
}

// Schema implements Index by handing back the primary schema.
func (c Composite) Schema() Schema {
	return c.primary
}

// GraphQLSchema implements Index; composite routing has no single loaded schema, so it's nil.
func (c Composite) GraphQLSchema() *ast.Schema {
	return nil
}

// RootTypeNameForOperation implements Index off the primary schema's root types.
func (c Composite) RootTypeNameForOperation(op ast.Operation) TypeNameInput {
	return c.indexes[c.primary].RootTypeNameForOperation(op)
}
