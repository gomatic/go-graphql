// Package normalize parses GraphQL operation text with gqlparser, checks the
// fields are good to rewrite, pulls inline literals out into variables (and
// rebuilds the operation's variable definitions), then runs the result back
// through [github.com/vektah/gqlparser/v2/validator] when the schema index hands
// us a loaded *ast.Schema. We validate after the rewrite so duplicate or unused
// client variable declarations don't trip validation — we drop them and emit one
// clean definition list.
//
// It sits on top of the root graphql package (SDL parsing) and the schema package
// (field-lookup indexes and composite routing), and works purely on in-memory
// inputs with no shared mutable state.
package normalize

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/formatter"
	"github.com/vektah/gqlparser/v2/parser"

	"github.com/gomatic/go-graphql/schema"
)

// The internal types normalize works with.
type (
	baseTypeName             string                  // a base GraphQL type name (Int, Float, Boolean, String, ID)
	fieldNameInput           string                  // a field name straight from the AST, for the introspection check
	fieldPath                string                  // a dot-separated field path, like "bomResolve.id"
	fieldPrefix              string                  // the prefix we build field paths from
	floatResult              float64                 // a parsed float
	formattedQuery           string                  // a minimal-whitespace query string
	graphqlTypeSyntax        string                  // a GraphQL type spelling, LIST and NON_NULL wrappers and all
	inferMissingGraphQLTypes bool                    // whether literals/vars may use inferred types when the schema's silent
	intResult                int64                   // a parsed int
	isIntrospection          bool                    // the answer to an introspection-field check
	pathStr                  string                  // a path string used while normalizing a value
	QueryInput               string                  // raw GraphQL query text to normalize
	rawStr                   string                  // a raw string value from the AST
	schemaType               string                  // a type we got back from a schema lookup
	varCounter               int                     // the counter we use to mint unique variable names
	varDefs                  map[string]variableType // variable name to its GraphQL type
	varMap                   map[string]string       // original variable name to its canonical name
	varTypeStr               string                  // a GraphQL type string we're about to parse
)

const (
	graphQLTypesFromSchema inferMissingGraphQLTypes = false // strict — the schema has to tell us
	graphQLTypesInferredOK inferMissingGraphQLTypes = true  // introspection / unknown meta-fields, so we can guess
)

// The base GraphQL type names.
const (
	baseTypeBoolean baseTypeName = "Boolean"
	baseTypeFloat   baseTypeName = "Float"
	baseTypeInt     baseTypeName = "Int"
)

// Raw AST spellings and the placeholders we use when extracting lists.
const (
	rawTrue                = "true"
	placeholderTypeKey     = "_type"
	placeholderListValue   = "list"
	placeholderObjectValue = "object"
)

// The inferred non-null GraphQL scalar spellings we fall back to when the schema
// can't pin down a concrete type.
const (
	gqlBooleanNonNull variableType = "Boolean!"
	gqlFloatNonNull   variableType = "Float!"
	gqlIntNonNull     variableType = "Int!"
	gqlStringNonNull  variableType = "String!"
)

var reSpaces = regexp.MustCompile(`([^a-zA-Z0-9_])\s+([^a-zA-Z0-9_])`)

// Process parses, validates, and normalizes a GraphQL query. It lifts inline
// literals into variables and makes sure every field belongs to a single schema.
func Process(idx schema.Index, raw QueryInput) (Result, error) {
	return process(idx, raw, "")
}

// ProcessWithSchemaHint does what Process does, but for a composite index it
// prefers the schema you hand it when that schema exists (say, a schema field
// off a JSON request envelope).
func ProcessWithSchemaHint(idx schema.Index, raw QueryInput, hint schema.Schema) (Result, error) {
	return process(idx, raw, hint)
}

func process(idx schema.Index, raw QueryInput, schemaHint schema.Schema) (Result, error) {
	doc, idx, err := prepare(idx, raw, schemaHint)
	if err != nil {
		return Result{}, err
	}
	if err = validateFields(idx, doc); err != nil {
		return Result{}, err
	}
	vars, varTypes, fields, err := extractAndRewrite(idx, doc)
	if err != nil {
		return Result{}, err
	}
	// Validate against the spec after the rewrite: we've cleared and rebuilt
	// VariableDefinitions, which fixes up duplicate names and unused declarations.
	if err = validateAgainstGraphQLSchema(idx, doc); err != nil {
		return Result{}, err
	}
	return buildResult(idx.Schema(), formatMinimal(doc), vars, varTypes, fields), nil
}

