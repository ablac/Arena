// Package version exposes build-time identity for the running server.
// Values are injected at build time via:
//
//	go build -ldflags "-X arena-server/internal/version.Commit=<sha> \
//	                   -X arena-server/internal/version.BuildTime=<iso8601>"
//
// The Dockerfile wires these from the GIT_COMMIT / BUILD_TIME build args so
// the live container always knows which commit it was built from.
package version

import "os"

var (
	// Commit is the full git commit hash the binary was built from.
	Commit = "unknown"
	// BuildTime is the UTC build timestamp (ISO 8601).
	BuildTime = "unknown"
)

// ResolvedCommit returns the build-time commit, falling back to the
// ARENA_GIT_COMMIT environment variable for deployments that inject identity
// at runtime instead of build time.
func ResolvedCommit() string {
	if Commit != "" && Commit != "unknown" {
		return Commit
	}
	if v := os.Getenv("ARENA_GIT_COMMIT"); v != "" {
		return v
	}
	return "unknown"
}

// ShortCommit returns the first 7 characters of the resolved commit hash,
// or the full value when it is already shorter (e.g. "unknown").
func ShortCommit() string {
	c := ResolvedCommit()
	if len(c) > 7 {
		return c[:7]
	}
	return c
}
