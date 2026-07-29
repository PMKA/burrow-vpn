package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/getlantern/systray"
	"github.com/gotk3/gotk3/glib"
	"github.com/gotk3/gotk3/gtk"
)

type App struct {
	cfg            Config
	settingsWin    *SettingsWindow
	mStatus        *systray.MenuItem
	mConnect       *systray.MenuItem
	mDisconnect    *systray.MenuItem
	mPauseMenu     *systray.MenuItem // visible when not paused
	mResume        *systray.MenuItem // visible when paused
	pauseUntil     time.Time
	connectedSince time.Time
	wgIface        string
	ipv6Blocked    bool
}

func iconDir() string {
	exe, _ := os.Executable()
	return filepath.Join(filepath.Dir(exe), "icons")
}

func readIconFile(name string) []byte {
	for _, dir := range []string{iconDir(), "icons"} {
		if data, err := os.ReadFile(filepath.Join(dir, name)); err == nil {
			return data
		}
	}
	return nil
}

func main() {
	if !acquireLock() {
		notify("Burrow VPN", "Already running in the system tray.")
		return
	}
	defer releaseLock()
	gtk.Init(nil)
	systray.Run(onReady, onExit)
}

func onReady() {
	initLogger()
	cfg := loadConfig()

	if cfg.WGConnection == "" {
		win, _ := gtk.WindowNew(gtk.WINDOW_TOPLEVEL)
		win.Hide()
		var skipped bool
		cfg, skipped = runSetupWizard(win, cfg)
		if !skipped {
			saveConfig(cfg)
		}
		win.Destroy()
	}

	app := &App{cfg: cfg}

	startUpdateChecker(app)

	if icon := readIconFile("burrow-off.png"); icon != nil {
		systray.SetIcon(icon)
	}
	systray.SetTitle("Burrow VPN")
	systray.SetTooltip("Burrow VPN")

	// Status (disabled, informational)
	app.mStatus = systray.AddMenuItem("Status: checking…", "")
	app.mStatus.Disable()
	systray.AddSeparator()

	// Connect / Disconnect
	app.mConnect = systray.AddMenuItem("Connect VPN", "")
	app.mDisconnect = systray.AddMenuItem("Disconnect VPN", "")
	systray.AddSeparator()

	// Profile switcher — only shown when 2+ WireGuard connections exist
	conns := listWGConnections()
	if len(conns) > 1 {
		mProfile := systray.AddMenuItem("Switch Profile", "")
		for _, c := range conns {
			name := c
			sub := mProfile.AddSubMenuItem(name, "")
			go func() {
				for range sub.ClickedCh {
					app.cfg.WGConnection = name
					saveConfig(app.cfg)
					logf("switched profile to %s", name)
					glib.IdleAdd(func() bool { app.updateStatus(); return false })
				}
			}()
		}
		systray.AddSeparator()
	}

	// Pause auto-connect submenu
	app.mPauseMenu = systray.AddMenuItem("Pause auto-connect", "")
	for _, opt := range []struct {
		label string
		dur   time.Duration
	}{
		{"30 minutes", 30 * time.Minute},
		{"1 hour", time.Hour},
		{"4 hours", 4 * time.Hour},
		{"Until restart", 0},
	} {
		d := opt.dur
		label := opt.label
		sub := app.mPauseMenu.AddSubMenuItem(label, "")
		go func() {
			for range sub.ClickedCh {
				app.applyPause(d)
			}
		}()
	}

	// Resume item (hidden until paused)
	app.mResume = systray.AddMenuItem("Resume auto-connect", "")
	app.mResume.Hide()
	go func() {
		for range app.mResume.ClickedCh {
			app.pauseUntil = time.Time{}
			app.mResume.Hide()
			app.mPauseMenu.Show()
			logf("auto-connect resumed")
			notify("Burrow VPN", "Auto-connect resumed")
			glib.IdleAdd(func() bool { app.updateStatus(); return false })
		}
	}()

	systray.AddSeparator()
	mSettings := systray.AddMenuItem("Settings…", "")
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("Quit", "")

	// NetworkManager D-Bus events
	subscribeNMEvents(func() {
		time.Sleep(500 * time.Millisecond)
		glib.IdleAdd(func() bool { app.updateStatus(); return false })
	})

	// Ticker to refresh connection duration and byte stats
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for range ticker.C {
			glib.IdleAdd(func() bool { app.updateStatus(); return false })
		}
	}()

	go gtk.Main()

	app.updateStatus()

	for {
		select {
		case <-app.mConnect.ClickedCh:
			if app.cfg.WGConnection != "" {
				logf("user: connecting %s", app.cfg.WGConnection)
				conn := app.cfg.WGConnection
				go func() {
					if err := wgUpWithRetry(conn); err != nil {
						notify("Burrow VPN", "Failed to connect: "+err.Error())
						logf("connect failed: %v", err)
						return
					}
					notify("Burrow VPN", "Connected to "+conn)
					app.applyIPv6KillSwitch(conn)
					time.Sleep(time.Second)
					glib.IdleAdd(func() bool { app.updateStatus(); return false })
				}()
			}

		case <-app.mDisconnect.ClickedCh:
			if app.cfg.WGConnection != "" {
				logf("user: disconnecting %s", app.cfg.WGConnection)
				wgDown(app.cfg.WGConnection)
				app.teardownIPv6KillSwitch()
				time.Sleep(time.Second)
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
			app.teardownIPv6KillSwitch()
			gtk.MainQuit()
			systray.Quit()
			return
		}
	}
}

