package main

import (
	"github.com/gotk3/gotk3/gtk"
)

type SettingsWindow struct {
	win      *gtk.Window
	app      *App
	ssidList *gtk.ListBox
}

func newSettingsWindow(app *App) *SettingsWindow {
	win, _ := gtk.WindowNew(gtk.WINDOW_TOPLEVEL)
	win.SetTitle("Burrow VPN — Settings")
	win.SetDefaultSize(480, 420)
	win.SetBorderWidth(16)
	win.SetResizable(false)

	sw := &SettingsWindow{win: win, app: app}

	root, _ := gtk.BoxNew(gtk.ORIENTATION_VERTICAL, 16)
	win.Add(root)

	// WireGuard connection
	wgFrame, _ := gtk.FrameNew(" WireGuard Connection ")
	wgBox, _ := gtk.BoxNew(gtk.ORIENTATION_VERTICAL, 8)
	wgBox.SetBorderWidth(10)
	wgFrame.Add(wgBox)

	connRow, _ := gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 8)
	connLabel, _ := gtk.LabelNew("Connection:")
	combo, _ := gtk.ComboBoxTextNew()
	combo.SetHExpand(true)
	sw.refreshConnections(combo)
	combo.Connect("changed", func() {
		app.cfg.WGConnection = combo.GetActiveText()
		saveConfig(app.cfg)
		app.updateStatus()
	})
	connRow.PackStart(connLabel, false, false, 0)
	connRow.PackStart(combo, true, true, 0)
	wgBox.PackStart(connRow, false, false, 0)

	importRow, _ := gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 8)
	importBtn, _ := gtk.ButtonNewWithLabel("Import .conf file…")
	importStatus, _ := gtk.LabelNew("")
	importBtn.Connect("clicked", func() {
		dlg, _ := gtk.FileChooserDialogNewWith2Buttons(
			"Select WireGuard .conf",
			win,
			gtk.FILE_CHOOSER_ACTION_OPEN,
			"Cancel", gtk.RESPONSE_CANCEL,
			"Open", gtk.RESPONSE_OK,
		)
		filter, _ := gtk.FileFilterNew()
		filter.SetName("WireGuard config (*.conf)")
		filter.AddPattern("*.conf")
		dlg.AddFilter(filter)

		if dlg.Run() == gtk.RESPONSE_OK {
			path := dlg.GetFilename()
			dlg.Destroy()
			name, err := importWGConfig(path)
			if err != nil {
				importStatus.SetText("Import failed: " + err.Error())
			} else {
				app.cfg.WGConnection = name
				saveConfig(app.cfg)
				sw.refreshConnections(combo)
				importStatus.SetText("Imported: " + name)
				app.updateStatus()
			}
		} else {
			dlg.Destroy()
		}
	})
	importRow.PackStart(importBtn, false, false, 0)
	importRow.PackStart(importStatus, false, false, 0)
	wgBox.PackStart(importRow, false, false, 0)

	root.PackStart(wgFrame, false, false, 0)

	// Trusted SSIDs
	ssidFrame, _ := gtk.FrameNew(" Trusted WiFi Networks ")
	ssidBox, _ := gtk.BoxNew(gtk.ORIENTATION_VERTICAL, 8)
	ssidBox.SetBorderWidth(10)
	ssidFrame.Add(ssidBox)

	listBox, _ := gtk.ListBoxNew()
	listBox.SetSelectionMode(gtk.SELECTION_SINGLE)
	sw.ssidList = listBox
	for _, s := range app.cfg.TrustedSSIDs {
		sw.appendSSIDRow(s)
	}

	scroll, _ := gtk.ScrolledWindowNew(nil, nil)
	scroll.SetMinContentHeight(130)
	scroll.SetPolicy(gtk.POLICY_NEVER, gtk.POLICY_AUTOMATIC)
	scroll.Add(listBox)
	ssidBox.PackStart(scroll, true, true, 0)

	btnRow, _ := gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 8)
	addCurBtn, _ := gtk.ButtonNewWithLabel("Add Current Network")
	addCurBtn.Connect("clicked", func() {
		if ssid := getCurrentSSID(); ssid != "" {
			app.cfg.addTrusted(ssid)
			saveConfig(app.cfg)
			sw.appendSSIDRow(ssid)
			app.updateStatus()
		}
	})
	addBtn, _ := gtk.ButtonNewWithLabel("Add…")
	addBtn.Connect("clicked", func() {
		dlg, _ := gtk.DialogNewWithButtons("Add Trusted Network", win,
			gtk.DIALOG_MODAL,
			[]interface{}{"Cancel", gtk.RESPONSE_CANCEL, "Add", gtk.RESPONSE_OK},
		)
		entry, _ := gtk.EntryNew()
		entry.SetPlaceholderText("Network name (SSID)")
		entry.SetMarginStart(12)
		entry.SetMarginEnd(12)
		entry.SetMarginTop(12)
		entry.SetMarginBottom(12)
		ca, _ := dlg.GetContentArea()
		ca.Add(entry)
		dlg.ShowAll()
		if dlg.Run() == gtk.RESPONSE_OK {
			if text, _ := entry.GetText(); text != "" {
				app.cfg.addTrusted(text)
				saveConfig(app.cfg)
				sw.appendSSIDRow(text)
				app.updateStatus()
			}
		}
		dlg.Destroy()
	})
	removeBtn, _ := gtk.ButtonNewWithLabel("Remove")
	removeBtn.Connect("clicked", func() {
		row := listBox.GetSelectedRow()
		if row == nil {
			return
		}
		child, _ := row.GetChild()
		if lbl, ok := child.(*gtk.Label); ok {
			text, _ := lbl.GetText()
			app.cfg.removeTrusted(text)
			saveConfig(app.cfg)
			listBox.Remove(row)
			app.updateStatus()
		}
	})
	btnRow.PackStart(addCurBtn, false, false, 0)
	btnRow.PackStart(addBtn, false, false, 0)
	btnRow.PackStart(removeBtn, false, false, 0)
	ssidBox.PackStart(btnRow, false, false, 0)

	root.PackStart(ssidFrame, true, true, 0)

	// Auto-connect toggle
	autoRow, _ := gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 8)
	autoLabel, _ := gtk.LabelNew("Auto-connect on untrusted networks")
	autoSwitch, _ := gtk.SwitchNew()
	autoSwitch.SetActive(app.cfg.AutoConnect)
	autoSwitch.Connect("notify::active", func() {
		app.cfg.AutoConnect = autoSwitch.GetActive()
		saveConfig(app.cfg)
		app.updateStatus()
	})
	autoRow.PackStart(autoLabel, true, true, 0)
	autoRow.PackEnd(autoSwitch, false, false, 0)
	root.PackStart(autoRow, false, false, 0)

	// Trust ethernet toggle
	ethRow, _ := gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 8)
	ethLabel, _ := gtk.LabelNew("Trust wired (ethernet) connections")
	ethSwitch, _ := gtk.SwitchNew()
	ethSwitch.SetActive(app.cfg.TrustEthernet)
	ethSwitch.Connect("notify::active", func() {
		app.cfg.TrustEthernet = ethSwitch.GetActive()
		saveConfig(app.cfg)
		app.updateStatus()
	})
	ethRow.PackStart(ethLabel, true, true, 0)
	ethRow.PackEnd(ethSwitch, false, false, 0)
	root.PackStart(ethRow, false, false, 0)

	// IPv6 kill switch toggle
	ip6Row, _ := gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 8)
	ip6Label, _ := gtk.LabelNew("IPv6 kill switch (requires sudo)")
	ip6Switch, _ := gtk.SwitchNew()
	ip6Switch.SetActive(app.cfg.IPv6KillSwitch)
	ip6Switch.Connect("notify::active", func() {
		app.cfg.IPv6KillSwitch = ip6Switch.GetActive()
		saveConfig(app.cfg)
	})
	ip6Row.PackStart(ip6Label, true, true, 0)
	ip6Row.PackEnd(ip6Switch, false, false, 0)
	root.PackStart(ip6Row, false, false, 0)

	// Check for updates toggle
	updRow, _ := gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 8)
	updLabel, _ := gtk.LabelNew("Check for updates automatically")
	updSwitch, _ := gtk.SwitchNew()
	updSwitch.SetActive(app.cfg.CheckForUpdates)
	updSwitch.Connect("notify::active", func() {
		app.cfg.CheckForUpdates = updSwitch.GetActive()
		saveConfig(app.cfg)
	})
	updRow.PackStart(updLabel, true, true, 0)
	updRow.PackEnd(updSwitch, false, false, 0)
	root.PackStart(updRow, false, false, 0)

	win.ShowAll()
	win.Connect("delete-event", func() bool {
		win.Hide()
		return true
	})

	return sw
}

func (sw *SettingsWindow) appendSSIDRow(ssid string) {
	lbl, _ := gtk.LabelNew(ssid)
	lbl.SetXAlign(0)
	lbl.SetMarginStart(8)
	lbl.SetMarginTop(6)
	lbl.SetMarginBottom(6)
	sw.ssidList.Add(lbl)
	sw.ssidList.ShowAll()
}

func (sw *SettingsWindow) refreshConnections(combo *gtk.ComboBoxText) {
	combo.RemoveAll()
	current := sw.app.cfg.WGConnection
	for i, name := range listWGConnections() {
		combo.AppendText(name)
		if name == current {
			combo.SetActive(i)
		}
	}
}

func (sw *SettingsWindow) show() {
	sw.win.Present()
}