// prepare checks the raw input, parses it, and narrows a composite index down to
// the single schema that owns the document's root fields.
func prepare(idx schema.Index, raw QueryInput, schemaHint schema.Schema) (*ast.QueryDocument, schema.Index, error) {
	if raw == "" {
		return nil, nil, ErrEmptyQuery
	}
	doc, err := parser.ParseQuery(&ast.Source{Input: string(raw)})
	if err != nil {
		return nil, nil, ErrQueryParse.With(err)
	}
	resolved, err := resolveCompositeIndex(idx, doc, schemaHint)
	if err != nil {
		return nil, nil, err
	}
	return doc, resolved, nil
}

// buildResult stitches the rewrite outputs together into a normalized Result.
func buildResult(s schema.Schema, normalized formattedQuery, vars VariableMap, varTypes varDefs, fields []fieldPath) Result {
	vtm := make(VariableTypeMap, len(varTypes))
	for name, vt := range varTypes {
		vtm[name] = string(vt)
	}
	return Result{
		hasVars:       len(vars) > 0,
		index:         buildIndex(fields),
		normalized:    true,
		query:         QueryResult(normalized),
		schema:        s,
		variables:     vars,
		variableTypes: vtm,
	}
}

// validateFields makes sure every field in the query actually exists in the schema.
func validateFields(idx schema.Index, doc *ast.QueryDocument) error {
	for _, op := range doc.Operations {
		root := idx.RootTypeNameForOperation(op.Operation)
		if err := validateSelectionSet(idx, root, op.SelectionSet); err != nil {
			return err
		}
	}
	return nil
}

// validateSelectionSet walks a selection set and validates every field in it.
func validateSelectionSet(idx schema.Index, parentType schema.TypeNameInput, set ast.SelectionSet) error {
	for _, sel := range set {
		if err := validateSelection(idx, parentType, sel); err != nil {
			return err
		}
	}
	return nil
}

// validateSelection validates one selection item.
func validateSelection(idx schema.Index, parentType schema.TypeNameInput, sel ast.Selection) error {
	switch s := sel.(type) {
	case *ast.Field:
		return validateField(idx, parentType, s)
	case *ast.InlineFragment:
		return validateSelectionSet(idx, parentType, s.SelectionSet)
	default:
		return nil // we validate fragment spreads separately
	}
}

// validateField validates a field along with anything nested under it.
func validateField(idx schema.Index, parentType schema.TypeNameInput, field *ast.Field) error {
	if bool(isIntrospectionField(fieldNameInput(field.Name))) {
		return nil
	}
	fieldInput := schema.FieldNameInput(field.Name)
	if !idx.HasField(parentType, fieldInput) {
		return schema.ErrUnknownField.With(nil, keyType, string(parentType), keyField, field.Name)
	}
	if len(field.SelectionSet) == 0 {
		return nil
	}
	returnType := idx.ArgType(parentType, fieldInput, "")
	return validateSelectionSet(idx, namedTypeForSelectionSetParent(returnType), field.SelectionSet)
}

// isIntrospectionField reports whether a field name is an introspection field.
func isIntrospectionField(name fieldNameInput) isIntrospection {
	return isIntrospection(len(name) > 0 && name[0] == '_')
}

// extractAndRewrite pulls inline literals out, rewrites the AST, and gathers up the field paths.
func extractAndRewrite(idx schema.Index, doc *ast.QueryDocument) (VariableMap, varDefs, []fieldPath, error) {
	vars := make(VariableMap)
	varTypes := make(varDefs)
	var fields []fieldPath
	var counter varCounter
	var aliasSeqVal int
	aliasSeq := aliasSequence(&aliasSeqVal)
	canonicalNames := make(varMap)

	for _, op := range doc.Operations {
		if err := normalizeOperation(idx, op, vars, varTypes, &fields, &counter, canonicalNames, aliasSeq); err != nil {
			return nil, nil, nil, err
		}
	}
	return vars, varTypes, fields, nil
}

