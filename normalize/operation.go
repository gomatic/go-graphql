package normalize

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"

	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/parser"
)

// OperationName is a GraphQL operation name (it matches /[_A-Za-z][_0-9A-Za-z]*/).
type OperationName string

// operationHashPrefix goes in front of hash-derived names so they come out as
// valid GraphQL names, which can't start with a digit.
const operationHashPrefix = "op_"

// ResolveOperation rewrites a query so it carries a named operation and gives you
// back the name to send as the request operationName.
//
// The name we return is always valid to send per the GraphQL over HTTP spec — it
// either names the query's only operation or matches one that's already there:
//   - Zero operations: you get the formatted query and an empty name.
//   - One operation: we name it with preferred (or, when preferred is empty, a
//     stable hash of the query) and return that name.
//   - Many operations: these documents already need named operations, so we return
//     preferred only when it matches an existing operation; otherwise the name comes
//     back empty so the caller leaves operationName off and lets the server sort it out.
func ResolveOperation(raw QueryInput, preferred OperationName) (QueryResult, OperationName, error) {
	if raw == "" {
		return "", "", ErrEmptyQuery
	}
	doc, err := parser.ParseQuery(&ast.Source{Input: string(raw)})
	if err != nil {
		return "", "", ErrQueryParse.With(err)
	}
	stripComments(doc)
	return resolveOperationName(doc, preferred)
}

// resolveOperationName picks the operation name to send based on how many
// operations the document defines.
func resolveOperationName(doc *ast.QueryDocument, preferred OperationName) (QueryResult, OperationName, error) {
	switch len(doc.Operations) {
	case 0:
		return QueryResult(formatMinimal(doc)), "", nil
	case 1:
		return nameSingleOperation(doc, preferred)
	default:
		return namePreferredIfPresent(doc, preferred)
	}
}

// nameSingleOperation names a document's only operation, falling back to a stable
// query hash when you don't give it a preferred name.
func nameSingleOperation(doc *ast.QueryDocument, preferred OperationName) (QueryResult, OperationName, error) {
	name := sanitizeName(preferred)
	if name == "" {
		name = hashName(QueryResult(formatMinimal(doc)))
	}
	doc.Operations[0].Name = string(name)
	return QueryResult(formatMinimal(doc)), name, nil
}

// namePreferredIfPresent returns preferred only when it names an operation that's
// actually in a multi-operation document; otherwise you get an empty name.
func namePreferredIfPresent(doc *ast.QueryDocument, preferred OperationName) (QueryResult, OperationName, error) {
	if preferred != "" && bool(hasOperationNamed(doc, preferred)) {
		return QueryResult(formatMinimal(doc)), preferred, nil
	}
	return QueryResult(formatMinimal(doc)), "", nil
}

// sanitizeName squeezes an arbitrary string into a valid GraphQL operation name
// (/[_A-Za-z][_0-9A-Za-z]*/): characters that don't fit turn into underscores, and
// a leading digit gets an underscore in front. Empty input gives you "" back.
func sanitizeName(name OperationName) OperationName {
	if name == "" {
		return ""
	}
	out := strings.Map(sanitizeRune, string(name))
	if out[0] >= '0' && out[0] <= '9' {
		out = "_" + out
	}
	return OperationName(out)
}

// sanitizeRune keeps GraphQL name characters and turns everything else into '_'.
func sanitizeRune(r rune) rune {
	if r == '_' || (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
		return r
	}
	return '_'
}

// hashName derives a stable, valid operation name from the query text.
func hashName(q QueryResult) OperationName {
	sum := sha256.Sum256([]byte(q))
	return OperationName(operationHashPrefix + hex.EncodeToString(sum[:])[:12])
}

// operationPresent says whether the document defines an operation with a given name.
type operationPresent bool

// hasOperationNamed reports whether the document defines an operation called name.
func hasOperationNamed(doc *ast.QueryDocument, name OperationName) operationPresent {
	for _, op := range doc.Operations {
		if op.Name == string(name) {
			return true
		}
	}
	return false
}

// stripComments wipes operation and selection comments before we format.
func stripComments(doc *ast.QueryDocument) {
	for _, op := range doc.Operations {
		op.Comment = nil
		removeCommentsFromSelectionSet(op.SelectionSet)
	}
}
