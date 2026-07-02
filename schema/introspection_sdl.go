package schema

// We generate SDL from introspection JSON the way gqlfetch does
// (github.com/suessflorian/gqlfetch, BSD-3-Clause): decode the response, print
// SDL, then load it. The struct layout and printing follow that tool; here the
// error handling replaces its log.Fatalf, and input-field descriptions come
// from the field rather than the parent type.

import (
	"bytes"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/vektah/gqlparser/v2/ast"
)

// Named types for introspection struct fields and SDL generation.
type (
	deprecatedFlag      bool   // the is_deprecated JSON field
	errorMessage        string // an error message from an introspection response
	introspectionKindID string // a kind string for type references (LIST, NON_NULL, …)
	introspectionName   string // a name field across introspection types
	sdlDescription      string // GraphQL description block text
	sdlFormat           string // a format string for writeStringFmt
	sdlOutput           string // rendered SDL text
	typeRefString       string // a rendered GraphQL type reference (e.g. "[String!]!")
	unmarshalContext    string // an error-context label for unmarshalJSONArrayOrNull
)

// introspectionResults is the top-level JSON envelope for an introspection response.
type introspectionResults struct {
	Errors []introspectionError `json:"errors"`
	Data   introspectionData    `json:"data"`
}

// introspectionError is one error entry in an introspection response.
type introspectionError struct {
	Message errorMessage `json:"message"`
}

// introspectionData holds the schema object under data.schema, once it's canonicalized.
type introspectionData struct {
	Schema introspectionSchema `json:"schema"`
}

// introspectionSchema holds the top-level schema introspection: types and directives.
type introspectionSchema struct {
	Directives []introspectionDirectiveDefinition `json:"directives"`
	Types      []introspectionTypeDefinition      `json:"types"`
}

// introspectionTypeDefinition is one type from __Type introspection.
type introspectionTypeDefinition struct {
	Description   sdlDescription            `json:"description"`
	EnumValues    json.RawMessage           `json:"enum_values"`
	Fields        []introspectedTypeField   `json:"fields"`
	InputFields   []introspectionInputField `json:"input_fields"`
	Interfaces    []ast.Definition          `json:"interfaces"`
	Kind          ast.DefinitionKind        `json:"kind"`
	Name          introspectionName         `json:"name"`
	PossibleTypes json.RawMessage           `json:"possible_types"`
}

// introspectedTypeField is one field on an OBJECT or INTERFACE type.
type introspectedTypeField struct {
	DeprecationReason any                       `json:"deprecation_reason"`
	Type              *introspectedType         `json:"type"`
	Description       sdlDescription            `json:"description"`
	Name              introspectionName         `json:"name"`
	Args              []introspectionInputField `json:"args"`
	IsDeprecated      deprecatedFlag            `json:"is_deprecated"`
}

// introspectedEnumValue is one value of an ENUM type.
type introspectedEnumValue struct {
	DeprecationReason any               `json:"deprecation_reason"`
	Description       sdlDescription    `json:"description"`
	Name              introspectionName `json:"name"`
	IsDeprecated      deprecatedFlag    `json:"is_deprecated"`
}

// introspectionDirectiveDefinition is one directive definition.
type introspectionDirectiveDefinition struct {
	Description sdlDescription              `json:"description"`
	Name        introspectionName           `json:"name"`
	Args        []introspectionDirectiveArg `json:"args"`
	Locations   []ast.DirectiveLocation     `json:"locations"`
}

// introspectionDirectiveArg is one argument on a directive definition.
type introspectionDirectiveArg struct {
	DefaultValue any               `json:"default_value"`
	Type         *introspectedType `json:"type"`
	Description  sdlDescription    `json:"description"`
	Name         introspectionName `json:"name"`
}

// introspectionInputField is an input value — a field arg or an input object field.
type introspectionInputField struct {
	DefaultValue any               `json:"default_value"`
	Type         *introspectedType `json:"type"`
	Description  sdlDescription    `json:"description"`
	Name         introspectionName `json:"name"`
}

// introspectedType is a __Type reference — a named type, or a LIST/NON_NULL wrapper.
type introspectedType struct {
	Name   *introspectionName  `json:"name"`
	OfType *introspectedType   `json:"of_type"`
	Kind   introspectionKindID `json:"kind"`
}

const (
	// emptyEnumPlaceholder is what we emit when introspection hands back no enum
	// values (a redacted schema, or an alias merge that didn't happen). GraphQL
	// wants at least one value; real values still win when enum_values is present.
	emptyEnumPlaceholder introspectionName = "_emptyEnum"

	introspectionKindList    introspectionKindID = "LIST"
	introspectionKindNonNull introspectionKindID = "NON_NULL"
)

