package graphql

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseValid(t *testing.T) {
	t.Parallel()
	doc, err := Parse("test", "type Query { hello: String }")
	require.NoError(t, err)
	require.NotNil(t, doc)
	require.Len(t, doc.Definitions, 1)
	assert.Equal(t, "Query", doc.Definitions[0].Name)
}

func TestParseInvalidWrapsErrParse(t *testing.T) {
	t.Parallel()
	_, err := Parse("test", "type Query {{{")
	require.Error(t, err)
	assert.ErrorIs(t, err, ErrParse)
}
