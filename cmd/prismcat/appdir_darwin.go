//go:build darwin

package main

import (
	"log"
	"os"
	"path/filepath"
	"strings"
)

// prepareWorkDir 在以 PrismCat.app 形式运行时，把工作目录切到
// ~/Library/Application Support/PrismCat，让 data/ 下的配置、数据库和 blob
// 都落在 macOS 的规范位置。从访达启动的 .app 工作目录是 /，不可写。
//
// 同时把日志写到 data/prismcat.log（每次启动覆盖）：.app 没有终端，
// 标准输出会被丢弃。
//
// 直接运行裸二进制时不做任何改变，行为与 Linux 一致。
func prepareWorkDir() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if !strings.Contains(exe, ".app/Contents/MacOS/") {
		return nil
	}

	base, err := os.UserConfigDir()
	if err != nil {
		return err
	}
	dir := filepath.Join(base, "PrismCat")
	if err := os.MkdirAll(filepath.Join(dir, "data"), 0755); err != nil {
		return err
	}
	if err := os.Chdir(dir); err != nil {
		return err
	}

	logFile, err := os.Create(filepath.Join("data", "prismcat.log"))
	if err != nil {
		return err
	}
	log.SetOutput(logFile)
	log.Printf("工作目录: %s", dir)
	return nil
}