func (app *App) applyPause(d time.Duration) {
	if d == 0 {
		app.pauseUntil = time.Now().Add(365 * 24 * time.Hour)
		logf("auto-connect paused until restart")
		notify("Burrow VPN", "Auto-connect paused until restart")
	} else {
		app.pauseUntil = time.Now().Add(d)
		logf("auto-connect paused for %v", d)
		notify("Burrow VPN", fmt.Sprintf("Auto-connect paused for %s", formatDuration(d)))
	}
	app.mPauseMenu.Hide()
	app.mResume.Show()
	glib.IdleAdd(func() bool { app.updateStatus(); return false })
}

func (app *App) applyIPv6KillSwitch(conn string) {
	if !app.cfg.IPv6KillSwitch || app.ipv6Blocked {
		return
	}
	iface := getWGInterface(conn)
	if iface == "" {
		return
	}
	if err := blockIPv6(iface); err != nil {
		logf("IPv6 kill switch failed: %v", err)
		notify("Burrow VPN", "IPv6 kill switch failed — check sudo permissions")
		return
	}
	app.ipv6Blocked = true
	app.wgIface = iface
	logf("IPv6 blocked on non-%s interfaces", iface)
}

func (app *App) teardownIPv6KillSwitch() {
	if app.ipv6Blocked {
		unblockIPv6(app.wgIface)
		app.ipv6Blocked = false
		logf("IPv6 unblocked")
	}
}