// Format parses query text and gives you back minimal-whitespace formatting,
// skipping variable extraction. It drops comments and collapses whitespace.
func Format(raw QueryInput) (QueryResult, error) {
	if raw == "" {
		return "", ErrEmptyQuery
	}
	doc, err := parser.ParseQuery(&ast.Source{Input: string(raw)})
	if err != nil {
		return "", ErrQueryParse.With(err)
	}
	stripComments(doc)
	return QueryResult(formatMinimal(doc)), nil
}

func removeCommentsFromSelectionSet(set ast.SelectionSet) {
	for _, selection := range set {
		clearSelectionComments(selection)
	}
}

func clearSelectionComments(selection ast.Selection) {
	switch sel := selection.(type) {
	case *ast.Field:
		sel.Comment = nil
		removeCommentsFromSelectionSet(sel.SelectionSet)
	case *ast.InlineFragment:
		sel.Comment = nil
		removeCommentsFromSelectionSet(sel.SelectionSet)
	case *ast.FragmentSpread:
		sel.Comment = nil
	}
}

// normalizeOperation normalizes one operation.
func normalizeOperation(idx schema.Index, op *ast.OperationDefinition, vars VariableMap, varTypes varDefs, fields *[]fieldPath, counter *varCounter, canonicalNames varMap, aliasSeq aliasSequence) error {
	parentType := idx.RootTypeNameForOperation(op.Operation)
	op.Name = ""
	op.VariableDefinitions = nil
	op.Directives = nil
	op.Comment = nil

	if err := normalizeSelectionSet(idx, parentType, "", op.SelectionSet, vars, varTypes, fields, counter, canonicalNames, aliasSeq); err != nil {
		return err
	}
	op.VariableDefinitions = buildVariableDefinitions(varTypes)
	return nil
}

// buildVariableDefinitions turns the types we collected into AST variable definitions.
func buildVariableDefinitions(varTypes varDefs) ast.VariableDefinitionList {
	if len(varTypes) == 0 {
		return nil
	}
	names := make([]string, 0, len(varTypes))
	for name := range varTypes {
		names = append(names, name)
	}
	sort.Strings(names)

	defs := make(ast.VariableDefinitionList, 0, len(names))
	for _, name := range names {
		defs = append(defs, &ast.VariableDefinition{
			Variable: name,
			Type:     parseGraphQLType(varTypeStr(varTypes[name])),
		})
	}
	return defs
}

// parseGraphQLType turns a GraphQL type string into an AST Type.
func parseGraphQLType(ts varTypeStr) *ast.Type {
	t := &ast.Type{}
	typeStr := string(ts)
	if strings.HasSuffix(typeStr, "!") {
		t.NonNull = true
		typeStr = strings.TrimSuffix(typeStr, "!")
	}
	if strings.HasPrefix(typeStr, "[") && strings.HasSuffix(typeStr, "]") {
		t.Elem = parseGraphQLType(varTypeStr(typeStr[1 : len(typeStr)-1]))
		return t
	}
	t.NamedType = typeStr
	return t
}

// normalizeSelectionSet normalizes a selection set and sorts it.
func normalizeSelectionSet(idx schema.Index, parentType schema.TypeNameInput, prefix fieldPrefix, set ast.SelectionSet, vars VariableMap, varTypes varDefs, fields *[]fieldPath, counter *varCounter, canonicalNames varMap, aliasSeq aliasSequence) error {
	sortFieldSelections(set)
	for _, sel := range set {
		if err := normalizeSelection(idx, parentType, prefix, sel, vars, varTypes, fields, counter, canonicalNames, aliasSeq); err != nil {
			return err
		}
	}
	// Once literals are variables, hand out alN aliases only where sibling fields
	// would otherwise break GraphQL's merge rules (same name, args that won't merge).
	assignMergeAliasesForSelectionSet(set, aliasSeq)
	return nil
}

// sortFieldSelections sorts field selections by name so the output is deterministic.
func sortFieldSelections(set ast.SelectionSet) {
	sort.Slice(set, func(i, j int) bool {
		fi, ok1 := set[i].(*ast.Field)
		fj, ok2 := set[j].(*ast.Field)
		if !ok1 || !ok2 {
			return false
		}
		return fi.Name < fj.Name
	})
}

