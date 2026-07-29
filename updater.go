package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/gotk3/gotk3/glib"
	"github.com/gotk3/gotk3/gtk"
)

const currentVersion = "0.7.1"
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
	version, debURL, sha256Sum, err := fetchLatestRelease()
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
		app.onUpdateFound(version, debURL, sha256Sum)
	} else {
		logf("update check: already on latest (%s)", currentVersion)
		glib.IdleAdd(func() bool {
			app.mCheckUpdate.SetTitle("Up to date")
			return false
		})
	}
}

func fetchLatestRelease() (version, debURL, sha256Sum string, err error) {
	client := &http.Client{Timeout: 10 * time.Second}
	req, _ := http.NewRequest("GET", releaseAPI, nil)
	req.Header.Set("User-Agent", "burrow-vpn/"+currentVersion)

	resp, err := client.Do(req)
	if err != nil {
		return "", "", "", err
	}
	defer resp.Body.Close()

	var r githubRelease
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return "", "", "", err
	}

	version = strings.TrimPrefix(r.TagName, "v")
	var debName, checksumURL string
	for _, a := range r.Assets {
		if strings.HasSuffix(a.Name, "_amd64.deb") {
			debURL = a.BrowserDownloadURL
			debName = a.Name
		}
	}
	for _, a := range r.Assets {
		if a.Name == debName+".sha256" {
			checksumURL = a.BrowserDownloadURL
			break
		}
	}
	if checksumURL != "" {
		sum, cerr := fetchChecksum(checksumURL)
		if cerr != nil {
			logf("could not fetch checksum for %s: %v", debName, cerr)
		} else {
			sha256Sum = sum
		}
	}
	return version, debURL, sha256Sum, nil
}

// fetchChecksum downloads a "<hash>  <filename>"-style checksum file and
// returns the hash (lowercase hex).
func fetchChecksum(url string) (string, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return "", fmt.Errorf("empty checksum file")
	}
	return strings.ToLower(fields[0]), nil
}

// sha256File returns the lowercase hex SHA-256 digest of a file's contents.
func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
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

func performUpdate(app *App, version, debURL, expectedSHA256 string) {
	glib.IdleAdd(func() bool {
		dlg, _ := gtk.DialogNew()
		dlg.SetTitle("Update Burrow VPN")
		dlg.SetDefaultSize(400, 130)
		dlg.SetModal(true)
		dlg.SetKeepAbove(true)
		dlg.AddButton("Cancel", gtk.RESPONSE_CANCEL)
		dlg.AddButton("Download & Install", gtk.RESPONSE_OK)
		ca, _ := dlg.GetContentArea()
		ca.SetBorderWidth(16)
		ca.SetSpacing(8)
		lbl, _ := gtk.LabelNew(fmt.Sprintf("Version %s is available (you have %s).\nDownload and install now?", version, currentVersion))
		lbl.SetXAlign(0)
		ca.Add(lbl)
		dlg.ShowAll()
		dlg.Present()
		response := dlg.Run()
		dlg.Destroy()

		if response != gtk.RESPONSE_OK {
			return false
		}

		progDlg, _ := gtk.DialogNew()
		progDlg.SetTitle("Updating Burrow VPN…")
		progDlg.SetDefaultSize(400, 110)
		progDlg.SetDeletable(false)
		progDlg.SetKeepAbove(true)
		progCa, _ := progDlg.GetContentArea()
		progCa.SetBorderWidth(16)
		progCa.SetSpacing(10)
		progLbl, _ := gtk.LabelNew("Downloading Burrow VPN " + version + "…")
		progLbl.SetXAlign(0)
		progBar, _ := gtk.ProgressBarNew()
		progCa.Add(progLbl)
		progCa.Add(progBar)
		progDlg.ShowAll()
		progDlg.Present()

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

			if expectedSHA256 != "" {
				actual, err := sha256File(debPath)
				if err != nil || actual != expectedSHA256 {
					os.Remove(debPath)
					logf("checksum verification failed for v%s: got %s want %s (err=%v)", version, actual, expectedSHA256, err)
					glib.IdleAdd(func() bool {
						progDlg.Destroy()
						notify("Burrow VPN", "Update aborted: checksum verification failed")
						return false
					})
					return
				}
				logf("checksum verified for v%s", version)
			} else {
				logf("warning: no checksum available for v%s, installing unverified", version)
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
				rdlg, _ := gtk.DialogNew()
				rdlg.SetTitle("Update installed")
				rdlg.SetDefaultSize(380, 120)
				rdlg.SetModal(true)
				rdlg.SetKeepAbove(true)
				rdlg.AddButton("Later", gtk.RESPONSE_CANCEL)
				rdlg.AddButton("Restart Now", gtk.RESPONSE_OK)
				rca, _ := rdlg.GetContentArea()
				rca.SetBorderWidth(16)
				rlbl, _ := gtk.LabelNew(fmt.Sprintf("Burrow VPN %s installed successfully.", version))
				rlbl.SetXAlign(0)
				rca.Add(rlbl)
				rdlg.ShowAll()
				rdlg.Present()

				if rdlg.Run() == gtk.RESPONSE_OK {
					rdlg.Destroy()
					exe, _ := os.Executable()
					releaseLock()
					exec.Command(exe).Start()
					os.Exit(0)
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
