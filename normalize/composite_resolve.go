package normalize

import (
	"github.com/vektah/gqlparser/v2/ast"

	"github.com/gomatic/go-graphql/schema"
)

// resolveCompositeIndex narrows a composite (multi-schema) index down to the
// single schema that owns this document's root Query fields. Without that, ArgType
// lookups on the composite index just return the first matching schema (whatever
// map iteration hands back), which gives you wrong variable types and output that
// changes run to run.
//
// When schemaHint is non-empty and the composite index has it, we use that schema
// directly and skip root-field detection (the per-envelope schema from NDJSON replay).
// Otherwise the owning schema is detected from the document's root fields.
func resolveCompositeIndex(idx schema.Index, doc *ast.QueryDocument, schemaHint schema.Schema) (schema.Index, error) {
	c, ok := idx.(*schema.Composite)
	if !ok || c == nil {
		return idx, nil
	}
	if schemaHint != "" {
		if sub := c.ForSchema(schemaHint); sub != nil {
			return sub, nil
		}
	}
	detected, err := c.DetectSchema(rootQueryFieldNamesForSchemaDetect(doc))
	if err != nil {
		return nil, err
	}
	// DetectSchema only ever returns a schema that's in the composite's build
	// order, so ForSchema can't be nil here.
	return c.ForSchema(detected), nil
}

// rootQueryFieldNamesForSchemaDetect gathers root field names across every
// operation. We skip introspection selections (the leading _) so DetectSchema can
// fall back to the primary schema when the document only asks for
// __typename / __schema.
func rootQueryFieldNamesForSchemaDetect(doc *ast.QueryDocument) []schema.FieldNameInput {
	if doc == nil {
		return nil
	}
	var names []schema.FieldNameInput
	for _, op := range doc.Operations {
		names = appendRootFieldNames(names, op.SelectionSet)
	}
	return names
}

// appendRootFieldNames tacks on the non-introspection root field names from set.
func appendRootFieldNames(names []schema.FieldNameInput, set ast.SelectionSet) []schema.FieldNameInput {
	for _, sel := range set {
		f, ok := sel.(*ast.Field)
		if !ok || bool(isIntrospectionField(fieldNameInput(f.Name))) {
			continue
		}
		names = append(names, schema.FieldNameInput(f.Name))
	}
	return names
}