// normalizeSelection normalizes one selection item.
func normalizeSelection(idx schema.Index, parentType schema.TypeNameInput, prefix fieldPrefix, sel ast.Selection, vars VariableMap, varTypes varDefs, fields *[]fieldPath, counter *varCounter, canonicalNames varMap, aliasSeq aliasSequence) error {
	switch s := sel.(type) {
	case *ast.Field:
		return normalizeField(idx, parentType, prefix, s, vars, varTypes, fields, counter, canonicalNames, aliasSeq)
	case *ast.InlineFragment:
		s.Directives = nil
		s.Comment = nil
		return normalizeSelectionSet(idx, parentType, prefix, s.SelectionSet, vars, varTypes, fields, counter, canonicalNames, aliasSeq)
	case *ast.FragmentSpread:
		s.Directives = nil
		s.Comment = nil
	}
	return nil
}

// normalizeField normalizes a field, its arguments, and whatever's nested under it.
func normalizeField(idx schema.Index, parentType schema.TypeNameInput, prefix fieldPrefix, field *ast.Field, vars VariableMap, varTypes varDefs, fields *[]fieldPath, counter *varCounter, canonicalNames varMap, aliasSeq aliasSequence) error {
	path := buildFieldPath(prefix, fieldNameInput(field.Name))
	field.Directives = nil
	field.Comment = nil
	field.ObjectDefinition = nil

	inferMissing := inferMissingGraphQLTypes(bool(isIntrospectionField(fieldNameInput(field.Name))))
	if err := normalizeArguments(idx, parentType, schema.FieldNameInput(field.Name), path, field.Arguments, vars, varTypes, fields, counter, canonicalNames, inferMissing); err != nil {
		return err
	}
	if len(field.SelectionSet) > 0 {
		return normalizeFieldSelections(idx, parentType, path, field, vars, varTypes, fields, counter, canonicalNames, aliasSeq)
	}
	*fields = append(*fields, fieldPath(path))
	return nil
}

// normalizeFieldSelections normalizes a field's nested selection set against that
// field's return type, with the wrappers peeled off.
func normalizeFieldSelections(idx schema.Index, parentType schema.TypeNameInput, path pathStr, field *ast.Field, vars VariableMap, varTypes varDefs, fields *[]fieldPath, counter *varCounter, canonicalNames varMap, aliasSeq aliasSequence) error {
	returnType := idx.ArgType(parentType, schema.FieldNameInput(field.Name), "")
	childType := namedTypeForSelectionSetParent(returnType)
	return normalizeSelectionSet(idx, childType, fieldPrefix(path), field.SelectionSet, vars, varTypes, fields, counter, canonicalNames, aliasSeq)
}

// buildFieldPath builds up a dot-separated field path.
func buildFieldPath(prefix fieldPrefix, name fieldNameInput) pathStr {
	if prefix == "" {
		return pathStr(name)
	}
	return pathStr(string(prefix) + "." + string(name))
}

// normalizeArguments sorts and normalizes a field's arguments, leaning on the schema to infer types.
func normalizeArguments(idx schema.Index, parentType schema.TypeNameInput, fieldName schema.FieldNameInput, path pathStr, args ast.ArgumentList, vars VariableMap, varTypes varDefs, fields *[]fieldPath, counter *varCounter, canonicalNames varMap, inferMissing inferMissingGraphQLTypes) error {
	sort.Slice(args, func(i, j int) bool {
		return args[i].Name < args[j].Name
	})
	for _, arg := range args {
		if err := normalizeArgument(idx, parentType, fieldName, path, arg, vars, varTypes, fields, counter, canonicalNames, inferMissing); err != nil {
			return err
		}
	}
	return nil
}

// normalizeArgument normalizes one argument's value.
func normalizeArgument(idx schema.Index, parentType schema.TypeNameInput, fieldName schema.FieldNameInput, path pathStr, arg *ast.Argument, vars VariableMap, varTypes varDefs, fields *[]fieldPath, counter *varCounter, canonicalNames varMap, inferMissing inferMissingGraphQLTypes) error {
	argPath := pathStr(string(path) + "." + arg.Name)
	argType := schemaType(idx.ArgType(parentType, fieldName, schema.ArgNameInput(arg.Name)))
	if argType == "" && inferMissing == graphQLTypesFromSchema {
		return ErrGraphQLTypeUnresolved.With(nil, keyParentType, string(parentType), keyField, string(fieldName), keyArgument, arg.Name)
	}
	return normalizeValue(idx, argPath, arg.Value, vars, varTypes, fields, counter, canonicalNames, argType, inferMissing)
}