func (app *App) updateStatus() {
	conn := app.cfg.WGConnection
	ssid := getCurrentSSID()
	onEthernet := app.cfg.TrustEthernet && isEthernetConnected()
	trusted := (ssid != "" && app.cfg.isTrusted(ssid)) || onEthernet
	connected := getWGStatus(conn)
	paused := !app.pauseUntil.IsZero() && time.Now().Before(app.pauseUntil)

	// Clear expired pause
	if !app.pauseUntil.IsZero() && !paused {
		app.pauseUntil = time.Time{}
		app.mResume.Hide()
		app.mPauseMenu.Show()
		logf("pause expired, auto-connect resumed")
	}

	// Track connection start time
	if connected && app.connectedSince.IsZero() {
		app.connectedSince = time.Now()
		if app.wgIface == "" {
			app.wgIface = getWGInterface(conn)
		}
	} else if !connected {
		app.connectedSince = time.Time{}
		app.wgIface = ""
	}

	// Build status label
	switch {
	case conn == "":
		app.mStatus.SetTitle("No VPN configured")
		systray.SetTooltip("Burrow VPN — no VPN configured")
	case connected:
		dur := ""
		if !app.connectedSince.IsZero() {
			dur = " — " + formatDuration(time.Since(app.connectedSince))
		}
		rx, tx := getIfaceStats(app.wgIface)
		stats := ""
		if app.wgIface != "" && (rx > 0 || tx > 0) {
			stats = fmt.Sprintf(" (↑%s ↓%s)", formatBytes(tx), formatBytes(rx))
		}
		app.mStatus.SetTitle(fmt.Sprintf("VPN on (%s)%s%s", conn, dur, stats))
		systray.SetTooltip("Burrow VPN — VPN connected")
	case paused:
		remaining := time.Until(app.pauseUntil)
		app.mStatus.SetTitle(fmt.Sprintf("Paused — %s remaining", formatDuration(remaining)))
		systray.SetTooltip("Burrow VPN — auto-connect paused")
	case onEthernet:
		app.mStatus.SetTitle("Trusted network (wired)")
		systray.SetTooltip("Burrow VPN — trusted wired network")
	case trusted:
		app.mStatus.SetTitle("Trusted network (" + ssid + ")")
		systray.SetTooltip("Burrow VPN — trusted network")
	case ssid != "":
		app.mStatus.SetTitle("Untrusted (" + ssid + ") — VPN off")
		systray.SetTooltip("Burrow VPN — untrusted network")
	default:
		app.mStatus.SetTitle("No network")
		systray.SetTooltip("Burrow VPN — no network")
	}

	// Swap tray icon based on connection state
	iconName := "burrow-off.png"
	if connected {
		iconName = "burrow-on.png"
	}
	if icon := readIconFile(iconName); icon != nil {
		systray.SetIcon(icon)
	}

	// Connect / Disconnect sensitivity
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
	if app.cfg.AutoConnect && conn != "" && !paused {
		if !trusted && !connected {
			go func() {
				logf("auto: connecting %s (untrusted %q)", conn, ssid)
				if err := wgUpWithRetry(conn); err != nil {
					notify("Burrow VPN", "Auto-connect failed: "+err.Error())
					logf("auto-connect failed: %v", err)
					return
				}
				notify("Burrow VPN", "Connected to "+conn)
				app.applyIPv6KillSwitch(conn)
				time.Sleep(time.Second)
				glib.IdleAdd(func() bool { app.updateStatus(); return false })
			}()
		} else if trusted && connected {
			go func() {
				logf("auto: disconnecting %s (trusted %q)", conn, ssid)
				wgDown(conn)
				notify("Burrow VPN", "Disconnected from "+conn)
				app.teardownIPv6KillSwitch()
				time.Sleep(time.Second)
				glib.IdleAdd(func() bool { app.updateStatus(); return false })
			}()
		}
	}
}

func formatDuration(d time.Duration) string {
	d = d.Round(time.Minute)
	if d < time.Minute {
		d = time.Minute
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh %dm", h, m)
	}
	return fmt.Sprintf("%dm", m)
}

func formatBytes(b uint64) string {
	const (
		KB = 1024
		MB = 1024 * KB
		GB = 1024 * MB
	)
	switch {
	case b >= GB:
		return fmt.Sprintf("%.1fGB", float64(b)/float64(GB))
	case b >= MB:
		return fmt.Sprintf("%.1fMB", float64(b)/float64(MB))
	case b >= KB:
		return fmt.Sprintf("%.1fKB", float64(b)/float64(KB))
	default:
		return fmt.Sprintf("%dB", b)
	}
}

func onExit() {}
