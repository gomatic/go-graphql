package schema

import (
	"encoding/json"
	"strings"

	graphql "github.com/gomatic/go-graphql"
)

// IntrospectionJSON is a raw GraphQL introspection JSON response. We take both
// the standard camelCase keys (__schema, ofType, …) and fmtctl-style snake_case
// aliases (schema, of_type, …) and canonicalize either shape.
type IntrospectionJSON []byte

// IntrospectionToSDL turns a GraphQL introspection response into SDL text,
// dropping built-in scalars and directives. It fails with [ErrIntrospectionParse]
// on malformed JSON, [ErrIntrospectionGraphQLErrors] when the response carries
// GraphQL errors, and [ErrIntrospectionEmpty] when it declares no types.
func IntrospectionToSDL(data IntrospectionJSON) (graphql.SDL, error) {
	results, err := decodeIntrospection(data)
	if err != nil {
		return "", err
	}
	if len(results.Errors) > 0 {
		return "", introspectionResponseError(results.Errors)
	}
	if len(results.Data.Schema.Types) == 0 {
		return "", ErrIntrospectionEmpty.With(nil)
	}
	sdl, err := printIntrospectionSchema(results.Data.Schema)
	if err != nil {
		return "", err
	}
	return graphql.SDL(sdl), nil
}

// IndexFromIntrospection builds an [Index] straight from an introspection
// response: it converts the response to SDL ([IntrospectionToSDL]) and loads it
// ([NewIndex]).
func IndexFromIntrospection(name Schema, data IntrospectionJSON) (Index, error) {
	sdl, err := IntrospectionToSDL(data)
	if err != nil {
		return nil, err
	}
	return NewIndex(name, sdl)
}

// decodeIntrospection unmarshals the response, canonicalizes camelCase keys to
// snake_case, then decodes the result into the typed introspection envelope.
func decodeIntrospection(data IntrospectionJSON) (introspectionResults, error) {
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return introspectionResults{}, ErrIntrospectionParse.With(err)
	}
	unifyIntrospectionEnvelope(raw)
	canonicalizeIntrospectionResponse(raw)
	fixed, _ := json.Marshal(raw) // raw came from JSON, so re-marshaling it can't fail
	var results introspectionResults
	if err := json.Unmarshal(fixed, &results); err != nil {
		return introspectionResults{}, ErrIntrospectionParse.With(err)
	}
	return results, nil
}

func introspectionResponseError(items []introspectionError) error {
	msgs := make([]string, 0, len(items))
	for _, e := range items {
		msgs = append(msgs, string(e.Message))
	}
	return ErrIntrospectionGraphQLErrors.With(nil, "messages", strings.Join(msgs, ", "))
}
