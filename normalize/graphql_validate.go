package normalize

import (
	"strconv"
	"strings"

	"github.com/vektah/gqlparser/v2/ast"
	"github.com/vektah/gqlparser/v2/gqlerror"
	"github.com/vektah/gqlparser/v2/validator"

	"github.com/gomatic/go-graphql/schema"
)

// The named types we use for GraphQL validation results.
type (
	gqlErrorSummary   string // the validation error messages, joined together
	usesIntrospection bool   // whether a query reaches for built-in introspection fields
)

func formatGQLErrors(errs gqlerror.List) gqlErrorSummary {
	msgs := make([]string, 0, len(errs))
	for _, e := range errs {
		if e != nil {
			msgs = append(msgs, e.Error())
		}
	}
	return gqlErrorSummary(strings.Join(msgs, "; "))
}

func validateAgainstGraphQLSchema(idx schema.Index, doc *ast.QueryDocument) error {
	gql := idx.GraphQLSchema()
	if gql == nil {
		return nil
	}
	if bool(queryUsesBuiltinIntrospection(doc)) {
		return nil
	}
	gqlErrs := validator.ValidateWithRules(gql, doc, nil)
	if len(gqlErrs) == 0 {
		return nil
	}
	return ErrGraphQLValidation.With(gqlErrs[0],
		keyMessages, string(formatGQLErrors(gqlErrs)),
		keyCount, strconv.Itoa(len(gqlErrs)))
}

func queryUsesBuiltinIntrospection(doc *ast.QueryDocument) usesIntrospection {
	if doc == nil {
		return false
	}
	seen := map[string]bool{}
	for _, op := range doc.Operations {
		if bool(selectionUsesBuiltinIntrospection(doc, op.SelectionSet, seen)) {
			return true
		}
	}
	for _, frag := range doc.Fragments {
		if bool(selectionUsesBuiltinIntrospection(doc, frag.SelectionSet, seen)) {
			return true
		}
	}
	return false
}

func selectionUsesBuiltinIntrospection(doc *ast.QueryDocument, set ast.SelectionSet, seen map[string]bool) usesIntrospection {
	for _, sel := range set {
		if bool(selectionItemUsesIntrospection(doc, sel, seen)) {
			return true
		}
	}
	return false
}

func selectionItemUsesIntrospection(doc *ast.QueryDocument, sel ast.Selection, seen map[string]bool) usesIntrospection {
	switch s := sel.(type) {
	case *ast.Field:
		return usesIntrospection(strings.HasPrefix(s.Name, "__")) || selectionUsesBuiltinIntrospection(doc, s.SelectionSet, seen)
	case *ast.FragmentSpread:
		return fragmentSpreadUsesIntrospection(doc, s, seen)
	}
	// What's left can only be an *ast.InlineFragment — it's the last ast.Selection type.
	return selectionUsesBuiltinIntrospection(doc, sel.(*ast.InlineFragment).SelectionSet, seen)
}

func fragmentSpreadUsesIntrospection(doc *ast.QueryDocument, spread *ast.FragmentSpread, seen map[string]bool) usesIntrospection {
	if seen[spread.Name] {
		return false
	}
	seen[spread.Name] = true
	fd := doc.Fragments.ForName(spread.Name)
	if fd == nil {
		return false
	}
	return selectionUsesBuiltinIntrospection(doc, fd.SelectionSet, seen)
}
