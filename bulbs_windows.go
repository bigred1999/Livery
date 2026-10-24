//go:build windows

package main

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

// TuyaConfig holds the smart-bulb setup. The Access Secret is encrypted with
// Windows DPAPI, so only this Windows account can read it back.
type TuyaConfig struct {
	Region    string     `json:"region,omitempty"`
	AccessID  string     `json:"accessId,omitempty"`
	SecretEnc string     `json:"accessSecretProtected,omitempty"`
	Bulbs     []TuyaBulb `json:"bulbs,omitempty"`
}

type TuyaBulb struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Key      string `json:"localKey"`
	Category string `json:"category,omitempty"`
	IP       string `json:"ip,omitempty"`
	Version  string `json:"version,omitempty"`
	Layout   string `json:"layout,omitempty"` // "A" or "B", learned from the bulb
	Enabled  bool   `json:"enabled"`
	Role     string `json:"role,omitempty"` // auto | main | accent
}

// bulbView is what the control panel sees (no keys).
type bulbView struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	IP      string `json:"ip"`
	Enabled bool   `json:"enabled"`
	Role    string `json:"role"`
	Light   bool   `json:"light"`
	Status  string `json:"status"`
}

type tuyaView struct {
	Configured bool       `json:"configured"`
	Region     string     `json:"region"`
	AccessID   string     `json:"accessId"`
	Bulbs      []bulbView `json:"bulbs"`
	Finding    bool       `json:"finding"`
}

var (
	bulbMu     sync.Mutex // serialises talking to bulbs
	bulbStatMu sync.Mutex
	bulbStat   = map[string]string{} // bulb id -> last result
	finding    bool
	cfgEdit    sync.Mutex // read-modify-write of config.json
)

func updateConfig(f func(*Config)) error {
	cfgEdit.Lock()
	defer cfgEdit.Unlock()
	c := loadConfig()
	f(&c)
	return saveConfig(c)
}

func protectSecret(s string) (string, error) {
	in := windows.DataBlob{Size: uint32(len(s)), Data: unsafe.SliceData([]byte(s))}
	var out windows.DataBlob
	if err := windows.CryptProtectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return "", err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return base64.StdEncoding.EncodeToString(unsafe.Slice(out.Data, out.Size)), nil
}

func unprotectSecret(enc string) (string, error) {
	b, err := base64.StdEncoding.DecodeString(enc)
	if err != nil || len(b) == 0 {
		return "", errors.New("no saved secret")
	}
	in := windows.DataBlob{Size: uint32(len(b)), Data: &b[0]}
	var out windows.DataBlob
	if err := windows.CryptUnprotectData(&in, nil, nil, 0, nil, windows.CRYPTPROTECT_UI_FORBIDDEN, &out); err != nil {
		return "", err
	}
	defer windows.LocalFree(windows.Handle(unsafe.Pointer(out.Data)))
	return string(unsafe.Slice(out.Data, out.Size)), nil
}

func buildTuyaView(c Config) tuyaView {
	v := tuyaView{Configured: c.Tuya.AccessID != "", Region: c.Tuya.Region, AccessID: c.Tuya.AccessID}
	bulbStatMu.Lock()
	v.Finding = finding
	for _, b := range c.Tuya.Bulbs {
		role := b.Role
		if role == "" {
			role = "auto"
		}
		v.Bulbs = append(v.Bulbs, bulbView{ID: b.ID, Name: b.Name, IP: b.IP, Enabled: b.Enabled, Role: role,
			Light: isLightCategory(b.Category), Status: bulbStat[b.ID]})
	}
	bulbStatMu.Unlock()
	return v
}

func setBulbStat(id, s string) {
	bulbStatMu.Lock()
	bulbStat[id] = s
	bulbStatMu.Unlock()
}

