package normalize

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/vektah/gqlparser/v2/gqlerror"
)

func TestFormatGQLErrors(t *testing.T) {
	t.Parallel()

	errs := gqlerror.List{
		nil,
		gqlerror.Errorf("first problem"),
		gqlerror.Errorf("second problem"),
	}
	got := string(formatGQLErrors(errs))
	assert.Contains(t, got, "first problem")
	assert.Contains(t, got, "second problem")
	assert.Contains(t, got, "; ")
}

func TestQueryUsesBuiltinIntrospectionNilDocument(t *testing.T) {
	t.Parallel()

	assert.False(t, bool(queryUsesBuiltinIntrospection(nil)))
}

func TestQueryUsesBuiltinIntrospectionDeduplicatesFragmentSpreads(t *testing.T) {
	t.Parallel()

	// F is spread under two siblings; the seen-set has to short-circuit the
	// second visit so we don't walk F's body again.
	doc := mustParseQuery(t, `query { a { ...F } b { ...F } } fragment F on R { id }`)
	assert.False(t, bool(queryUsesBuiltinIntrospection(doc)))
}

func TestQueryUsesBuiltinIntrospectionDetection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		query string
		want  bool
	}{
		{name: "nested field", query: `query { wrapper { __typename } }`, want: true},
		{name: "inside inline fragment", query: `query { a { ... on T { __typename } } }`, want: true},
		{name: "inside fragment spread", query: `query { a { ...F } } fragment F on R { __typename }`, want: true},
		{
			name:  "unused fragment with introspection",
			query: `query { a { id } } fragment F on R { __typename }`,
			want:  true,
		},
		{name: "missing fragment spread", query: `query { a { ...Missing } }`, want: false},
		{name: "no introspection", query: `query { a { id } }`, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			doc := mustParseQuery(t, tt.query)
			assert.Equal(t, tt.want, bool(queryUsesBuiltinIntrospection(doc)))
		})
	}
}
