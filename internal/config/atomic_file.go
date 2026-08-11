//go:build !windows

package config

import "os"

func replaceConfigFile(source, destination string) error {
	return os.Rename(source, destination)
}
