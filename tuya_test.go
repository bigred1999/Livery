package main

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestTuyaAgainstReference talks to a fake bulb built on TinyTuya's own
// framing and crypto (see testdata/tuya_fakebulb.py) for every protocol version.
// Run with LIVERY_TUYA_FAKE=testdata/tuya_fakebulb.py (needs pip install tinytuya).
func TestTuyaAgainstReference(t *testing.T) {
	script := os.Getenv("LIVERY_TUYA_FAKE")
	if script == "" {
		t.Skip("set LIVERY_TUYA_FAKE to fakebulb.py to run")
	}
	for i, ver := range []string{"3.3", "3.4", "3.5"} {
		t.Run(ver, func(t *testing.T) {
			port := 16668 + i
			logPath := filepath.Join(t.TempDir(), "bulb.log")
			cmd := exec.Command("python3", script, ver, itoa(port), logPath)
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			defer cmd.Process.Kill()
			time.Sleep(700 * time.Millisecond)
			old := tuyaPort
			tuyaPort = port
			defer func() { tuyaPort = old }()

			d := &TuyaDevice{ID: "ebfakebulb1234567890ab", IP: "127.0.0.1", Key: []byte("0123456789abcdef"), Version: ver}
			if err := d.Connect(); err != nil {
				t.Fatalf("connect: %v\n%s", err, readLog(logPath))
			}
			defer d.Close()
			dps, err := d.Status()
			if err != nil {
				t.Fatalf("status: %v\n%s", err, readLog(logPath))
			}
			if bulbLayout(dps) != "B" {
				t.Fatalf("layout from %v", dps)
			}
			want := bulbDPS("B", RGB{255, 0, 0})
			if err := d.Set(want); err != nil {
				t.Fatalf("set: %v\n%s", err, readLog(logPath))
			}
			time.Sleep(200 * time.Millisecond)
			lg := readLog(logPath)
			if !strings.Contains(lg, `SET {"20": true, "21": "colour", "24": "000003e803e8"}`) {
				t.Fatalf("bulb did not get the color:\n%s", lg)
			}
			// A wrong key must never look like a match.
			bad := &TuyaDevice{ID: "ebfakebulb1234567890ab", IP: "127.0.0.1", Key: []byte("fedcba9876543210"), Version: ver, Timeout: time.Second}
			if err := bad.Connect(); err == nil {
				if _, err := bad.Status(); err == nil {
					t.Fatal("wrong key was accepted")
				}
				bad.Close()
			}
			// Nor a different device ID.
			other := &TuyaDevice{ID: "ebsomeotherbulb0000000", IP: "127.0.0.1", Key: []byte("0123456789abcdef"), Version: ver, Timeout: time.Second}
			if err := other.Connect(); err == nil {
				if _, err := other.Status(); err == nil {
					t.Fatal("another device's ID was accepted")
				}
				other.Close()
			}
		})
	}
}

func itoa(i int) string { return fmtInt(i) }
func fmtInt(i int) string {
	s := ""
	for i > 0 {
		s = string(rune('0'+i%10)) + s
		i /= 10
	}
	return s
}

func readLog(p string) string {
	f, err := os.Open(p)
	if err != nil {
		return ""
	}
	defer f.Close()
	var b strings.Builder
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		b.WriteString(sc.Text() + "\n")
	}
	return b.String()
}

func TestBulbColors(t *testing.T) {
	if got := bulbDPS("B", RGB{255, 0, 0})["24"]; got != "000003e803e8" {
		t.Errorf("red = %v", got)
	}
	if got := bulbDPS("B", RGB{0, 0, 255})["24"]; got != "00f003e803e8" {
		t.Errorf("blue = %v", got)
	}
	if got := bulbDPS("B", RGB{255, 225, 200}); got["21"] != "white" {
		t.Errorf("warm white should use white mode: %v", got)
	}
	if got := bulbDPS("B", RGB{0, 0, 0}); got["20"] != false {
		t.Errorf("black should switch off: %v", got)
	}
	if got := bulbDPS("A", RGB{255, 0, 0})["5"]; got != "ff00000000ffff" {
		t.Errorf("type A red = %v", got)
	}
}

func TestBulbColorRoles(t *testing.T) {
	th, _ := findTheme("tricolore", nil)
	got := []RGB{bulbColor(th, "auto", 0, 100), bulbColor(th, "auto", 1, 100), bulbColor(th, "auto", 2, 100)}
	want := []string{hexGreen, hexWarmWhite, hexRed}
	for i := range got {
		if got[i] != mustHex(want[i]) {
			t.Errorf("bulb %d = %v want %s", i, got[i], want[i])
		}
	}
	th, _ = findTheme("papaya", nil)
	if bulbColor(th, "accent", 0, 100) != mustHex(hexElecBlue) || bulbColor(th, "auto", 1, 100) != mustHex(hexElecBlue) {
		t.Error("accent role")
	}
}

// Signatures match what TinyTuya's cloud client produces for the same input.
func TestTuyaSign(t *testing.T) {
	got := tuyaSign("myaccessid", "mysecret", "", "1700000000123", "GET", "", "/v1.0/token?grant_type=1")
	if got != "AC7C2467258493E105970319160BD9C9422F70DF1C531537E42FE8FCBE6960AC" {
		t.Errorf("token sign %s", got)
	}
	got = tuyaSign("myaccessid", "mysecret", "tok123", "1700000000123", "GET", "", "/v1.0/iot-01/associated-users/devices?last_row_key=abc&size=50")
	if got != "DA2837C3D27E49EC5A2E23B6358F0C63ED48CE4C3EA63D1651B36BE702D47E35" {
		t.Errorf("request sign %s", got)
	}
}

func TestNewerVersion(t *testing.T) {
	for _, c := range []struct {
		a, b string
		want bool
	}{{"v1.10.0", "1.9.2", true}, {"1.4.0", "1.4.0", false}, {"v1.3.9", "1.4.0", false}, {"2", "1.9.9", true}} {
		if newerVersion(c.a, c.b) != c.want {
			t.Errorf("%s > %s should be %v", c.a, c.b, c.want)
		}
	}
	m := shaLine.FindStringSubmatch("SHA-256: `fb8f23ff03d43c15d6131b17aaa31c98b4853593875efb29175a2c0717a4a476`  Livery-Setup-1.2.2.exe")
	if m == nil || m[2] != "Livery-Setup-1.2.2.exe" {
		t.Errorf("sha line not parsed: %v", m)
	}
}
