# Embedded UI placeholder

This directory must exist in a clean checkout because `internal/server/server.go`
uses `//go:embed all:ui`.

Release builds copy `web/dist` into this directory before compiling the Go
binary. The generated UI assets are ignored by Git; keep this file tracked.

For local Windows builds, use `docs/快速编译并运行.bat` so ignored generated
assets are cleaned without deleting this placeholder.
