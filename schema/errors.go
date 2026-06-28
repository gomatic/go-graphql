package schema

import errs "github.com/gomatic/go-error"

// Sentinel errors this package can return. Match them with errors.Is, not by string.
const (
	ErrIntrospectionEmpty           errs.Const = "no introspection types"
	ErrIntrospectionGraphQLErrors   errs.Const = "introspection errors"
	ErrIntrospectionMissingName     errs.Const = "type missing name"
	ErrIntrospectionNilType         errs.Const = "nil type ref"
	ErrIntrospectionParse           errs.Const = "parse introspection"
	ErrIntrospectionSDL             errs.Const = "render SDL"
	ErrIntrospectionUnionEmpty      errs.Const = "empty union"
	ErrIntrospectionUnmarshal       errs.Const = "unmarshal introspection"
	ErrIntrospectionUnsupportedKind errs.Const = "unsupported kind"
	ErrNoSchemas                    errs.Const = "no schemas"
	ErrSchemaConflict               errs.Const = "schema conflict"
	ErrSchemaSDLMissing             errs.Const = "missing schema SDL"
	ErrUnknownField                 errs.Const = "unknown field"
)
