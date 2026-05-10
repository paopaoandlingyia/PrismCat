# Embedded UI placeholder

This directory must exist in a clean checkout because `internal/server/server.go`
uses `//go:embed all:ui`.

Release builds copy `web/dist` into this directory before compiling the Go
binary. The generated UI assets are ignored by Git; keep this file tracked.

For local builds, use `scripts/sync-ui.ps1` so ignored generated assets are
cleaned without deleting this placeholder.