// normalizeValue normalizes a value, turning literals into variables. When
// inferMissing is graphQLTypesFromSchema, every value position has to come with a
// schema type — that's the strict path.
func normalizeValue(idx schema.Index, path pathStr, value *ast.Value, vars VariableMap, varTypes varDefs, fields *[]fieldPath, counter *varCounter, canonicalNames varMap, sType schemaType, inferMissing inferMissingGraphQLTypes) error {
	if value == nil {
		return nil
	}
	switch value.Kind {
	case ast.Variable:
		return normalizeExistingVariable(path, value, vars, varTypes, fields, counter, canonicalNames, sType, inferMissing)
	case ast.ListValue:
		return normalizeListValue(idx, path, value, vars, varTypes, fields, counter, canonicalNames, sType, inferMissing)
	case ast.ObjectValue:
		return normalizeObjectValue(idx, path, value, vars, varTypes, fields, counter, canonicalNames, sType, inferMissing)
	case ast.NullValue:
		*fields = append(*fields, fieldPath(path))
		return nil
	default:
		return normalizeScalarValue(path, value, vars, varTypes, fields, counter, sType, inferMissing)
	}
}

// normalizeScalarValue swaps a scalar literal (Int, Float, Boolean, String,
// Block, Enum) for a generated variable.
func normalizeScalarValue(path pathStr, value *ast.Value, vars VariableMap, varTypes varDefs, fields *[]fieldPath, counter *varCounter, sType schemaType, inferMissing inferMissingGraphQLTypes) error {
	inferred, goValue := scalarInferredTypeAndValue(value)
	varType, err := schemaOrInferVariableType(sType, inferred, inferMissing, path)
	if err != nil {
		return err
	}
	replaceWithVariable(path, value, vars, varTypes, fields, counter, goValue, varType)
	return nil
}

// scalarInferredTypeAndValue maps a scalar literal to its inferred GraphQL type
// and the Go value pulled out of it.
func scalarInferredTypeAndValue(value *ast.Value) (variableType, any) {
	switch value.Kind {
	case ast.IntValue:
		return gqlIntNonNull, int64(parseIntValue(rawStr(value.Raw)))
	case ast.FloatValue:
		return gqlFloatNonNull, float64(parseFloatValue(rawStr(value.Raw)))
	case ast.BooleanValue:
		return gqlBooleanNonNull, value.Raw == rawTrue
	default: // StringValue, BlockValue, EnumValue
		return gqlStringNonNull, value.Raw
	}
}

// schemaOrInferVariableType insists on a schema type unless inferMissing says we can infer one.
func schemaOrInferVariableType(sType schemaType, inferred variableType, inferMissing inferMissingGraphQLTypes, path pathStr) (variableType, error) {
	if sType != "" {
		return variableType(sType), nil
	}
	if inferMissing == graphQLTypesInferredOK {
		return inferred, nil
	}
	return "", ErrGraphQLTypeUnresolved.With(nil, keyPath, string(path), keyReason, "missing schema type for value")
}

// unwrapToNamedInputType peels off GraphQL list/non-null wrappers so we can look up an input-object field.
func unwrapToNamedInputType(sType schemaType) schemaType {
	return schemaType(stripGraphQLListAndNonNull(graphqlTypeSyntax(sType)))
}

// stripGraphQLListAndNonNull keeps peeling outer NON_NULL and LIST wrappers until only a named type's left.
func stripGraphQLListAndNonNull(s graphqlTypeSyntax) graphqlTypeSyntax {
	ts := strings.TrimSpace(string(s))
	for ts != "" {
		ts = strings.TrimSuffix(ts, "!")
		if len(ts) >= 2 && ts[0] == '[' && ts[len(ts)-1] == ']' {
			ts = strings.TrimSpace(ts[1 : len(ts)-1])
			continue
		}
		break
	}
	return graphqlTypeSyntax(ts)
}

// namedTypeForSelectionSetParent is the parent type GraphQL uses for a field's
// selection set: we peel off LIST and NON_NULL wrappers so lookups hit the
// underlying OBJECT / INTERFACE name.
func namedTypeForSelectionSetParent(rt schema.ArgTypeResult) schema.TypeNameInput {
	if rt == "" {
		return ""
	}
	return schema.TypeNameInput(stripGraphQLListAndNonNull(graphqlTypeSyntax(rt)))
}

