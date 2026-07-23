// Package e2e holds end-to-end tests that drive the built atlantis-mcp binary
// against a real Atlantis server. The tests are gated behind the `e2e` build
// tag (see e2e_test.go); without it this package is intentionally empty so that
// `go build ./...` and `go vet ./...` succeed in the normal unit-test lane.
package e2e