// Built-in GraphQL scalar type names. We leave these out of the generated SDL
// because gqlparser already supplies them through its Prelude.
const (
	scalarBoolean = "Boolean"
	scalarFloat   = "Float"
	scalarID      = "ID"
	scalarInt     = "Int"
	scalarString  = "String"
)

// excludeDirectives has to stay in sync with gqlparser's validator builtins (see
// validator/schema.go: include, skip, deprecated, specifiedBy, defer, oneOf).
// Printing them from introspection duplicates the Prelude, and some servers emit
// type refs that break introspectionTypeToAstType (e.g. @specifiedBy url).
var (
	excludeDirectives  = []string{"deprecated", "include", "skip", "specifiedBy", "defer", "oneOf"}
	excludeScalarTypes = []string{scalarID, scalarInt, scalarString, scalarFloat, scalarBoolean}
)

func writeStringFmt(sb *strings.Builder, format sdlFormat, args ...any) {
	_, _ = fmt.Fprintf(sb, string(format), args...)
}

// sParam names the s parameter of writeStr; rename it to the real domain concept.
type sParam string

// writeStr appends s to sb. strings.Builder.WriteString never errors, so we
// throw the result away.
func writeStr(sb *strings.Builder, s sParam) {
	_, _ = sb.WriteString(string(s))
}

func introspectionTypeToAstType(typ *introspectedType) (*ast.Type, error) {
	if typ == nil {
		return nil, ErrIntrospectionNilType.With(nil)
	}
	if typ.OfType == nil {
		return namedAstType(typ)
	}
	return wrappedAstType(typ)
}

func namedAstType(typ *introspectedType) (*ast.Type, error) {
	if typ.Name == nil || string(*typ.Name) == "" {
		return nil, ErrIntrospectionMissingName.With(nil)
	}
	return &ast.Type{NamedType: string(*typ.Name)}, nil
}

func wrappedAstType(typ *introspectedType) (*ast.Type, error) {
	inner, err := introspectionTypeToAstType(typ.OfType)
	if err != nil {
		return nil, err
	}
	switch typ.Kind {
	case introspectionKindNonNull:
		cp := *inner
		cp.NonNull = true
		return &cp, nil
	case introspectionKindList:
		return &ast.Type{Elem: inner}, nil
	default:
		return inner, nil
	}
}

func typeStringOrError(t *introspectedType) (typeRefString, error) {
	at, err := introspectionTypeToAstType(t)
	if err != nil {
		return "", err
	}
	return typeRefString(at.String()), nil
}

func unmarshalJSONArrayOrNull(raw json.RawMessage, dest any, ctx unmarshalContext) error {
	b := bytes.TrimSpace(raw)
	if len(b) == 0 || bytes.Equal(b, []byte("null")) {
		return nil
	}
	if err := json.Unmarshal(b, dest); err != nil {
		return ErrIntrospectionUnmarshal.With(err, "context", string(ctx))
	}
	return nil
}

func printIntrospectionSchema(s introspectionSchema) (sdlOutput, error) {
	sb := &strings.Builder{}
	if err := printDirectives(sb, s.Directives); err != nil {
		return "", err
	}
	if err := printTypes(sb, s.Types); err != nil {
		return "", err
	}
	return sdlOutput(sb.String()), nil
}

func printDirectives(sb *strings.Builder, directives []introspectionDirectiveDefinition) error {
	for _, directive := range directives {
		if slices.Contains(excludeDirectives, string(directive.Name)) {
			continue
		}
		if err := printOneDirective(sb, directive); err != nil {
			return err
		}
	}
	return nil
}

func printOneDirective(sb *strings.Builder, directive introspectionDirectiveDefinition) error {
	printDescription(sb, directive.Description)
	writeStringFmt(sb, "directive @%s", string(directive.Name))
	if err := printDirectiveArgs(sb, directive); err != nil {
		return err
	}
	writeStr(sb, sParam(" on "))
	writeStr(sb, sParam(joinDirectiveLocations(directive.Locations)))
	writeStr(sb, sParam("\n\n"))
	return nil
}

func printDirectiveArgs(sb *strings.Builder, directive introspectionDirectiveDefinition) error {
	if len(directive.Args) == 0 {
		return nil
	}
	writeStr(sb, sParam("(\n"))
	for _, arg := range directive.Args {
		if err := printDirectiveArg(sb, directive.Name, arg); err != nil {
			return err
		}
	}
	writeStr(sb, sParam(")"))
	return nil
}

func joinDirectiveLocations(locations []ast.DirectiveLocation) string {
	parts := make([]string, 0, len(locations))
	for _, l := range locations {
		parts = append(parts, string(l))
	}
	return strings.Join(parts, " | ")
}

