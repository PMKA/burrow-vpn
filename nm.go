package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

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

func isEthernetConnected() bool {
	out, err := nmcli("-f", "type,state", "dev")
	if err != nil {
		return false
	}
	for _, line := range strings.Split(out, "\n") {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 && strings.TrimSpace(parts[0]) == "ethernet" && strings.TrimSpace(parts[1]) == "connected" {
			return true
		}
	}
	return false
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

func wgUpWithRetry(name string) error {
	var lastErr error
	for i := 0; i < 3; i++ {
		if i > 0 {
			time.Sleep(2 * time.Second)
		}
		if lastErr = wgUp(name); lastErr == nil {
			return nil
		}
		logf("connect attempt %d failed: %v", i+1, lastErr)
	}
	return lastErr
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

// getWGInterface returns the network interface name for an active WG connection.
func getWGInterface(connName string) string {
	out, _ := nmcli("--fields", "GENERAL.IP-IFACE", "con", "show", connName)
	for _, line := range strings.Split(out, "\n") {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) == 2 {
			if iface := strings.TrimSpace(parts[1]); iface != "" {
				return iface
			}
		}
	}
	return connName // WireGuard typically uses the connection name as interface name
}

// getIfaceStats reads RX/TX bytes for an interface from /proc/net/dev.
func getIfaceStats(iface string) (rx, tx uint64) {
	if iface == "" {
		return
	}
	data, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, iface+":") {
			continue
		}
		fields := strings.Fields(strings.TrimPrefix(line, iface+":"))
		if len(fields) >= 9 {
			fmt.Sscan(fields[0], &rx)
			fmt.Sscan(fields[8], &tx)
		}
		return
	}
	return
}

// blockIPv6 drops all IPv6 output except on the given interface and loopback.
func blockIPv6(iface string) error {
	exec.Command("sudo", "ip6tables", "-I", "OUTPUT", "-o", "lo", "-j", "ACCEPT").Run()
	return exec.Command("sudo", "ip6tables", "-I", "OUTPUT", "!", "-o", iface, "-j", "DROP").Run()
}

func unblockIPv6(iface string) error {
	exec.Command("sudo", "ip6tables", "-D", "OUTPUT", "!", "-o", iface, "-j", "DROP").Run()
	exec.Command("sudo", "ip6tables", "-D", "OUTPUT", "-o", "lo", "-j", "ACCEPT").Run()
	return nil
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
