//go:build !windows

package main

import (
	"github.com/prismcat/prismcat/internal/config"
	"github.com/prismcat/prismcat/internal/server"
)

func platformRun(srv *server.Server, _ *config.Config, _ bool) error {
	return srv.Start()
}