// connectTuyaCloud reads the device list (with local keys) from the person's
// Tuya cloud project and merges it into the config, keeping their choices.
func connectTuyaCloud(region, id, secret string) (int, error) {
	cfg := loadConfig()
	if secret == "" && cfg.Tuya.SecretEnc != "" && id == cfg.Tuya.AccessID {
		secret, _ = unprotectSecret(cfg.Tuya.SecretEnc)
	}
	cl, err := newTuyaCloud(region, id, secret)
	if err != nil {
		return 0, err
	}
	devs, err := cl.Devices()
	if err != nil {
		return 0, err
	}
	enc, err := protectSecret(strings.TrimSpace(secret))
	if err != nil {
		return 0, fmt.Errorf("could not protect the secret: %v", err)
	}
	n := 0
	err = updateConfig(func(c *Config) {
		old := map[string]TuyaBulb{}
		for _, b := range c.Tuya.Bulbs {
			old[b.ID] = b
		}
		c.Tuya.Region, c.Tuya.AccessID, c.Tuya.SecretEnc = region, strings.TrimSpace(id), enc
		c.Tuya.Bulbs = nil
		for _, d := range devs {
			if d.LocalKey == "" {
				continue
			}
			b := TuyaBulb{ID: d.ID, Name: d.Name, Key: d.LocalKey, Category: d.Category, Enabled: isLightCategory(d.Category), Role: "auto"}
			if o, ok := old[d.ID]; ok {
				b.IP, b.Version, b.Layout, b.Enabled, b.Role = o.IP, o.Version, o.Layout, o.Enabled, o.Role
				if o.Key != b.Key { // re-paired: forget what we learned
					b.IP, b.Version, b.Layout = "", "", ""
				}
			}
			if isLightCategory(d.Category) {
				n++
			}
			c.Tuya.Bulbs = append(c.Tuya.Bulbs, b)
		}
		sort.SliceStable(c.Tuya.Bulbs, func(i, j int) bool {
			li, lj := isLightCategory(c.Tuya.Bulbs[i].Category), isLightCategory(c.Tuya.Bulbs[j].Category)
			if li != lj {
				return li
			}
			return strings.ToLower(c.Tuya.Bulbs[i].Name) < strings.ToLower(c.Tuya.Bulbs[j].Name)
		})
	})
	logf("smart bulbs: %d device(s) from Tuya cloud, %d light(s)", len(devs), n)
	return n, err
}

// ---- finding bulbs on the network ---------------------------------------------

// localSubnets lists the /24 networks of this PC's IPv4 addresses.
func localSubnets() []net.IP {
	var out []net.IP
	seen := map[string]bool{}
	addrs, _ := net.InterfaceAddrs()
	for _, a := range addrs {
		n, ok := a.(*net.IPNet)
		if !ok {
			continue
		}
		ip := n.IP.To4()
		if ip == nil || ip.IsLoopback() || ip[0] == 169 && ip[1] == 254 {
			continue
		}
		ones, _ := n.Mask.Size()
		if ones < 16 { // VPN-sized ranges: don't scan
			continue
		}
		base := net.IPv4(ip[0], ip[1], ip[2], 0).To4()
		if !seen[base.String()] {
			seen[base.String()] = true
			out = append(out, base)
		}
	}
	return out
}

// scanTuyaHosts returns local addresses with the Tuya port open.
func scanTuyaHosts() []string {
	var (
		mu    sync.Mutex
		hosts []string
		wg    sync.WaitGroup
		sem   = make(chan struct{}, 64)
	)
	for _, base := range localSubnets() {
		for i := 1; i < 255; i++ {
			ip := net.IPv4(base[0], base[1], base[2], byte(i)).String()
			wg.Add(1)
			sem <- struct{}{}
			go func() {
				defer wg.Done()
				defer func() { <-sem }()
				c, err := net.DialTimeout("tcp", net.JoinHostPort(ip, fmt.Sprint(tuyaPort)), 400*time.Millisecond)
				if err == nil {
					c.Close()
					mu.Lock()
					hosts = append(hosts, ip)
					mu.Unlock()
				}
			}()
		}
	}
	wg.Wait()
	sort.Strings(hosts)
	return hosts
}

// probeBulb tries the bulb's key on ip with each protocol version and returns
// the version and data-point layout that worked.
func probeBulb(ip string, b TuyaBulb) (string, string, error) {
	versions := []string{"3.5", "3.4", "3.3", "3.1"}
	if b.Version != "" {
		versions = append([]string{b.Version}, versions...)
	}
	var lastErr error
	for _, v := range versions {
		d := &TuyaDevice{ID: b.ID, IP: ip, Key: []byte(b.Key), Version: v, Timeout: 1500 * time.Millisecond}
		if err := d.Connect(); err != nil {
			lastErr = err
			continue
		}
		dps, err := d.Status()
		d.Close()
		if err != nil {
			lastErr = err
			continue
		}
		if len(dps) == 0 {
			continue
		}
		return v, bulbLayout(dps), nil
	}
	if lastErr == nil {
		lastErr = errors.New("no reply")
	}
	return "", "", lastErr
}

