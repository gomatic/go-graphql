// Package schema composes and inspects GraphQL schemas. It builds field-lookup
// indexes from SDL ([Index]), routes a query's root fields across a set of
// schemas you name ([Composite]), and turns GraphQL introspection responses
// into SDL ([IntrospectionToSDL]).
//
// You supply the schema names — the package bakes in no fixed set. You own all
// persistence too; the package works purely on in-memory SDL and introspection
// JSON and keeps no shared mutable state.
package schema

// Schema names a GraphQL schema. You define what's a member; the package
// imposes no fixed set of names.
type Schema string