func printDirectiveArg(sb *strings.Builder, directiveName introspectionName, arg introspectionDirectiveArg) error {
	printDescription(sb, arg.Description)
	ts, err := typeStringOrError(arg.Type)
	if err != nil {
		return ErrIntrospectionSDL.With(
			err,
			"element",
			"directive_arg_type",
			"directive",
			string(directiveName),
			"arg",
			string(arg.Name),
		)
	}
	writeStringFmt(sb, "\t%s: %s\n", string(arg.Name), string(ts))
	return nil
}

func printTypes(sb *strings.Builder, types []introspectionTypeDefinition) error {
	for _, typ := range types {
		if skipType(typ) {
			continue
		}
		if err := printType(sb, typ); err != nil {
			return err
		}
	}
	return nil
}

func skipType(typ introspectionTypeDefinition) bool {
	if strings.HasPrefix(string(typ.Name), "__") {
		return true
	}
	return typ.Kind == ast.Scalar && slices.Contains(excludeScalarTypes, string(typ.Name))
}

func printType(sb *strings.Builder, typ introspectionTypeDefinition) error {
	if typ.Name == "" {
		return ErrIntrospectionMissingName.With(nil, "kind", string(typ.Kind))
	}
	printDescription(sb, typ.Description)
	if err := printOneType(sb, typ); err != nil {
		return err
	}
	writeStr(sb, sParam("\n\n"))
	return nil
}

func printOneType(sb *strings.Builder, typ introspectionTypeDefinition) error {
	switch typ.Kind {
	case ast.Object:
		return printObjectType(sb, typ)
	case ast.Union:
		return printUnionType(sb, typ)
	case ast.Enum:
		return printEnumType(sb, typ)
	case ast.Scalar:
		writeStringFmt(sb, "scalar %s", string(typ.Name))
		return nil
	case ast.InputObject:
		return printInputObjectType(sb, typ)
	case ast.Interface:
		return printInterfaceType(sb, typ)
	default:
		return ErrIntrospectionUnsupportedKind.With(nil, "type", string(typ.Name), "kind", string(typ.Kind))
	}
}

func printObjectType(sb *strings.Builder, typ introspectionTypeDefinition) error {
	writeStringFmt(sb, "type %s ", string(typ.Name))
	printImplements(sb, typ.Interfaces)
	writeStr(sb, sParam("{\n"))
	for _, field := range typ.Fields {
		if err := printObjectField(sb, typ.Name, field); err != nil {
			return err
		}
	}
	writeStr(sb, sParam("}"))
	return nil
}

func printImplements(sb *strings.Builder, interfaces []ast.Definition) {
	if len(interfaces) == 0 {
		return
	}
	writeStr(sb, sParam("implements "))
	names := make([]string, 0, len(interfaces))
	for _, intface := range interfaces {
		names = append(names, intface.Name)
	}
	writeStr(sb, sParam(strings.Join(names, " & ")))
}

func printObjectField(sb *strings.Builder, parentName introspectionName, field introspectedTypeField) error {
	printDescription(sb, field.Description)
	writeStringFmt(sb, "\t%s", string(field.Name))
	if err := printFieldArgs(sb, parentName, field.Name, field.Args); err != nil {
		return err
	}
	ts, err := typeStringOrError(field.Type)
	if err != nil {
		return ErrIntrospectionSDL.With(
			err,
			"element",
			"field_type",
			"parent",
			string(parentName),
			"field",
			string(field.Name),
		)
	}
	writeStringFmt(sb, ": %s", string(ts))
	printDeprecation(sb, field.IsDeprecated, field.DeprecationReason)
	writeStr(sb, sParam("\n"))
	return nil
}

func printFieldArgs(
	sb *strings.Builder,
	parentName, fieldName introspectionName,
	args []introspectionInputField,
) error {
	if len(args) == 0 {
		return nil
	}
	writeStr(sb, sParam("(\n"))
	for _, arg := range args {
		printDescription(sb, arg.Description)
		ts, err := typeStringOrError(arg.Type)
		if err != nil {
			return ErrIntrospectionSDL.With(
				err,
				"element",
				"arg_type",
				"parent",
				string(parentName),
				"field",
				string(fieldName),
				"arg",
				string(arg.Name),
			)
		}
		writeStringFmt(sb, "\t\t%s: %s\n", string(arg.Name), string(ts))
	}
	writeStr(sb, sParam("\t)"))
	return nil
}

func printDeprecation(sb *strings.Builder, isDeprecated deprecatedFlag, reason any) {
	if !bool(isDeprecated) {
		return
	}
	writeStr(sb, sParam(" @deprecated"))
	if r, ok := reason.(string); ok && r != "" {
		writeStringFmt(sb, `(reason: "%s")`, r)
	}
}

