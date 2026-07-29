package main

import (
	"github.com/gotk3/gotk3/gtk"
)

// runSetupWizard opens a first-run modal and returns the updated config and
// whether the user skipped setup. If skipped, the caller should not save config.
func runSetupWizard(parent *gtk.Window, cfg Config) (Config, bool) {
	dlg, _ := gtk.DialogNewWithButtons("Welcome to Burrow VPN", parent,
		gtk.DIALOG_MODAL|gtk.DIALOG_DESTROY_WITH_PARENT,
		[]interface{}{"Skip for now", gtk.RESPONSE_CANCEL, "Done", gtk.RESPONSE_OK},
	)
	dlg.SetDefaultSize(440, 380)
	dlg.SetBorderWidth(16)
	defer dlg.Destroy()

	ca, _ := dlg.GetContentArea()
	ca.SetSpacing(14)

	heading, _ := gtk.LabelNew("")
	heading.SetMarkup("<b>First-time setup</b>")
	heading.SetXAlign(0)
	ca.Add(heading)

	sub, _ := gtk.LabelNew("Configure your WireGuard connection and trusted networks.")
	sub.SetXAlign(0)
	sub.SetLineWrap(true)
	ca.Add(sub)

	// WireGuard section
	wgFrame, _ := gtk.FrameNew(" WireGuard Connection ")
	wgBox, _ := gtk.BoxNew(gtk.ORIENTATION_VERTICAL, 8)
	wgBox.SetBorderWidth(10)
	wgFrame.Add(wgBox)

	connRow, _ := gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 8)
	connLabel, _ := gtk.LabelNew("Connection:")
	combo, _ := gtk.ComboBoxTextNew()
	combo.SetHExpand(true)

	conns := listWGConnections()
	for i, name := range conns {
		combo.AppendText(name)
		if name == cfg.WGConnection {
			combo.SetActive(i)
		}
	}
	combo.Connect("changed", func() {
		cfg.WGConnection = combo.GetActiveText()
	})
	connRow.PackStart(connLabel, false, false, 0)
	connRow.PackStart(combo, true, true, 0)
	wgBox.PackStart(connRow, false, false, 0)

	importRow, _ := gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 8)
	importBtn, _ := gtk.ButtonNewWithLabel("Import .conf file…")
	importStatus, _ := gtk.LabelNew("")
	importBtn.Connect("clicked", func() {
		fdlg, _ := gtk.FileChooserDialogNewWith2Buttons(
			"Select WireGuard .conf", dlg,
			gtk.FILE_CHOOSER_ACTION_OPEN,
			"Cancel", gtk.RESPONSE_CANCEL,
			"Open", gtk.RESPONSE_OK,
		)
		filter, _ := gtk.FileFilterNew()
		filter.SetName("WireGuard config (*.conf)")
		filter.AddPattern("*.conf")
		fdlg.AddFilter(filter)
		if fdlg.Run() == gtk.RESPONSE_OK {
			path := fdlg.GetFilename()
			fdlg.Destroy()
			name, err := importWGConfig(path)
			if err != nil {
				importStatus.SetText("Import failed: " + err.Error())
			} else {
				cfg.WGConnection = name
				combo.RemoveAll()
				for i, c := range listWGConnections() {
					combo.AppendText(c)
					if c == name {
						combo.SetActive(i)
					}
				}
				importStatus.SetText("Imported: " + name)
			}
		} else {
			fdlg.Destroy()
		}
	})
	importRow.PackStart(importBtn, false, false, 0)
	importRow.PackStart(importStatus, false, false, 0)
	wgBox.PackStart(importRow, false, false, 0)
	ca.Add(wgFrame)

	// Trusted network section
	netFrame, _ := gtk.FrameNew(" Trusted WiFi Network ")
	netBox, _ := gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 8)
	netBox.SetBorderWidth(10)
	netFrame.Add(netBox)

	ssid := getCurrentSSID()
	var netLabel *gtk.Label
	if ssid != "" {
		netLabel, _ = gtk.LabelNew("Current network: " + ssid)
	} else {
		netLabel, _ = gtk.LabelNew("No WiFi detected")
	}
	netLabel.SetXAlign(0)
	netLabel.SetHExpand(true)

	addNetBtn, _ := gtk.ButtonNewWithLabel("Add as trusted")
	addNetBtn.SetSensitive(ssid != "" && !cfg.isTrusted(ssid))
	addNetBtn.Connect("clicked", func() {
		if ssid != "" {
			cfg.addTrusted(ssid)
			addNetBtn.SetSensitive(false)
			addNetBtn.SetLabel("Added")
			logf("wizard: added trusted SSID %q", ssid)
		}
	})

	netBox.PackStart(netLabel, true, true, 0)
	netBox.PackStart(addNetBtn, false, false, 0)
	ca.Add(netFrame)

	// Auto-connect toggle
	autoRow, _ := gtk.BoxNew(gtk.ORIENTATION_HORIZONTAL, 8)
	autoLabel, _ := gtk.LabelNew("Auto-connect on untrusted networks")
	autoLabel.SetXAlign(0)
	autoLabel.SetHExpand(true)
	autoSwitch, _ := gtk.SwitchNew()
	autoSwitch.SetActive(cfg.AutoConnect)
	autoSwitch.Connect("notify::active", func() {
		cfg.AutoConnect = autoSwitch.GetActive()
	})
	autoRow.PackStart(autoLabel, true, true, 0)
	autoRow.PackEnd(autoSwitch, false, false, 0)
	ca.Add(autoRow)

	dlg.ShowAll()
	response := dlg.Run()

	if response != gtk.RESPONSE_OK {
		logf("wizard skipped")
		return cfg, true
	}

	logf("wizard complete: connection=%q trusted=%v auto=%v", cfg.WGConnection, cfg.TrustedSSIDs, cfg.AutoConnect)
	return cfg, false
}
