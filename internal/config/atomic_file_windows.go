package config

import "golang.org/x/sys/windows"

func replaceConfigFile(source, destination string) error {
	return windows.Rename(source, destination)
}
