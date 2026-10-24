//go:build windows

package main

// In-place updates from GitHub Releases. Livery checks the latest release
// once a day, and on request downloads its installer, checks the SHA-256
// published with the release, and runs it silently. The installer stops
// Livery, replaces the files (settings are kept) and starts it again.

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

const releasesAPI = "https://api.github.com/repos/bigred1999/Livery/releases/latest"

type updateInfo struct {
	Available  bool   `json:"available"`
	Version    string `json:"version,omitempty"`
	URL        string `json:"url,omitempty"` // release page
	Installing bool   `json:"installing"`
	Error      string `json:"error,omitempty"`
	CheckedAt  string `json:"checkedAt,omitempty"`

	assetURL, sha string
}

var (
	updMu  sync.Mutex
	updNow updateInfo
)

func getUpdateInfo() updateInfo {
	updMu.Lock()
	defer updMu.Unlock()
	return updNow
}

func checkForUpdate() {
	info := updateInfo{CheckedAt: time.Now().Format("15:04")}
	defer func() {
		updMu.Lock()
		installing := updNow.Installing
		updNow = info
		updNow.Installing = installing
		updMu.Unlock()
	}()
	req, _ := http.NewRequest("GET", releasesAPI, nil)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "Livery/"+version)
	resp, err := (&http.Client{Timeout: 15 * time.Second}).Do(req)
	if err != nil {
		info.Error = "couldn't reach GitHub"
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return // no releases yet
	}
	var rel struct {
		Tag     string `json:"tag_name"`
		HTMLURL string `json:"html_url"`
		Body    string `json:"body"`
		Draft   bool   `json:"draft"`
		Pre     bool   `json:"prerelease"`
		Assets  []struct {
			Name   string `json:"name"`
			URL    string `json:"browser_download_url"`
			Digest string `json:"digest"` // "sha256:..."
		} `json:"assets"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&rel); err != nil {
		info.Error = "unexpected reply from GitHub"
		return
	}
	if rel.Draft || rel.Pre || !newerVersion(rel.Tag, version) {
		return
	}
	info.Version = strings.TrimPrefix(rel.Tag, "v")
	info.URL = rel.HTMLURL
	for _, a := range rel.Assets {
		if strings.HasPrefix(a.Name, "Livery-Setup-") && strings.HasSuffix(a.Name, ".exe") {
			info.assetURL = a.URL
			if strings.HasPrefix(a.Digest, "sha256:") {
				info.sha = strings.ToLower(strings.TrimPrefix(a.Digest, "sha256:"))
			}
			if info.sha == "" {
				for _, m := range shaLine.FindAllStringSubmatch(rel.Body, -1) {
					if m[2] == a.Name {
						info.sha = strings.ToLower(m[1])
					}
				}
			}
		}
	}
	info.Available = info.assetURL != ""
	if info.Available {
		logf("update: Livery %s is available", info.Version)
	}
}

// installUpdate downloads, verifies and launches the new installer. The
// installer stops this process and starts the new version when it's done.
func installUpdate() error {
	updMu.Lock()
	u := updNow
	if !u.Available || u.Installing {
		updMu.Unlock()
		return errors.New("no update to install")
	}
	updNow.Installing, updNow.Error = true, ""
	updMu.Unlock()
	fail := func(err error) error {
		updMu.Lock()
		updNow.Installing, updNow.Error = false, err.Error()
		updMu.Unlock()
		logf("update: %v", err)
		return err
	}
	if u.sha == "" {
		return fail(errors.New("the release has no published checksum, so it wasn't installed automatically; download it from the release page"))
	}
	if !strings.HasPrefix(u.assetURL, "https://github.com/bigred1999/Livery/releases/download/") {
		return fail(errors.New("unexpected download location"))
	}
	resp, err := (&http.Client{Timeout: 5 * time.Minute}).Get(u.assetURL)
	if err != nil {
		return fail(fmt.Errorf("download failed: %v", err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fail(fmt.Errorf("download failed (HTTP %d)", resp.StatusCode))
	}
	dir, err := os.MkdirTemp("", "livery-update-")
	if err != nil {
		return fail(err)
	}
	path := filepath.Join(dir, "Livery-Setup-"+u.Version+".exe")
	f, err := os.Create(path)
	if err != nil {
		return fail(err)
	}
	h := sha256.New()
	_, err = io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, 200<<20))
	f.Close()
	if err != nil {
		return fail(fmt.Errorf("download failed: %v", err))
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != u.sha {
		os.Remove(path)
		return fail(errors.New("the download didn't match the published checksum, so it wasn't installed"))
	}
	logf("update: installing Livery %s", u.Version)
	cmd := exec.Command(path, "/S", "/RELAUNCH")
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x00000008 /* DETACHED_PROCESS */}
	if err := cmd.Start(); err != nil {
		return fail(err)
	}
	cmd.Process.Release()
	return nil
}

// updateLoop checks shortly after start and then daily (background mode only).
func updateLoop(stop <-chan struct{}) {
	select {
	case <-time.After(time.Minute):
	case <-stop:
		return
	}
	for {
		if loadConfig().Settings.AutoUpdateCheck() {
			checkForUpdate()
		}
		select {
		case <-time.After(24 * time.Hour):
		case <-stop:
			return
		}
	}
}