// listElementSchemaType gives you the inner GraphQL type of a list type string
// (so "[String!]!" -> "String!"). You get "" back if sType isn't a list.
func listElementSchemaType(sType schemaType) schemaType {
	ts := strings.TrimSpace(string(sType))
	if ts == "" {
		return ""
	}
	ts = strings.TrimSuffix(ts, "!")
	if len(ts) < 2 || ts[0] != '[' || ts[len(ts)-1] != ']' {
		return ""
	}
	return schemaType(strings.TrimSpace(ts[1 : len(ts)-1]))
}

// normalizeExistingVariable gives an already-present variable reference its canonical name.
func normalizeExistingVariable(path pathStr, value *ast.Value, vars VariableMap, varTypes varDefs, fields *[]fieldPath, counter *varCounter, canonicalNames varMap, sType schemaType, inferMissing inferMissingGraphQLTypes) error {
	canonical, exists := canonicalNames[value.Raw]
	if !exists {
		varType, err := schemaOrInferVariableType(sType, gqlStringNonNull, inferMissing, path)
		if err != nil {
			return err
		}
		canonical = nextVariableName(counter)
		canonicalNames[value.Raw] = canonical
		vars[canonical] = zeroValueForType(schemaType(varType))
		varTypes[canonical] = varType
	}
	value.Raw = canonical
	*fields = append(*fields, fieldPath(path))
	return nil
}

// zeroValueForType hands back a zero value that fits the schema type.
func zeroValueForType(sType schemaType) any {
	typeStr := strings.TrimSuffix(string(sType), "!")
	typeStr = strings.TrimPrefix(typeStr, "[")
	typeStr = strings.TrimSuffix(typeStr, "]")
	typeStr = strings.TrimSuffix(typeStr, "!")

	switch baseTypeName(typeStr) {
	case baseTypeInt:
		return int64(0)
	case baseTypeFloat:
		return float64(0)
	case baseTypeBoolean:
		return false
	default:
		return ""
	}
}

// normalizeListValue normalizes a list value.
func normalizeListValue(idx schema.Index, path pathStr, value *ast.Value, vars VariableMap, varTypes varDefs, fields *[]fieldPath, counter *varCounter, canonicalNames varMap, sType schemaType, inferMissing inferMissingGraphQLTypes) error {
	if listHasLiterals(value.Children) {
		return normalizeListLiterals(path, value, vars, varTypes, fields, counter, sType, inferMissing)
	}
	return normalizeListVariablesOnly(idx, path, value, vars, varTypes, fields, counter, canonicalNames, sType, inferMissing)
}

// listHasLiterals reports whether any child of the list is a non-variable literal.
func listHasLiterals(children ast.ChildValueList) bool {
	for _, child := range children {
		if child.Value != nil && child.Value.Kind != ast.Variable {
			return true
		}
	}
	return false
}

// normalizeListVariablesOnly normalizes a list whose elements are all variables.
// Each element gets the list's element type; if that type can't be resolved in
// strict mode, normalizing the element bubbles up ErrGraphQLTypeUnresolved.
func normalizeListVariablesOnly(idx schema.Index, path pathStr, value *ast.Value, vars VariableMap, varTypes varDefs, fields *[]fieldPath, counter *varCounter, canonicalNames varMap, sType schemaType, inferMissing inferMissingGraphQLTypes) error {
	elemType := listElementSchemaType(sType)
	for _, child := range value.Children {
		if err := normalizeValue(idx, path, child.Value, vars, varTypes, fields, counter, canonicalNames, elemType, inferMissing); err != nil {
			return err
		}
	}
	return nil
}

// normalizeListLiterals swaps a list that contains literals for a single variable.
func normalizeListLiterals(path pathStr, value *ast.Value, vars VariableMap, varTypes varDefs, fields *[]fieldPath, counter *varCounter, sType schemaType, inferMissing inferMissingGraphQLTypes) error {
	listValues := extractListValues(value.Children)
	varType, err := schemaOrInferVariableType(sType, inferListType(listValues), inferMissing, path)
	if err != nil {
		return err
	}
	replaceWithVariable(path, value, vars, varTypes, fields, counter, listValues, varType)
	value.Children = nil
	return nil
}