// findBulbs locates enabled bulbs on the local network (no inbound firewall
// rules needed: it only makes outgoing connections to port 6668).
func findBulbs() {
	bulbStatMu.Lock()
	if finding {
		bulbStatMu.Unlock()
		return
	}
	finding = true
	bulbStatMu.Unlock()
	defer func() { bulbStatMu.Lock(); finding = false; bulbStatMu.Unlock() }()

	bulbMu.Lock()
	defer bulbMu.Unlock()
	cfg := loadConfig()
	var want []TuyaBulb
	for _, b := range cfg.Tuya.Bulbs {
		if b.Enabled {
			want = append(want, b)
			setBulbStat(b.ID, "searching")
		}
	}
	if len(want) == 0 {
		return
	}
	hosts := scanTuyaHosts()
	logf("smart bulbs: %d host(s) answer on port %d", len(hosts), tuyaPort)
	found := map[string]TuyaBulb{}
	taken := map[string]bool{}
	// Known addresses first, then everything else.
	for _, b := range want {
		if b.IP == "" {
			continue
		}
		if v, l, err := probeBulb(b.IP, b); err == nil {
			b.Version, b.Layout = v, l
			found[b.ID] = b
			taken[b.IP] = true
		}
	}
	// Remaining bulbs: check every other address in parallel; each address
	// tries the keys of the bulbs not found yet.
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, ip := range hosts {
		if taken[ip] {
			continue
		}
		wg.Add(1)
		go func(ip string) {
			defer wg.Done()
			for _, b := range want {
				mu.Lock()
				_, done := found[b.ID]
				mu.Unlock()
				if done {
					continue
				}
				if v, l, err := probeBulb(ip, b); err == nil {
					b.IP, b.Version, b.Layout = ip, v, l
					mu.Lock()
					if _, dup := found[b.ID]; !dup {
						found[b.ID] = b
					}
					mu.Unlock()
					return
				}
			}
		}(ip)
	}
	wg.Wait()
	for _, b := range want {
		if f, ok := found[b.ID]; ok {
			logf("smart bulb %q at %s (protocol %s, layout %s)", b.Name, f.IP, f.Version, f.Layout)
			setBulbStat(b.ID, "ok")
		} else {
			logf("smart bulb %q not found on the network", b.Name)
			setBulbStat(b.ID, "not found on your network")
		}
	}
	updateConfig(func(c *Config) {
		for i, b := range c.Tuya.Bulbs {
			if f, ok := found[b.ID]; ok {
				c.Tuya.Bulbs[i].IP, c.Tuya.Bulbs[i].Version, c.Tuya.Bulbs[i].Layout = f.IP, f.Version, f.Layout
			}
		}
	})
}

// paintBulbs sets every enabled bulb whose address is known. It reports
// whether any enabled bulb is missing or unreachable.
func paintBulbs(t ThemeDef, s Settings) (missing bool) {
	bulbMu.Lock()
	defer bulbMu.Unlock()
	cfg := loadConfig()
	var (
		wg  sync.WaitGroup
		mu  sync.Mutex
		idx int
	)
	for _, b := range cfg.Tuya.Bulbs {
		if !b.Enabled {
			continue
		}
		c := bulbColor(t, b.Role, idx, s.Brightness)
		idx++
		if b.IP == "" || b.Version == "" {
			missing = true
			continue
		}
		wg.Add(1)
		go func(b TuyaBulb) {
			defer wg.Done()
			d := &TuyaDevice{ID: b.ID, IP: b.IP, Key: []byte(b.Key), Version: b.Version}
			err := d.Connect()
			if err == nil {
				layout := b.Layout
				if layout == "" {
					layout = "B"
				}
				err = d.Set(bulbDPS(layout, c))
				d.Close()
			}
			if err != nil {
				logf("smart bulb %q: %v", b.Name, err)
				setBulbStat(b.ID, "not reachable")
				mu.Lock()
				missing = true
				mu.Unlock()
				return
			}
			setBulbStat(b.ID, "ok")
		}(b)
	}
	wg.Wait()
	return missing
}

var (
	lastFind    time.Time
	bulbApplyCh = make(chan [2]any, 1)
)

// applyBulbs paints the bulbs in the background (network calls can take a
// few seconds), searching the network when a bulb has moved or is new.
func applyBulbs(t ThemeDef, s Settings) {
	select {
	case <-bulbApplyCh: // drop a queued older request: latest wins
	default:
	}
	bulbApplyCh <- [2]any{t, s}
}

func bulbWorker() {
	for req := range bulbApplyCh {
		t, s := req[0].(ThemeDef), req[1].(Settings)
		if paintBulbs(t, s) && time.Since(lastFind) > 10*time.Minute {
			lastFind = time.Now()
			findBulbs()
			paintBulbs(t, s)
		}
	}
}

func init() { go bulbWorker() }
