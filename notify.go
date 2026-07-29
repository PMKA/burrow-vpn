package main

import "os/exec"

func notify(title, body string) {
	exec.Command("notify-send", "-a", "Burrow", "-i", "burrow", "--", title, body).Run()
}
