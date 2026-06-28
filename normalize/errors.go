package normalize

import errs "github.com/gomatic/go-error"

// Sentinel errors this package can return. Match them with errors.Is, not by string.
const (
	ErrEmptyQuery            errs.Const = "empty query"
	ErrGraphQLTypeUnresolved errs.Const = "unresolved type"
	ErrGraphQLValidation     errs.Const = "schema validation"
	ErrQueryParse            errs.Const = "parse query"
)

// Context keys we hang on wrapped errors. Kept here so a repeated key is spelled once.
const (
	keyArgument   = "argument"
	keyCount      = "count"
	keyField      = "field"
	keyInputType  = "input_type"
	keyMessages   = "messages"
	keyParentType = "parent_type"
	keyPath       = "path"
	keyReason     = "reason"
	keyType       = "type"
)
