//go:build windows

package main

import (
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"sync/atomic"
	"syscall"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const (
	mutexName    = `Local\LiveryRunning`
	stopName     = `Local\LiveryStop`
	runKeyPath   = `Software\Microsoft\Windows\CurrentVersion\Run`
	runValueName = "Livery"
	panelAddr    = "127.0.0.1:47820"
	panelURL     = "http://" + panelAddr + "/"

	tick           = 2 * time.Second
	rescanEvery    = 30 * time.Second
	mouseEvery     = 60 * time.Second
	laptopEvery    = 2 * time.Minute
	resumeGapLimit = 20 * time.Second
)

type presence struct{ laptop, gmbk, receiver, ajazz bool }

func detectPresence(all []HidInfo) presence {
	var p presence
	for _, h := range all {
		switch {
		case detectAfxVersion(h) != 0:
			p.laptop = true
		case isGMBK(h) && h.UsagePage == 0xFF01:
			p.gmbk = true
		case isLogiReceiver(h):
			p.receiver = true
		case isAK820Pro(h) && h.UsagePage == ajazzUsagePage:
			p.ajazz = true
		}
	}
	return p
}

// applyRequest asks the background loop (the only code that touches the
// hardware while running) to re-apply the saved configuration.
type applyRequest struct{ done chan DeviceStatus }

var applyCh = make(chan applyRequest)

func currentTheme() (ThemeDef, Settings) {
	cfg := loadConfig()
	t, ok := findTheme(cfg.Settings.ThemeID, cfg.Custom)
	if !ok {
		t = BuiltinThemes[0]
	}
	return t, cfg.Settings
}

func runBackground() {
	killLegacy()
	name, _ := windows.UTF16PtrFromString(mutexName)
	mtx, err := windows.CreateMutex(nil, true, name)
	if err == windows.ERROR_ALREADY_EXISTS {
		if !hasArg("--takeover") {
			logf("already running; exiting")
			return
		}
		// A watchdog restart: wait for the stuck instance to exit.
		if ev, _ := windows.WaitForSingleObject(mtx, 30000); ev != windows.WAIT_OBJECT_0 && ev != 0x80 /* WAIT_ABANDONED */ {
			logf("previous instance did not exit; giving up")
			return
		}
		err = nil
	}
	if err != nil {
		logf("mutex: %v", err)
		return
	}
	defer windows.CloseHandle(mtx)

	sn, _ := windows.UTF16PtrFromString(stopName)
	stop, err := windows.CreateEvent(nil, 1, 0, sn)
	if err != nil {
		logf("stop event: %v", err)
		return
	}
	defer windows.CloseHandle(stop)
	windows.ResetEvent(stop)
	stopCh := make(chan struct{})
	go func() {
		windows.WaitForSingleObject(stop, windows.INFINITE)
		close(stopCh)
	}()

	go startPanel()
	go watchdog()
	go updateLoop(stopCh)

	t, s := currentTheme()
	logf("background mode started (theme %s, %s, %d%%)", t.Name, s.Effect, s.Brightness)

	var (
		all, _     = EnumerateHID()
		have       = detectPresence(all)
		rx         *Receiver
		lastWall   = time.Now().Round(0)
		lastRescan = time.Now()
		lastMouse  time.Time
		lastLaptop = time.Now()
		mouseOK    bool
	)
	applyAll := func() {
		devMu.Lock()
		defer devMu.Unlock()
		t, s = currentTheme()
		r := t.Resolve(s.Brightness)
		applyLaptop(all, r, s)
		applyGMBK(all, r, s)
		applyAjazz(all, t, s)
		st := applyOpenRGB(t, s)
		setStatus(func(d *DeviceStatus) { d.OpenRGB = st })
		applyBulbs(t, s)
		startAnimator(t, s)
		if rx == nil && have.receiver {
			rx, _ = OpenReceiver(all, logf)
		}
		if rx != nil {
			mouseOK = applyMouseWith(rx, r, s)
		} else {
			setStatus(func(d *DeviceStatus) { d.Mouse = "receiver not found" })
		}
		lastMouse, lastLaptop = time.Now(), time.Now()
	}
	applyAll()

	// If OpenRGB is installed but its server isn't up, start it and paint
	// its devices once it's ready.
	go func() {
		if ensureOpenRGB() {
			req := applyRequest{done: make(chan DeviceStatus, 1)}
			select {
			case applyCh <- req:
			case <-stopCh:
			}
		}
	}()

	ticker := time.NewTicker(tick)
	defer ticker.Stop()
loop:
	for {
		beat.Add(1)
		select {
		case <-stopCh:
			logf("stop requested")
			break loop
		case pr := <-probeCh:
			devMu.Lock()
			all, _ = EnumerateHID()
			pr.done <- runProbe(all, pr)
			devMu.Unlock()
			continue
		case req := <-applyCh:
			all, _ = EnumerateHID()
			have = detectPresence(all)
			applyAll()
			req.done <- getStatus()
			continue
		case <-ticker.C:
		}

		now := time.Now()
		wall := now.Round(0)
		resumed := wall.Sub(lastWall) > resumeGapLimit
		lastWall = wall

		if resumed {
			logf("resume detected; re-applying everything")
			time.Sleep(3 * time.Second)
			if rx != nil {
				rx.Close()
				rx = nil
			}
			all, _ = EnumerateHID()
			have = detectPresence(all)
			lastRescan = time.Now()
			applyAll()
			continue
		}

		if now.Sub(lastRescan) >= rescanEvery {
			lastRescan = now
			all, _ = EnumerateHID()
			p := detectPresence(all)
			t, s = currentTheme()
			r := t.Resolve(s.Brightness)
			devMu.Lock()
			if p.gmbk && !have.gmbk {
				logf("GMBK connected")
				time.Sleep(time.Second)
				applyGMBK(all, r, s)
			}
			if !p.gmbk && have.gmbk {
				setStatus(func(d *DeviceStatus) { d.GMBK = "not connected" })
			}
			if p.ajazz && !have.ajazz {
				logf("Ajazz AK820 Pro connected")
				time.Sleep(time.Second)
				applyAjazz(all, t, s)
			}
			if !p.ajazz && have.ajazz {
				setStatus(func(d *DeviceStatus) { d.Ajazz = "not connected" })
			}
			if p.laptop && !have.laptop {
				applyLaptop(all, r, s)
			}
			if p.receiver && !have.receiver {
				logf("Logitech receiver connected")
				if rx != nil {
					rx.Close()
				}
				rx, _ = OpenReceiver(all, logf)
				lastMouse = time.Time{}
			}
			if !p.receiver && rx != nil {
				rx.Close()
				rx = nil
				setStatus(func(d *DeviceStatus) { d.Mouse = "receiver not found" })
			}
			devMu.Unlock()
			have = p
		}

		if rx != nil && rx.short != nil {
			devMu.Lock()
			woke := false
			for i := 0; i < 10; i++ {
				b, _ := rx.short.Read(5 * time.Millisecond)
				if b == nil {
					break
				}
				if len(b) >= 5 && b[0] == hidppShort && b[2] == 0x41 && b[4]&0x40 == 0 {
					woke = true
				}
			}
			t, s = currentTheme()
			r := t.Resolve(s.Brightness)
			if woke {
				logf("mouse woke up")
				time.Sleep(500 * time.Millisecond)
				mouseOK = applyMouseWith(rx, r, s)
				lastMouse = time.Now()
			} else if now.Sub(lastMouse) >= mouseEvery && (!mouseOK || s.Effect != "breathe") {
				mouseOK = quietly(func() bool { return applyMouseWith(rx, r, s) }, mouseOK, "mouse")
				lastMouse = time.Now()
			}
			devMu.Unlock()
		}

		// Re-assert the laptop colors in case Alienware Command Center changed them.
		// Skipped while breathing so the animation isn't restarted.
		if now.Sub(lastLaptop) >= laptopEvery {
			lastLaptop = now
			t, s = currentTheme()
			if s.Effect == "static" {
				r := t.Resolve(s.Brightness)
				devMu.Lock()
				quietly(func() bool { return applyLaptop(all, r, s) }, true, "laptop")
				devMu.Unlock()
			}
		}
	}
	if rx != nil {
		rx.Close()
	}
	logf("background mode stopped")
}

// quietly runs f with logging suppressed and only logs when the result changes.
func quietly(f func() bool, wasOK bool, what string) bool {
	saved := logw
	var sink discardWriter
	logw = &sink
	ok := f()
	logw = saved
	if ok != wasOK {
		if ok {
			logf("%s back online; colors applied", what)
		} else {
			logf("%s refresh failed: %s", what, sink.last)
		}
	}
	return ok
}

type discardWriter struct{ last string }

func (d *discardWriter) Write(p []byte) (int, error) {
	d.last = string(p)
	return len(p), nil
}

func stopBackground() {
	sn, _ := windows.UTF16PtrFromString(stopName)
	h, err := windows.OpenEvent(windows.EVENT_MODIFY_STATE, false, sn)
	if err != nil {
		logf("background mode is not running")
		return
	}
	windows.SetEvent(h)
	windows.CloseHandle(h)
	logf("asked background mode to stop")
}

func startDetached(args ...string) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cmd := exec.Command(exe, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x00000008 /* DETACHED_PROCESS */}
	return cmd.Start()
}