// extractListValues pulls Go values out of a list's children.
func extractListValues(children ast.ChildValueList) []any {
	values := make([]any, 0, len(children))
	for _, child := range children {
		if child.Value == nil {
			continue
		}
		values = append(values, extractSingleValue(child.Value))
	}
	return values
}

// extractSingleValue pulls a Go value out of an AST value.
//
// Replay-fidelity limitation: a nested ObjectValue or ListValue appearing inside a
// literal list is NOT recursed into — it collapses to a fixed placeholder sentinel
// (map[string]any{"_type": "object"} for an object, []any{"_type", "list"} for a
// list) rather than the real nested data. List literals are lifted wholesale into a
// single generated variable, so these inner placeholders only ever surface as the
// shape of that variable's value, never as something the rewritten query reads back.
// This is intentional: faithfully reconstructing arbitrarily nested literals here
// would duplicate the object/list normalization the rest of the package already does
// on the AST. The placeholder contract is pinned by TestExtractSingleValue.
func extractSingleValue(v *ast.Value) any {
	switch v.Kind {
	case ast.IntValue:
		return int64(parseIntValue(rawStr(v.Raw)))
	case ast.FloatValue:
		return float64(parseFloatValue(rawStr(v.Raw)))
	case ast.BooleanValue:
		return v.Raw == rawTrue
	case ast.NullValue:
		return nil
	case ast.ObjectValue:
		return map[string]any{placeholderTypeKey: placeholderObjectValue}
	case ast.ListValue:
		return []any{placeholderTypeKey, placeholderListValue}
	default:
		return v.Raw
	}
}

// normalizeObjectValue normalizes an object value's children, using the
// INPUT_OBJECT field types from the schema index.
func normalizeObjectValue(idx schema.Index, path pathStr, value *ast.Value, vars VariableMap, varTypes varDefs, fields *[]fieldPath, counter *varCounter, canonicalNames varMap, parentSType schemaType, inferMissing inferMissingGraphQLTypes) error {
	base := unwrapToNamedInputType(parentSType)
	effectiveInfer := inferObjectFieldInferenceMode(namedTypeName(base), inferMissing)
	sort.Slice(value.Children, func(i, j int) bool {
		return value.Children[i].Name < value.Children[j].Name
	})
	for _, child := range value.Children {
		if err := normalizeObjectChild(idx, path, base, child, vars, varTypes, fields, counter, canonicalNames, effectiveInfer); err != nil {
			return err
		}
	}
	return nil
}

// normalizeObjectChild normalizes a single field of an input-object literal.
func normalizeObjectChild(idx schema.Index, path pathStr, base schemaType, child *ast.ChildValue, vars VariableMap, varTypes varDefs, fields *[]fieldPath, counter *varCounter, canonicalNames varMap, inferMissing inferMissingGraphQLTypes) error {
	childPath := pathStr(string(path) + "." + child.Name)
	childType := objectChildSchemaType(idx, base, child.Name)
	if childType == "" && inferMissing == graphQLTypesFromSchema {
		return ErrGraphQLTypeUnresolved.With(nil, keyPath, string(childPath), keyInputType, string(base), keyField, child.Name)
	}
	return normalizeValue(idx, childPath, child.Value, vars, varTypes, fields, counter, canonicalNames, childType, inferMissing)
}

// objectChildSchemaType resolves the schema type of an input-object field, and
// skips the lookup entirely when the parent is an opaque JSON-like scalar.
func objectChildSchemaType(idx schema.Index, base schemaType, name string) schemaType {
	if base == "" || bool(isOpaqueJSONLikeScalarType(namedTypeName(base))) {
		return ""
	}
	return schemaType(idx.ArgType(schema.TypeNameInput(base), schema.FieldNameInput(name), ""))
}

// isOpaqueJSONLikeScalarType reports whether the named input type is a scalar
// that doesn't expose input fields through introspection (think PostGraphile JSON
// filters). Nested object literals under these can't use ArgType lookups.
func isOpaqueJSONLikeScalarType(namedUnwrapped namedTypeName) isJSONLike {
	n := namedTypeName(strings.TrimSpace(string(namedUnwrapped)))
	if n == "" {
		return false
	}
	switch n {
	case scalarJSON, scalarJSONObject, scalarJSONB, scalarOpaque:
		return true
	default:
		return false
	}
}

