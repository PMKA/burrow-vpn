package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

const currentVersion = "0.4.0"
const releaseAPI = "https://api.github.com/repos/PMKA/burrow-vpn/releases/latest"

type githubRelease struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
}

func startUpdateChecker(app *App) {
	go func() {
		// Small delay so startup isn't slowed
		time.Sleep(15 * time.Second)
		if app.cfg.CheckForUpdates {
			runUpdateCheck()
		}

		ticker := time.NewTicker(24 * time.Hour)
		defer ticker.Stop()
		for range ticker.C {
			if app.cfg.CheckForUpdates {
				runUpdateCheck()
			}
		}
	}()
}

func runUpdateCheck() {
	latest, url, err := fetchLatestVersion()
	if err != nil {
		logf("update check failed: %v", err)
		return
	}
	if isNewerVersion(latest, currentVersion) {
		logf("update available: v%s → v%s", currentVersion, latest)
		notify("Burrow VPN update available",
			fmt.Sprintf("Version %s is available (you have %s)\n%s", latest, currentVersion, url))
	} else {
		logf("update check: already on latest (%s)", currentVersion)
	}
}

func fetchLatestVersion() (version, url string, err error) {
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
	return strings.TrimPrefix(r.TagName, "v"), r.HTMLURL, nil
}

// isNewerVersion returns true if candidate is strictly newer than current (X.Y.Z).
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
