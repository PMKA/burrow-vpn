package main

import (
	"os"
	"path/filepath"
	"time"

	"github.com/getlantern/systray"
	"github.com/gotk3/gotk3/glib"
	"github.com/gotk3/gotk3/gtk"
)

type App struct {
	cfg         Config
	settingsWin *SettingsWindow
	mStatus     *systray.MenuItem
	mConnect    *systray.MenuItem
	mDisconnect *systray.MenuItem
}

func iconPath() string {
	exe, _ := os.Executable()
	return filepath.Join(filepath.Dir(exe), "icons", "burrow.svg")
}

func readIcon() []byte {
	// Try relative to executable first, then source dir
	for _, p := range []string{
		iconPath(),
		"icons/burrow.svg",
	} {
		if data, err := os.ReadFile(p); err == nil {
			return data
		}
	}
	return nil
}

func main() {
	gtk.Init(nil)
	systray.Run(onReady, onExit)
}

func onReady() {
	initLogger()
	cfg := loadConfig()

	// First-run wizard when no VPN is configured yet
	if cfg.WGConnection == "" {
		win, _ := gtk.WindowNew(gtk.WINDOW_TOPLEVEL)
		win.Hide()
		cfg = runSetupWizard(win, cfg)
		saveConfig(cfg)
		win.Destroy()
	}

	app := &App{cfg: cfg}

	if icon := readIcon(); icon != nil {
		systray.SetIcon(icon)
	}
	systray.SetTitle("Burrow")
	systray.SetTooltip("Burrow")

	app.mStatus = systray.AddMenuItem("Status: checking…", "")
	app.mStatus.Disable()
	systray.AddSeparator()
	app.mConnect = systray.AddMenuItem("Connect VPN", "")
	app.mDisconnect = systray.AddMenuItem("Disconnect VPN", "")
	systray.AddSeparator()
	mSettings := systray.AddMenuItem("Settings…", "")
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("Quit", "")

	// Subscribe to NetworkManager events
	subscribeNMEvents(func() {
		time.Sleep(500 * time.Millisecond)
		glib.IdleAdd(func() bool {
			app.updateStatus()
			return false
		})
	})

	// GTK main loop in background
	go func() {
		gtk.Main()
	}()

	app.updateStatus()

	// Menu event loop
	for {
		select {
		case <-app.mConnect.ClickedCh:
			if app.cfg.WGConnection != "" {
				logf("user: connecting %s", app.cfg.WGConnection)
				wgUp(app.cfg.WGConnection)
				time.Sleep(1 * time.Second)
				glib.IdleAdd(func() bool { app.updateStatus(); return false })
			}
		case <-app.mDisconnect.ClickedCh:
			if app.cfg.WGConnection != "" {
				logf("user: disconnecting %s", app.cfg.WGConnection)
				wgDown(app.cfg.WGConnection)
				time.Sleep(1 * time.Second)
				glib.IdleAdd(func() bool { app.updateStatus(); return false })
			}
		case <-mSettings.ClickedCh:
			glib.IdleAdd(func() bool {
				if app.settingsWin == nil {
					app.settingsWin = newSettingsWindow(app)
				} else {
					app.settingsWin.show()
				}
				return false
			})
		case <-mQuit.ClickedCh:
			gtk.MainQuit()
			systray.Quit()
			return
		}
	}
}

func (app *App) updateStatus() {
	conn := app.cfg.WGConnection
	ssid := getCurrentSSID()
	trusted := ssid != "" && app.cfg.isTrusted(ssid)
	connected := getWGStatus(conn)

	switch {
	case conn == "":
		app.mStatus.SetTitle("Status: no VPN configured")
		systray.SetTooltip("Burrow — no VPN configured")
	case connected:
		app.mStatus.SetTitle("Status: VPN on (" + conn + ")")
		systray.SetTooltip("Burrow — VPN connected")
	case trusted:
		app.mStatus.SetTitle("Status: trusted network (" + ssid + ")")
		systray.SetTooltip("Burrow — trusted network")
	case ssid != "":
		app.mStatus.SetTitle("Status: untrusted (" + ssid + ") — VPN off")
		systray.SetTooltip("Burrow — untrusted network")
	default:
		app.mStatus.SetTitle("Status: no WiFi")
		systray.SetTooltip("Burrow — no WiFi")
	}

	if connected {
		app.mConnect.Disable()
		app.mDisconnect.Enable()
	} else {
		app.mDisconnect.Disable()
		if conn != "" {
			app.mConnect.Enable()
		} else {
			app.mConnect.Disable()
		}
	}

	// Auto-connect logic
	if app.cfg.AutoConnect && conn != "" && ssid != "" {
		if !trusted && !connected {
			go func() {
				logf("auto: connecting %s (untrusted network %q)", conn, ssid)
				wgUp(conn)
				time.Sleep(1 * time.Second)
				glib.IdleAdd(func() bool { app.updateStatus(); return false })
			}()
		} else if trusted && connected {
			go func() {
				logf("auto: disconnecting %s (trusted network %q)", conn, ssid)
				wgDown(conn)
				time.Sleep(1 * time.Second)
				glib.IdleAdd(func() bool { app.updateStatus(); return false })
			}()
		}
	}
}

func onExit() {}
