package main

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
)

const maxLogSize = 1 << 20 // 1 MB

var logger *log.Logger

func initLogger() {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".local", "share", "burrow-vpn")
	os.MkdirAll(dir, 0755)
	path := filepath.Join(dir, "app.log")

	rotateLogs(path)

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		logger = log.New(os.Stderr, "", log.LstdFlags)
		return
	}
	logger = log.New(f, "", log.LstdFlags)
	logf("burrow started")
}

func rotateLogs(path string) {
	info, err := os.Stat(path)
	if err != nil || info.Size() < maxLogSize {
		return
	}
	os.Rename(path, path+".old")
}

func logf(format string, args ...interface{}) {
	if logger != nil {
		logger.Output(2, fmt.Sprintf(format, args...))
	}
}
