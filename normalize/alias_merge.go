package normalize

import (
	"fmt"
	"sort"
	"strings"

	"github.com/vektah/gqlparser/v2/ast"
)

// The named types alias merging works with.
type (
	aliasName            string // a generated alias, like al1 or al2
	aliasSequence        *int   // the counter that hands out sequential aliases
	directiveFingerprint string // the fingerprint of a directive list
	fieldFingerprint     string // the fingerprint of a field's args plus directives
	listFingerprint      string // the fingerprint of a list value
	objectFingerprint    string // the fingerprint of an object value
	valueHash            string // the fingerprint of a single value
)

// assignMergeAliasesForSelectionSet drops client field aliases, except where
// sibling fields share a GraphQL name but carry args/directives that won't merge.
// For those, it hands out sequential aliases al1, al2, … (per document) so replay
// stays valid without us hanging onto the client's chosen alias names.
func assignMergeAliasesForSelectionSet(set ast.SelectionSet, seq aliasSequence) {
	if len(set) == 0 {
		return
	}
	byName := groupDirectFieldsByName(set)
	for _, group := range byName {
		assignAliasesForSameNameFields(group, seq)
	}
}

func groupDirectFieldsByName(set ast.SelectionSet) map[string][]*ast.Field {
	byName := make(map[string][]*ast.Field)
	for _, sel := range set {
		f, ok := sel.(*ast.Field)
		if !ok || f == nil {
			continue
		}
		byName[f.Name] = append(byName[f.Name], f)
	}
	return byName
}

func assignAliasesForSameNameFields(group []*ast.Field, seq aliasSequence) {
	if len(group) == 1 {
		group[0].Alias = ""
		return
	}
	buckets := bucketFieldsByMergeFingerprint(group)
	keys := sortedMergeFingerprintKeys(buckets)
	if len(keys) == 1 {
		clearFieldAliases(group)
		return
	}
	for i, k := range keys {
		fields := buckets[k]
		if i == 0 {
			clearFieldAliases(fields)
			continue
		}
		setAliasOnFields(fields, nextSequentialAlias(seq))
	}
}

func clearFieldAliases(fields []*ast.Field) {
	for _, f := range fields {
		if f != nil {
			f.Alias = ""
		}
	}
}

func setAliasOnFields(fields []*ast.Field, alias aliasName) {
	for _, f := range fields {
		if f != nil {
			f.Alias = string(alias)
		}
	}
}

func bucketFieldsByMergeFingerprint(group []*ast.Field) map[fieldFingerprint][]*ast.Field {
	buckets := make(map[fieldFingerprint][]*ast.Field)
	for _, f := range group {
		if f == nil {
			continue
		}
		k := fieldMergeFingerprint(f)
		buckets[k] = append(buckets[k], f)
	}
	return buckets
}

func sortedMergeFingerprintKeys(buckets map[fieldFingerprint][]*ast.Field) []fieldFingerprint {
	keys := make([]fieldFingerprint, 0, len(buckets))
	for k := range buckets {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		return keys[i] < keys[j]
	})
	return keys
}

func nextSequentialAlias(seq aliasSequence) aliasName {
	*seq++
	return aliasName(fmt.Sprintf("al%d", *seq))
}

func fieldMergeFingerprint(f *ast.Field) fieldFingerprint {
	return fieldFingerprint(
		string(argumentListFingerprint(f.Arguments)) + "|" + string(directiveListFingerprint(f.Directives)),
	)
}

func directiveListFingerprint(ds ast.DirectiveList) directiveFingerprint {
	if len(ds) == 0 {
		return "[]"
	}
	parts := make([]string, 0, len(ds))
	for _, d := range ds {
		if d == nil {
			continue
		}
		parts = append(parts, d.Name+":"+string(argumentListFingerprint(d.Arguments)))
	}
	sort.Strings(parts)
	return directiveFingerprint("[" + strings.Join(parts, ",") + "]")
}

func argumentListFingerprint(args ast.ArgumentList) fieldFingerprint {
	if len(args) == 0 {
		return "()"
	}
	parts := make([]string, 0, len(args))
	for _, a := range args {
		if a == nil {
			continue
		}
		parts = append(parts, a.Name+":"+string(valueFingerprint(a.Value)))
	}
	sort.Strings(parts)
	return fieldFingerprint("(" + strings.Join(parts, ",") + ")")
}

// valueHashNull is the fingerprint we use for a missing (nil) value.
const valueHashNull valueHash = "null"

func valueFingerprint(value *ast.Value) valueHash {
	if value == nil {
		return valueHashNull
	}
	switch value.Kind {
	case ast.Variable:
		return valueHash("$" + value.Raw)
	case ast.IntValue, ast.FloatValue, ast.StringValue, ast.BlockValue, ast.BooleanValue, ast.EnumValue:
		return valueHash(fmt.Sprintf("%d:%s", value.Kind, value.Raw))
	case ast.NullValue:
		return "nullval"
	case ast.ListValue:
		return valueHash(listValueFingerprint(value.Children))
	case ast.ObjectValue:
		return valueHash(objectValueFingerprint(value.Children))
	default:
		return valueHash(fmt.Sprintf("unknown:%d", value.Kind))
	}
}

func listValueFingerprint(children ast.ChildValueList) listFingerprint {
	parts := make([]string, 0, len(children))
	for _, ch := range children {
		if ch == nil {
			continue
		}
		parts = append(parts, string(valueFingerprint(ch.Value)))
	}
	return listFingerprint("[" + strings.Join(parts, ",") + "]")
}

func objectValueFingerprint(children ast.ChildValueList) objectFingerprint {
	if len(children) == 0 {
		return "{}"
	}
	pairs := make([]string, 0, len(children))
	for _, ch := range children {
		if ch == nil {
			continue
		}
		pairs = append(pairs, ch.Name+":"+string(valueFingerprint(ch.Value)))
	}
	sort.Strings(pairs)
	return objectFingerprint("{" + strings.Join(pairs, ",") + "}")
}
