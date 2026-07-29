package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/getlantern/systray"
	"github.com/gotk3/gotk3/glib"
	"github.com/gotk3/gotk3/gtk"
)

const currentVersion = "0.6.1"
const releaseAPI = "https://api.github.com/repos/PMKA/burrow-vpn/releases/latest"

type releaseAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

type githubRelease struct {
	TagName string         `json:"tag_name"`
	HTMLURL string         `json:"html_url"`
	Assets  []releaseAsset `json:"assets"`
}

func startUpdateChecker(app *App) {
	go func() {
		time.Sleep(15 * time.Second)
		runUpdateCheck(app)

		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			if app.cfg.CheckForUpdates {
				runUpdateCheck(app)
			}
		}
	}()
}

func runUpdateCheck(app *App) {
	if !app.cfg.CheckForUpdates {
		return
	}
	version, debURL, err := fetchLatestRelease()
	if err != nil {
		logf("update check failed: %v", err)
		glib.IdleAdd(func() bool {
			app.mCheckUpdate.SetTitle("Check for updates")
			return false
		})
		return
	}
	if isNewerVersion(version, currentVersion) {
		logf("update available: v%s → v%s", currentVersion, version)
		app.onUpdateFound(version, debURL)
	} else {
		logf("update check: already on latest (%s)", currentVersion)
		glib.IdleAdd(func() bool {
			app.mCheckUpdate.SetTitle("Up to date")
			return false
		})
	}
}

func fetchLatestRelease() (version, debURL string, err error) {
	client := &http.Client{Timeout: 10 * time.Second}
	req, _ := http.NewRequest("GET", releaseAPI, nil)
	req.Header.Set("User-Agent", "burrow-vpn/"+currentVersion)

	resp, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	var r githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return "", "", err
	}

	version = strings.TrimPrefix(r.TagName, "v")
	for _, a := range r.Assets {
		if strings.HasSuffix(a.Name, "_amd64.deb") {
			debURL = a.BrowserDownloadURL
			break
		}
	}
	return version, debURL, nil
}

func downloadUpdate(url string, onProgress func(float64)) (string, error) {
	dest := "/tmp/burrow-vpn-update.deb"

	client := &http.Client{}
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("User-Agent", "burrow-vpn/"+currentVersion)

	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	f, err := os.Create(dest)
	if err != nil {
		return "", err
	}
	defer f.Close()

	total := resp.ContentLength
	var downloaded int64
	buf := make([]byte, 32*1024)
	for {
		n, readErr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := f.Write(buf[:n]); werr != nil {
				return "", werr
			}
			downloaded += int64(n)
			if total > 0 && onProgress != nil {
				onProgress(float64(downloaded) / float64(total))
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return "", readErr
		}
	}
	return dest, nil
}

func installUpdate(debPath string) error {
	for _, args := range [][]string{
		{"pkexec", "dpkg", "-i", debPath},
		{"sudo", "dpkg", "-i", debPath},
	} {
		if err := exec.Command(args[0], args[1:]...).Run(); err == nil {
			return nil
		}
	}
	return fmt.Errorf("installation failed — try: sudo dpkg -i %s", debPath)
}

func performUpdate(app *App, version, debURL string) {
	glib.IdleAdd(func() bool {
		dlg, _ := gtk.DialogNewWithButtons(
			"Update Burrow VPN",
			nil,
			gtk.DIALOG_MODAL,
			[]interface{}{"Cancel", gtk.RESPONSE_CANCEL, "Download & Install", gtk.RESPONSE_OK},
		)
		dlg.SetDefaultSize(400, 130)
		ca, _ := dlg.GetContentArea()
		ca.SetBorderWidth(16)
		lbl, _ := gtk.LabelNew(fmt.Sprintf("Version %s is available (you have %s).\nDownload and install now?", version, currentVersion))
		lbl.SetXAlign(0)
		ca.Add(lbl)
		dlg.ShowAll()
		response := dlg.Run()
		dlg.Destroy()

		if response != gtk.RESPONSE_OK {
			return false
		}

		progDlg, _ := gtk.DialogNew()
		progDlg.SetTitle("Updating Burrow VPN…")
		progDlg.SetDefaultSize(400, 110)
		progDlg.SetDeletable(false)
		progCa, _ := progDlg.GetContentArea()
		progCa.SetBorderWidth(16)
		progCa.SetSpacing(10)
		progLbl, _ := gtk.LabelNew("Downloading Burrow VPN " + version + "…")
		progLbl.SetXAlign(0)
		progBar, _ := gtk.ProgressBarNew()
		progCa.Add(progLbl)
		progCa.Add(progBar)
		progDlg.ShowAll()

		go func() {
			debPath, err := downloadUpdate(debURL, func(pct float64) {
				glib.IdleAdd(func() bool {
					progBar.SetFraction(pct)
					return false
				})
			})

			if err != nil {
				logf("download failed: %v", err)
				glib.IdleAdd(func() bool {
					progDlg.Destroy()
					notify("Burrow VPN", "Download failed: "+err.Error())
					return false
				})
				return
			}

			glib.IdleAdd(func() bool {
				progLbl.SetText("Installing…")
				progBar.SetFraction(1.0)
				return false
			})

			if err := installUpdate(debPath); err != nil {
				logf("install failed: %v", err)
				glib.IdleAdd(func() bool {
					progDlg.Destroy()
					notify("Burrow VPN", "Install failed: "+err.Error())
					return false
				})
				return
			}

			os.Remove(debPath)
			logf("updated to v%s", version)

			glib.IdleAdd(func() bool {
				progDlg.Destroy()
				rdlg, _ := gtk.DialogNewWithButtons(
					"Update installed",
					nil,
					gtk.DIALOG_MODAL,
					[]interface{}{"Later", gtk.RESPONSE_CANCEL, "Restart Now", gtk.RESPONSE_OK},
				)
				rdlg.SetDefaultSize(380, 120)
				rca, _ := rdlg.GetContentArea()
				rca.SetBorderWidth(16)
				rlbl, _ := gtk.LabelNew(fmt.Sprintf("Burrow VPN %s installed successfully.", version))
				rlbl.SetXAlign(0)
				rca.Add(rlbl)
				rdlg.ShowAll()

				if rdlg.Run() == gtk.RESPONSE_OK {
					rdlg.Destroy()
					exe, _ := os.Executable()
					releaseLock()
					exec.Command(exe).Start()
					gtk.MainQuit()
					systray.Quit()
				} else {
					rdlg.Destroy()
				}
				return false
			})
		}()

		return false
	})
}

func isNewerVersion(candidate, current string) bool {
	cp := versionParts(candidate)
	cu := versionParts(current)
	for i := 0; i < 3; i++ {
		if cp[i] > cu[i] {
			return true
		}
		if cp[i] < cu[i] {
			return false
		}
	}
	return false
}

func versionParts(v string) [3]int {
	var parts [3]int
	segs := strings.SplitN(v, ".", 3)
	for i, s := range segs {
		if i >= 3 {
			break
		}
		fmt.Sscan(s, &parts[i])
	}
	return parts
}
