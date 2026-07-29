package main

import (
	"os/exec"
	"strings"

	"github.com/godbus/dbus/v5"
)

func nmcli(args ...string) (string, error) {
	out, err := exec.Command("nmcli", append([]string{"--terse"}, args...)...).Output()
	return strings.TrimSpace(string(out)), err
}

func getCurrentSSID() string {
	out, err := nmcli("-f", "active,ssid", "dev", "wifi")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "yes:") {
			return strings.SplitN(line, ":", 2)[1]
		}
	}
	return ""
}

func getWGStatus(name string) bool {
	if name == "" {
		return false
	}
	out, err := nmcli("-f", "name,state", "con", "show", "--active")
	if err != nil {
		return false
	}
	for _, line := range strings.Split(out, "\n") {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 && parts[0] == name {
			return true
		}
	}
	return false
}

func wgUp(name string) error {
	return exec.Command("nmcli", "con", "up", name).Run()
}

func wgDown(name string) error {
	return exec.Command("nmcli", "con", "down", name).Run()
}

func importWGConfig(path string) (string, error) {
	err := exec.Command("nmcli", "con", "import", "type", "wireguard", "file", path).Run()
	if err != nil {
		return "", err
	}
	parts := strings.Split(path, "/")
	name := strings.TrimSuffix(parts[len(parts)-1], ".conf")
	return name, nil
}

func listWGConnections() []string {
	out, err := nmcli("-f", "name,type", "con", "show")
	if err != nil {
		return nil
	}
	var result []string
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(strings.ToLower(line), "wireguard") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) >= 1 {
				result = append(result, parts[0])
			}
		}
	}
	return result
}

func subscribeNMEvents(onChange func()) (*dbus.Conn, error) {
	conn, err := dbus.SystemBus()
	if err != nil {
		return nil, err
	}
	conn.BusObject().Call(
		"org.freedesktop.DBus.AddMatch", 0,
		"type='signal',interface='org.freedesktop.NetworkManager',member='StateChanged'",
	)
	go func() {
		ch := make(chan *dbus.Message, 10)
		conn.Eavesdrop(ch)
		for range ch {
			onChange()
		}
	}()
	return conn, nil
}
