package main

import "os/exec"

func notify(title, body string) {
	exec.Command("notify-send", "-a", "Burrow VPN", "-i", "burrow-vpn", "--", title, body).Run()
}
