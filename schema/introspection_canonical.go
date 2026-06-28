package schema

// The introspection JSON shape depends on the client: aliased queries give us
// snake_case keys (input_fields, of_type, enum_values, possible_types, schema),
// while standard GraphQL introspection gives camelCase (inputFields, ofType,
// enumValues, possibleTypes, __schema). We pick snake_case as canonical — it
// matches the struct JSON tags — so type resolution has a single code path.

type (
	introspectionJSONDstKey   string // canonical snake_case key after normalizing
	introspectionFieldListKey string // a JSON key holding a fields array
	introspectionJSONSrcKey   string // raw camelCase JSON key from standard introspection
)

// unifyIntrospectionEnvelope renames data.__schema to data.schema when schema isn't already there.
func unifyIntrospectionEnvelope(raw map[string]any) {
	dataObj, ok := raw["data"].(map[string]any)
	if !ok {
		return
	}
	if _, has := dataObj["schema"]; has {
		return
	}
	if s, ok := dataObj["__schema"].(map[string]any); ok {
		dataObj["schema"] = s
	}
}

func introspectionSchemaObjectFromData(dataObj map[string]any) map[string]any {
	if s, ok := dataObj["schema"].(map[string]any); ok {
		return s
	}
	if s, ok := dataObj["__schema"].(map[string]any); ok {
		return s
	}
	return nil
}

// canonicalizeIntrospectionResponse rewrites data.schema (or __schema).types[]
// to snake_case keys, in place.
func canonicalizeIntrospectionResponse(raw map[string]any) {
	dataObj, ok := raw["data"].(map[string]any)
	if !ok {
		return
	}
	schemaObj := introspectionSchemaObjectFromData(dataObj)
	if schemaObj == nil {
		return
	}
	promoteToSnake(schemaObj, "queryType", "query_type")
	promoteToSnake(schemaObj, "mutationType", "mutation_type")
	types, ok := schemaObj["types"].([]any)
	if !ok {
		return
	}
	for _, t := range types {
		if typeObj, ok := t.(map[string]any); ok {
			canonicalizeIntrospectionTypeMap(typeObj)
		}
	}
}

// canonicalizeIntrospectionTypeMap rewrites one __Type JSON object to snake_case.
func canonicalizeIntrospectionTypeMap(typeObj map[string]any) {
	promoteToSnake(typeObj, "inputFields", "input_fields")
	promoteToSnake(typeObj, "enumValues", "enum_values")
	promoteToSnake(typeObj, "possibleTypes", "possible_types")

	canonicalizeFieldsInList(typeObj, "fields")
	canonicalizeInputFieldsInList(typeObj, "input_fields")
}

func canonicalizeFieldsInList(typeObj map[string]any, key introspectionFieldListKey) {
	fields, ok := typeObj[string(key)].([]any)
	if !ok {
		return
	}
	for _, f := range fields {
		fieldObj, ok := f.(map[string]any)
		if !ok {
			continue
		}
		promoteToSnake(fieldObj, "isDeprecated", "is_deprecated")
		promoteToSnake(fieldObj, "deprecationReason", "deprecation_reason")
		canonicalizeTypeRefMap(fieldObj["type"])
		if args, ok := fieldObj["args"].([]any); ok {
			canonicalizeInputValueList(args)
		}
	}
}

func canonicalizeInputFieldsInList(typeObj map[string]any, key introspectionFieldListKey) {
	items, ok := typeObj[string(key)].([]any)
	if !ok {
		return
	}
	canonicalizeInputValueList(items)
}

func canonicalizeInputValueList(items []any) {
	for _, item := range items {
		infObj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		promoteToSnake(infObj, "defaultValue", "default_value")
		canonicalizeTypeRefMap(infObj["type"])
	}
}

// canonicalizeTypeRefMap rewrites a nested __Type reference (LIST / NON_NULL /
// named) to snake_case.
func canonicalizeTypeRefMap(ref any) {
	typeObj, ok := ref.(map[string]any)
	if !ok {
		return
	}
	promoteToSnake(typeObj, "ofType", "of_type")
	canonicalizeTypeRefMap(introspectionOfTypeRef(typeObj))
}

// promoteToSnake copies srcKey (camelCase) over to dstKey (snake_case) when dstKey isn't already set.
func promoteToSnake(m map[string]any, srcKey introspectionJSONSrcKey, dstKey introspectionJSONDstKey) {
	if _, hasDst := m[string(dstKey)]; hasDst {
		return
	}
	if v, ok := m[string(srcKey)]; ok {
		m[string(dstKey)] = v
	}
}

// introspectionOfTypeRef pulls the nested type out of a LIST/NON_NULL ref.
func introspectionOfTypeRef(typeObj map[string]any) any {
	if v, ok := typeObj["of_type"]; ok && v != nil {
		return v
	}
	return typeObj["ofType"]
}