// inferObjectFieldInferenceMode returns graphQLTypesInferredOK for subtrees living
// under opaque JSON scalars, so nested filter shapes normalize without needing the
// schema to resolve their fields.
func inferObjectFieldInferenceMode(parentNamedType namedTypeName, inferMissing inferMissingGraphQLTypes) inferMissingGraphQLTypes {
	if bool(isOpaqueJSONLikeScalarType(parentNamedType)) {
		return graphQLTypesInferredOK
	}
	return inferMissing
}

// nextVariableName bumps the counter and gives back the next generated variable name.
func nextVariableName(counter *varCounter) string {
	*counter++
	return fmt.Sprintf("var%d", int(*counter))
}

// replaceWithVariable swaps a literal value for a variable reference.
func replaceWithVariable(path pathStr, value *ast.Value, vars VariableMap, varTypes varDefs, fields *[]fieldPath, counter *varCounter, goValue any, varType variableType) {
	varName := nextVariableName(counter)
	vars[varName] = goValue
	varTypes[varName] = varType
	*fields = append(*fields, fieldPath(path))
	value.Kind = ast.Variable
	value.Raw = varName
}

// parseIntValue reads an int64 out of a string. The Sscanf error is intentionally
// discarded: raw always comes from an IntValue token that gqlparser already lexed
// and validated, so the scan can't fail on real AST input (the branch is
// unreachable from a parsed document). The one silent edge is an integer literal
// beyond int64's range, which Sscanf truncates rather than erroring; GraphQL Int is
// spec'd as 32-bit, so an in-spec value can never reach that edge.
func parseIntValue(raw rawStr) intResult {
	var i int64
	_, _ = fmt.Sscanf(string(raw), "%d", &i)
	return intResult(i)
}

// parseFloatValue reads a float64 out of a string. As with parseIntValue, the
// Sscanf error is discarded because raw is a gqlparser-validated FloatValue token,
// so the failure branch is unreachable from a parsed document.
func parseFloatValue(raw rawStr) floatResult {
	var f float64
	_, _ = fmt.Sscanf(string(raw), "%f", &f)
	return floatResult(f)
}

// inferListType works out the GraphQL type for a list value.
func inferListType(values []any) variableType {
	elementType := gqlStringNonNull
	for _, v := range values {
		if v != nil {
			elementType = inferType(v)
			break
		}
	}
	return variableType("[" + string(elementType) + "]!")
}

// inferType works out the GraphQL type from a Go value.
func inferType(value any) variableType {
	switch value.(type) {
	case bool:
		return gqlBooleanNonNull
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return gqlIntNonNull
	case float32, float64:
		return gqlFloatNonNull
	default:
		return gqlStringNonNull
	}
}

// formatMinimal formats the document down to minimal whitespace.
func formatMinimal(doc *ast.QueryDocument) formattedQuery {
	var b strings.Builder
	formatter.NewFormatter(&b, formatter.WithIndent("")).FormatQueryDocument(doc)
	normalized := reSpaces.ReplaceAllString(strings.ReplaceAll(b.String(), "\n", " "), "$1$2")
	return formattedQuery(strings.TrimSpace(normalized))
}

// buildIndex builds a reverse index that maps each field name to its paths.
func buildIndex(paths []fieldPath) fieldIndex {
	index := make(fieldIndex)
	for _, path := range paths {
		addPathSegments(index, path)
	}
	sortIndexPaths(index)
	return index
}

// addPathSegments files each segment of a path under its field name.
func addPathSegments(index fieldIndex, path fieldPath) {
	for _, segment := range strings.Split(string(path), ".") {
		if segment == "" {
			continue
		}
		fn := fieldName(segment)
		if !indexContainsPath(index[fn], path) {
			index[fn] = append(index[fn], path)
		}
	}
}

// indexContainsPath reports whether paths already has target in it.
func indexContainsPath(paths []fieldPath, target fieldPath) bool {
	for _, existing := range paths {
		if existing == target {
			return true
		}
	}
	return false
}

// sortIndexPaths sorts the paths inside each field-name entry.
func sortIndexPaths(index fieldIndex) {
	for fn := range index {
		sort.Slice(index[fn], func(i, j int) bool {
			return index[fn][i] < index[fn][j]
		})
	}
}