func printUnionType(sb *strings.Builder, typ introspectionTypeDefinition) error {
	writeStringFmt(sb, "union %s =", string(typ.Name))
	var possible []*introspectedType
	if err := unmarshalJSONArrayOrNull(typ.PossibleTypes, &possible, unmarshalContext("possible_types "+string(typ.Name))); err != nil {
		return err
	}
	if len(possible) == 0 {
		return ErrIntrospectionUnionEmpty.With(nil, "type", string(typ.Name))
	}
	members, err := unionMembers(possible, typ.Name)
	if err != nil {
		return err
	}
	writeStr(sb, sParam(strings.Join(members, " | ")))
	return nil
}

func unionMembers(possible []*introspectedType, name introspectionName) ([]string, error) {
	members := make([]string, 0, len(possible))
	for _, pt := range possible {
		ts, err := typeStringOrError(pt)
		if err != nil {
			return nil, ErrIntrospectionSDL.With(err, "element", "union_member", "type", string(name))
		}
		members = append(members, string(ts))
	}
	return members, nil
}

func printEnumType(sb *strings.Builder, typ introspectionTypeDefinition) error {
	writeStringFmt(sb, "enum %s {\n", string(typ.Name))
	var enumValues []introspectedEnumValue
	if err := unmarshalJSONArrayOrNull(typ.EnumValues, &enumValues, unmarshalContext("enum_values "+string(typ.Name))); err != nil {
		return err
	}
	if len(enumValues) == 0 {
		enumValues = []introspectedEnumValue{{Name: emptyEnumPlaceholder}}
	}
	for _, value := range enumValues {
		printEnumValue(sb, value)
	}
	writeStr(sb, sParam("}"))
	return nil
}

func printEnumValue(sb *strings.Builder, value introspectedEnumValue) {
	printDescription(sb, value.Description)
	writeStringFmt(sb, "\t%s", string(value.Name))
	printDeprecation(sb, value.IsDeprecated, value.DeprecationReason)
	writeStr(sb, sParam("\n"))
}

func printInputObjectType(sb *strings.Builder, typ introspectionTypeDefinition) error {
	writeStringFmt(sb, "input %s {\n", string(typ.Name))
	for _, field := range typ.InputFields {
		printDescription(sb, field.Description)
		ts, err := typeStringOrError(field.Type)
		if err != nil {
			return ErrIntrospectionSDL.With(
				err,
				"element",
				"input_field_type",
				"type",
				string(typ.Name),
				"field",
				string(field.Name),
			)
		}
		writeStringFmt(sb, "\t%s: %s\n", string(field.Name), string(ts))
	}
	writeStr(sb, sParam("}"))
	return nil
}

func printInterfaceType(sb *strings.Builder, typ introspectionTypeDefinition) error {
	writeStringFmt(sb, "interface %s {\n", string(typ.Name))
	for _, field := range typ.Fields {
		if err := printInterfaceField(sb, typ.Name, field); err != nil {
			return err
		}
	}
	writeStr(sb, sParam("}"))
	return nil
}

func printInterfaceField(sb *strings.Builder, parentName introspectionName, field introspectedTypeField) error {
	printDescription(sb, field.Description)
	writeStringFmt(sb, "\t%s", string(field.Name))
	if err := printInterfaceArgs(sb, parentName, field.Name, field.Args); err != nil {
		return err
	}
	ts, err := typeStringOrError(field.Type)
	if err != nil {
		return ErrIntrospectionSDL.With(
			err,
			"element",
			"interface_field_type",
			"parent",
			string(parentName),
			"field",
			string(field.Name),
		)
	}
	writeStringFmt(sb, ": %s\n", string(ts))
	return nil
}

func printInterfaceArgs(
	sb *strings.Builder,
	parentName, fieldName introspectionName,
	args []introspectionInputField,
) error {
	if len(args) == 0 {
		return nil
	}
	writeStr(sb, sParam("(\n"))
	for _, arg := range args {
		ts, err := typeStringOrError(arg.Type)
		if err != nil {
			return ErrIntrospectionSDL.With(
				err,
				"element",
				"interface_arg_type",
				"parent",
				string(parentName),
				"field",
				string(fieldName),
				"arg",
				string(arg.Name),
			)
		}
		writeStringFmt(sb, "\t\t%s: %s\n", string(arg.Name), string(ts))
	}
	writeStr(sb, sParam("\t)"))
	return nil
}

// printDescription writes a GraphQL description block, or nothing when it's
// empty. It only writes to the builder, so it can't fail and returns no error.
func printDescription(sb *strings.Builder, description sdlDescription) {
	if description == "" {
		return
	}
	writeStr(sb, sParam(`"""`))
	writeStr(sb, sParam("\n"))
	writeStr(sb, sParam(string(description)))
	writeStr(sb, sParam("\n"))
	writeStr(sb, sParam(`"""`))
	writeStr(sb, sParam("\n"))
}
