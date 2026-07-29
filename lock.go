package main

import (
	"fmt"
	"net"
	"os"
)

var instanceLock net.Listener

// acquireLock tries to bind a Unix socket to prevent multiple instances.
// Returns false if another instance is already running.
func acquireLock() bool {
	path := lockPath()
	// If we can connect, something already owns the socket — another instance is running.
	if c, err := net.Dial("unix", path); err == nil {
		c.Close()
		return false
	}
	// Remove stale socket from a previous crash.
	os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		// Can't create socket — allow startup rather than permanently blocking the app.
		logf("warning: could not acquire instance lock: %v", err)
		return true
	}
	instanceLock = ln
	return true
}

func releaseLock() {
	if instanceLock != nil {
		instanceLock.Close()
		os.Remove(lockPath())
	}
}

func lockPath() string {
	return fmt.Sprintf("/run/user/%d/burrow-vpn.lock", os.Getuid())
}