func autostartEnabled() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	_, _, err = k.GetStringValue(runValueName)
	return err == nil
}

func setAutostart(on bool) error {
	if !on {
		k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
		if err != nil {
			return err
		}
		defer k.Close()
		if err := k.DeleteValue(runValueName); err != nil && err != registry.ErrNotExist {
			return err
		}
		logf("autostart removed")
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	k, _, err := registry.CreateKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	cmdline := `"` + exe + `" run`
	if err := k.SetStringValue(runValueName, cmdline); err != nil {
		return err
	}
	logf("autostart installed: %s", cmdline)
	return nil
}

func installAutostart() {
	if err := setAutostart(true); err != nil {
		logf("install: %v", err)
		return
	}
	if !panelUp() {
		if err := startDetached("run"); err != nil {
			logf("start background mode: %v", err)
		}
	}
}

func uninstallAutostart() {
	if err := setAutostart(false); err != nil {
		logf("uninstall: %v", err)
	}
	stopBackground()
}

func panelUp() bool {
	c := http.Client{Timeout: 700 * time.Millisecond}
	resp, err := c.Get(panelURL + "api/ping")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	var body struct{ App string }
	json.NewDecoder(resp.Body).Decode(&body)
	return resp.StatusCode == 200 && body.App == "livery"
}

// openPanel makes sure the background mode is running and opens the control panel.
func openPanel() {
	if !panelUp() {
		if err := startDetached("run"); err != nil {
			logf("start background mode: %v", err)
			return
		}
		for i := 0; i < 40 && !panelUp(); i++ {
			time.Sleep(250 * time.Millisecond)
		}
	}
	verb, _ := windows.UTF16PtrFromString("open")
	u, _ := windows.UTF16PtrFromString(panelURL)
	if err := windows.ShellExecute(0, verb, u, nil, nil, windows.SW_SHOWNORMAL); err != nil {
		logf("open browser: %v", err)
	}
}

var beat atomic.Int64

// watchdog restarts background mode if the main loop stops ticking, e.g. when
// a device call hangs after sleep. It counts its own ticks, so time spent with
// the PC asleep never looks like a hang.
func watchdog() {
	last, still := beat.Load(), 0
	for range time.Tick(15 * time.Second) {
		now := beat.Load()
		if now != last {
			last, still = now, 0
			continue
		}
		if still++; still >= 6 {
			logf("watchdog: background loop stuck for ~90 s; restarting")
			if err := startDetached("run", "--takeover"); err != nil {
				logf("watchdog restart failed: %v", err)
			}
			os.Exit(3)
		}
	}
}

func hasArg(a string) bool {
	for _, x := range os.Args[1:] {
		if x == a {
			return true
		}
	}
	return false
}
