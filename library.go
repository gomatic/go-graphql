// This file flags the repo as a library, not a CLI app.
//
// Nothing ever sets the library_marker build tag, so this file never compiles —
// it only needs to exist. gomatic tooling and conventions tell a library repo
// from a CLI repo by whether this marker file is here, so don't remove it.

//go:build library_marker

package graphql
