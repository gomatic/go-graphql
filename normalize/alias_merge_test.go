package normalize

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/vektah/gqlparser/v2/ast"
)

func intVal(raw string) *ast.Value  { return &ast.Value{Kind: ast.IntValue, Raw: raw} }
func varVal(name string) *ast.Value { return &ast.Value{Kind: ast.Variable, Raw: name} }
func field(name, alias string, args ast.ArgumentList) *ast.Field {
	return &ast.Field{Name: name, Alias: alias, Arguments: args}
}

func TestValueFingerprint(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value *ast.Value
		want  valueHash
	}{
		{name: "nil", value: nil, want: "null"},
		{name: "variable", value: varVal("x"), want: "$x"},
		{name: "null value", value: &ast.Value{Kind: ast.NullValue}, want: "nullval"},
		{name: "unknown kind", value: &ast.Value{Kind: ast.ValueKind(99), Raw: "?"}, want: "unknown:99"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, valueFingerprint(tt.value))
		})
	}
}

func TestValueFingerprintCompositeKinds(t *testing.T) {
	t.Parallel()

	scalar := valueFingerprint(intVal("1"))
	assert.NotEmpty(t, string(scalar))

	list := &ast.Value{Kind: ast.ListValue, Children: ast.ChildValueList{
		nil,
		{Value: intVal("1")},
		{Value: varVal("y")},
	}}
	assert.Equal(t, valueHash("[1:1,$y]"), valueFingerprint(list))

	obj := &ast.Value{Kind: ast.ObjectValue, Children: ast.ChildValueList{
		nil,
		{Name: "b", Value: intVal("2")},
		{Name: "a", Value: varVal("z")},
	}}
	assert.Equal(t, valueHash("{a:$z,b:1:2}"), valueFingerprint(obj))

	assert.Equal(t, objectFingerprint("{}"), objectValueFingerprint(nil))
}

func TestArgumentAndDirectiveFingerprints(t *testing.T) {
	t.Parallel()

	assert.Equal(t, fieldFingerprint("()"), argumentListFingerprint(nil))
	assert.Equal(t, directiveFingerprint("[]"), directiveListFingerprint(nil))

	args := ast.ArgumentList{
		nil,
		{Name: "b", Value: intVal("2")},
		{Name: "a", Value: intVal("1")},
	}
	assert.Equal(t, fieldFingerprint("(a:1:1,b:1:2)"), argumentListFingerprint(args))

	dirs := ast.DirectiveList{
		nil,
		{Name: "skip", Arguments: ast.ArgumentList{{Name: "if", Value: varVal("c")}}},
	}
	assert.Equal(t, directiveFingerprint("[skip:(if:$c)]"), directiveListFingerprint(dirs))
}

func TestClearAndSetAliasesSkipNil(t *testing.T) {
	t.Parallel()

	a := field("f", "x", nil)
	clearFieldAliases([]*ast.Field{nil, a})
	assert.Empty(t, a.Alias)

	b := field("f", "", nil)
	setAliasOnFields([]*ast.Field{nil, b}, "al9")
	assert.Equal(t, "al9", b.Alias)
}

func TestGroupAndBucketSkipNilFields(t *testing.T) {
	t.Parallel()

	var typedNil *ast.Field
	set := ast.SelectionSet{typedNil, &ast.InlineFragment{}, field("f", "", nil)}
	groups := groupDirectFieldsByName(set)
	assert.Len(t, groups["f"], 1)

	buckets := bucketFieldsByMergeFingerprint([]*ast.Field{nil, field("f", "", nil)})
	assert.Len(t, buckets, 1)
}

func TestAssignMergeAliasesForSelectionSet(t *testing.T) {
	t.Parallel()

	t.Run("empty set is a no-op", func(t *testing.T) {
		t.Parallel()
		assignMergeAliasesForSelectionSet(ast.SelectionSet{}, newSeq())
	})

	t.Run("single field alias cleared", func(t *testing.T) {
		t.Parallel()
		f := field("solo", "client", nil)
		assignMergeAliasesForSelectionSet(ast.SelectionSet{f}, newSeq())
		assert.Empty(t, f.Alias)
	})

	t.Run("same fingerprint siblings cleared", func(t *testing.T) {
		t.Parallel()
		a := field("f", "a1", ast.ArgumentList{{Name: "x", Value: intVal("1")}})
		b := field("f", "a2", ast.ArgumentList{{Name: "x", Value: intVal("1")}})
		assignMergeAliasesForSelectionSet(ast.SelectionSet{a, b}, newSeq())
		assert.Empty(t, a.Alias)
		assert.Empty(t, b.Alias)
	})

	t.Run("different fingerprints get sequential alias", func(t *testing.T) {
		t.Parallel()
		a := field("f", "a1", ast.ArgumentList{{Name: "x", Value: intVal("1")}})
		b := field("f", "a2", ast.ArgumentList{{Name: "x", Value: intVal("2")}})
		assignMergeAliasesForSelectionSet(ast.SelectionSet{a, b}, newSeq())
		// One bucket stays alias-free, the other picks up al1.
		assert.True(t, (a.Alias == "" && b.Alias == "al1") || (b.Alias == "" && a.Alias == "al1"))
	})
}

func newSeq() aliasSequence {
	var v int
	return aliasSequence(&v)
}
