package normalize

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResolveOperation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		wantErr      error
		name         string
		raw          QueryInput
		preferred    OperationName
		wantName     OperationName
		wantInQuery  string
		wantHashedOp bool
	}{
		{
			name:        "single anonymous operation gets preferred name",
			raw:         `query { bom { id } }`,
			preferred:   "getbom",
			wantName:    "getbom",
			wantInQuery: "query getbom",
		},
		{
			name:        "preferred name with hyphens is sanitized",
			raw:         `{ bom { id } }`,
			preferred:   "falcon-instance-summarize",
			wantName:    "falcon_instance_summarize",
			wantInQuery: "query falcon_instance_summarize",
		},
		{
			name:         "empty preferred falls back to query hash",
			raw:          `query { bom { id } }`,
			preferred:    "",
			wantHashedOp: true,
		},
		{name: "empty query returns error", raw: "", preferred: "x", wantErr: ErrEmptyQuery},
		{name: "unparseable query returns parse error", raw: `query { bom (`, preferred: "x", wantErr: ErrQueryParse},
		{
			name:        "multi-operation document keeps matching preferred name",
			raw:         `query A { a } query B { b }`,
			preferred:   "B",
			wantName:    "B",
			wantInQuery: "query B",
		},
		{
			name:      "multi-operation document with non-matching preferred omits name",
			raw:       `query A { a } query B { b }`,
			preferred: "C",
			wantName:  "",
		},
		{name: "zero operations returns empty name", raw: `fragment F on T { id }`, preferred: "x", wantName: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			gotQuery, gotName, err := ResolveOperation(tt.raw, tt.preferred)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)

			if tt.wantHashedOp {
				assert.True(t, strings.HasPrefix(string(gotName), operationHashPrefix))
				_, gotName2, err2 := ResolveOperation(tt.raw, tt.preferred)
				require.NoError(t, err2)
				assert.Equal(t, gotName, gotName2)
				return
			}

			assert.Equal(t, tt.wantName, gotName)
			if tt.wantInQuery != "" {
				assert.Contains(t, string(gotQuery), tt.wantInQuery)
			}
		})
	}
}

func TestSanitizeName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   OperationName
		want OperationName
	}{
		{name: "empty stays empty", in: "", want: ""},
		{name: "valid name unchanged", in: "GetBom", want: "GetBom"},
		{name: "hyphens become underscores", in: "get-bom", want: "get_bom"},
		{name: "dots and slashes become underscores", in: "a.b/c", want: "a_b_c"},
		{name: "leading digit is prefixed", in: "1op", want: "_1op"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, sanitizeName(tt.in))
		})
	}
}
