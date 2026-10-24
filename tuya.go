package main

// Local-network control of Tuya-based Wi-Fi bulbs (Feit Electric and many
// other brands) over the Tuya LAN protocol, versions 3.1 to 3.5. Written from
// the protocol notes in TinyTuya (https://github.com/jasonacox/tinytuya, MIT);
// no TinyTuya code is used.

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/md5"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"hash/crc32"
	"io"
	"math"
	"net"
	"strconv"
	"time"
)

const (
	tuyaPrefix55AA = 0x000055AA
	tuyaSuffix55AA = 0x0000AA55
	tuyaPrefix6699 = 0x00006699
	tuyaSuffix6699 = 0x00009966

	tuyaCmdSessStart  = 0x03
	tuyaCmdSessResp   = 0x04
	tuyaCmdSessFinish = 0x05
	tuyaCmdControl    = 0x07
	tuyaCmdStatus     = 0x08
	tuyaCmdHeartbeat  = 0x09
	tuyaCmdDPQuery    = 0x0a
	tuyaCmdControlNew = 0x0d
	tuyaCmdDPQueryNew = 0x10
	tuyaCmdUpdateDPS  = 0x12
	tuyaCmdLanExt     = 0x40
)

var tuyaPort = 6668 // TCP port devices listen on (changed only by tests)

// Commands sent without the "3.x" version header.
var tuyaNoHeader = map[uint32]bool{
	tuyaCmdDPQuery: true, tuyaCmdDPQueryNew: true, tuyaCmdUpdateDPS: true,
	tuyaCmdHeartbeat: true, tuyaCmdSessStart: true, tuyaCmdSessResp: true,
	tuyaCmdSessFinish: true, tuyaCmdLanExt: true,
}

// ---- AES helpers -------------------------------------------------------------

func pkcs7Pad(b []byte) []byte {
	n := aes.BlockSize - len(b)%aes.BlockSize
	return append(append([]byte{}, b...), bytes.Repeat([]byte{byte(n)}, n)...)
}

func pkcs7Unpad(b []byte) ([]byte, error) {
	if len(b) == 0 || len(b)%aes.BlockSize != 0 {
		return nil, errors.New("bad padded length")
	}
	n := int(b[len(b)-1])
	if n == 0 || n > aes.BlockSize || n > len(b) {
		return nil, errors.New("bad padding")
	}
	for _, c := range b[len(b)-n:] {
		if int(c) != n {
			return nil, errors.New("bad padding")
		}
	}
	return b[:len(b)-n], nil
}

func ecbEncrypt(key, plain []byte, pad bool) ([]byte, error) {
	blk, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	if pad {
		plain = pkcs7Pad(plain)
	}
	if len(plain)%aes.BlockSize != 0 {
		return nil, errors.New("ecb: input not a multiple of the block size")
	}
	out := make([]byte, len(plain))
	for i := 0; i < len(plain); i += aes.BlockSize {
		blk.Encrypt(out[i:], plain[i:i+aes.BlockSize])
	}
	return out, nil
}

func ecbDecrypt(key, ct []byte) ([]byte, error) {
	blk, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	if len(ct) == 0 || len(ct)%aes.BlockSize != 0 {
		return nil, errors.New("ecb: ciphertext not a multiple of the block size")
	}
	out := make([]byte, len(ct))
	for i := 0; i < len(ct); i += aes.BlockSize {
		blk.Decrypt(out[i:], ct[i:i+aes.BlockSize])
	}
	return pkcs7Unpad(out)
}

func gcmSeal(key, iv, plain, aad []byte) ([]byte, error) {
	blk, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	g, err := cipher.NewGCM(blk)
	if err != nil {
		return nil, err
	}
	return g.Seal(nil, iv, plain, aad), nil // ciphertext || tag
}

func gcmOpen(key, iv, ctTag, aad []byte) ([]byte, error) {
	blk, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	g, err := cipher.NewGCM(blk)
	if err != nil {
		return nil, err
	}
	return g.Open(nil, iv, ctTag, aad)
}

// ---- framing -------------------------------------------------------------------

type tuyaMsg struct {
	Seq     uint32
	Cmd     uint32
	Retcode uint32
	Payload []byte // plaintext for 6699 frames; raw (still encrypted) for 55AA
	Good    bool   // CRC / HMAC / GCM tag verified
}

// pack55AA builds a 55AA frame (client to device: no return code). With
// hmacKey the integrity field is HMAC-SHA256 (v3.4), otherwise CRC32.
func pack55AA(seq, cmd uint32, payload, hmacKey []byte) []byte {
	endLen := 8
	if hmacKey != nil {
		endLen = 36
	}
	b := make([]byte, 16, 16+len(payload)+endLen)
	binary.BigEndian.PutUint32(b[0:], tuyaPrefix55AA)
	binary.BigEndian.PutUint32(b[4:], seq)
	binary.BigEndian.PutUint32(b[8:], cmd)
	binary.BigEndian.PutUint32(b[12:], uint32(len(payload)+endLen))
	b = append(b, payload...)
	if hmacKey != nil {
		m := hmac.New(sha256.New, hmacKey)
		m.Write(b)
		b = m.Sum(b)
	} else {
		b = binary.BigEndian.AppendUint32(b, crc32.ChecksumIEEE(b))
	}
	return binary.BigEndian.AppendUint32(b, tuyaSuffix55AA)
}

// pack6699 builds a v3.5 frame: AES-GCM with a fresh 12-byte IV, header as AAD.
func pack6699(seq, cmd uint32, plain, key []byte) ([]byte, error) {
	iv := make([]byte, 12)
	if _, err := rand.Read(iv); err != nil {
		return nil, err
	}
	return pack6699IV(seq, cmd, plain, key, iv)
}

func pack6699IV(seq, cmd uint32, plain, key, iv []byte) ([]byte, error) {
	h := make([]byte, 18)
	binary.BigEndian.PutUint32(h[0:], tuyaPrefix6699)
	// h[4:6] reserved = 0
	binary.BigEndian.PutUint32(h[6:], seq)
	binary.BigEndian.PutUint32(h[10:], cmd)
	binary.BigEndian.PutUint32(h[14:], uint32(12+len(plain)+16))
	ct, err := gcmSeal(key, iv, plain, h[4:])
	if err != nil {
		return nil, err
	}
	out := append(h, iv...)
	out = append(out, ct...)
	return binary.BigEndian.AppendUint32(out, tuyaSuffix6699), nil
}

// readFrame reads one complete 55AA or 6699 frame from r.
func readFrame(r io.Reader) ([]byte, error) {
	var hdr [16]byte
	if _, err := io.ReadFull(r, hdr[:4]); err != nil {
		return nil, err
	}
	switch binary.BigEndian.Uint32(hdr[:4]) {
	case tuyaPrefix55AA:
		if _, err := io.ReadFull(r, hdr[4:16]); err != nil {
			return nil, err
		}
		n := binary.BigEndian.Uint32(hdr[12:16])
		if n > 1<<16 {
			return nil, errors.New("tuya: frame too large")
		}
		body := make([]byte, n)
		if _, err := io.ReadFull(r, body); err != nil {
			return nil, err
		}
		return append(hdr[:16:16], body...), nil
	case tuyaPrefix6699:
		h := make([]byte, 18)
		copy(h, hdr[:4])
		if _, err := io.ReadFull(r, h[4:]); err != nil {
			return nil, err
		}
		n := binary.BigEndian.Uint32(h[14:18])
		if n > 1<<16 {
			return nil, errors.New("tuya: frame too large")
		}
		body := make([]byte, n+4) // + suffix
		if _, err := io.ReadFull(r, body); err != nil {
			return nil, err
		}
		return append(h, body...), nil
	}
	return nil, fmt.Errorf("tuya: bad frame prefix %x", hdr[:4])
}

// unpackFrame parses a device-to-client frame. key is the HMAC key for v3.4
// 55AA frames, or the GCM key for 6699 frames; nil means CRC32. hasRetcode
// says whether a 55AA frame carries a return code (device replies do).
func unpackFrame(data, key []byte, hasRetcode bool) (tuyaMsg, error) {
	var m tuyaMsg
	if len(data) < 16 {
		return m, errors.New("tuya: short frame")
	}
	switch binary.BigEndian.Uint32(data[:4]) {
	case tuyaPrefix55AA:
		m.Seq = binary.BigEndian.Uint32(data[4:])
		m.Cmd = binary.BigEndian.Uint32(data[8:])
		n := int(binary.BigEndian.Uint32(data[12:]))
		if len(data) < 16+n {
			return m, errors.New("tuya: truncated frame")
		}
		endLen := 8
		if key != nil {
			endLen = 36
		}
		start := 16
		if hasRetcode && n >= endLen+4 {
			m.Retcode = binary.BigEndian.Uint32(data[16:])
			start = 20
		}
		if 16+n-endLen < start {
			return m, errors.New("tuya: bad frame length")
		}
		covered := data[:16+n-endLen]
		m.Payload = append([]byte{}, data[start:16+n-endLen]...)
		if key != nil {
			mac := hmac.New(sha256.New, key)
			mac.Write(covered)
			m.Good = hmac.Equal(mac.Sum(nil), data[16+n-endLen:16+n-4])
		} else {
			m.Good = crc32.ChecksumIEEE(covered) == binary.BigEndian.Uint32(data[16+n-endLen:])
		}
		return m, nil
	case tuyaPrefix6699:
		if len(data) < 18 {
			return m, errors.New("tuya: short frame")
		}
		m.Seq = binary.BigEndian.Uint32(data[6:])
		m.Cmd = binary.BigEndian.Uint32(data[10:])
		n := int(binary.BigEndian.Uint32(data[14:]))
		if n < 28 || len(data) < 18+n {
			return m, errors.New("tuya: truncated frame")
		}
		iv := data[18:30]
		plain, err := gcmOpen(key, iv, data[30:18+n], data[4:18])
		if err != nil {
			return m, fmt.Errorf("tuya: frame did not decrypt (wrong key?)")
		}
		m.Good = true
		m.Payload = plain
		return m, nil
	}
	return m, errors.New("tuya: bad frame prefix")
}

// ---- device connection -----------------------------------------------------------

// TuyaDevice is one connection to a Tuya device on the local network.
type TuyaDevice struct {
	ID      string
	IP      string
	Key     []byte // local key (16 bytes)
	Version string // "3.1" .. "3.5"

	Timeout time.Duration // reply wait; default 4s

	conn       net.Conn
	sessionKey []byte
	seq        uint32
}

func (d *TuyaDevice) ver() float64 {
	v, _ := strconv.ParseFloat(d.Version, 64)
	if v == 0 {
		return 3.3
	}
	return v
}

func (d *TuyaDevice) versionHeader() []byte {
	return append([]byte(d.Version), make([]byte, 12)...)
}

func (d *TuyaDevice) Close() {
	if d.conn != nil {
		d.conn.Close()
		d.conn = nil
	}
	d.sessionKey = nil
}

// Connect opens the TCP connection and, for v3.4+, negotiates a session key.
func (d *TuyaDevice) Connect() error {
	if len(d.Key) != 16 {
		return errors.New("local key must be 16 characters")
	}
	c, err := net.DialTimeout("tcp", net.JoinHostPort(d.IP, strconv.Itoa(tuyaPort)), 3*time.Second)
	if err != nil {
		return err
	}
	d.conn = c
	d.seq = 1
	if d.ver() >= 3.4 {
		if err := d.negotiate(); err != nil {
			d.Close()
			return fmt.Errorf("session key: %v", err)
		}
	}
	return nil
}

func (d *TuyaDevice) key() []byte {
	if d.sessionKey != nil {
		return d.sessionKey
	}
	return d.Key
}

// encode turns a plaintext command payload into a frame for this version.
func (d *TuyaDevice) encode(cmd uint32, payload []byte) ([]byte, error) {
	seq := d.seq
	d.seq++
	v := d.ver()
	key := d.key()
	switch {
	case v >= 3.5:
		if !tuyaNoHeader[cmd] {
			payload = append(d.versionHeader(), payload...)
		}
		return pack6699(seq, cmd, payload, key)
	case v >= 3.4:
		if !tuyaNoHeader[cmd] {
			payload = append(d.versionHeader(), payload...)
		}
		ct, err := ecbEncrypt(key, payload, true)
		if err != nil {
			return nil, err
		}
		return pack55AA(seq, cmd, ct, key), nil
	case v >= 3.2:
		ct, err := ecbEncrypt(key, payload, true)
		if err != nil {
			return nil, err
		}
		if !tuyaNoHeader[cmd] {
			ct = append(d.versionHeader(), ct...)
		}
		return pack55AA(seq, cmd, ct, nil), nil
	default: // 3.1: only CONTROL is encrypted
		if cmd == tuyaCmdControl {
			ct, err := ecbEncrypt(key, payload, true)
			if err != nil {
				return nil, err
			}
			b64 := base64.StdEncoding.EncodeToString(ct)
			sum := md5.Sum([]byte("data=" + b64 + "||lpv=3.1||" + string(key)))
			payload = []byte("3.1" + hex.EncodeToString(sum[:])[8:24] + b64)
		}
		return pack55AA(seq, cmd, payload, nil), nil
	}
}

// decodePayload strips headers and decrypts a reply payload to JSON bytes.
func (d *TuyaDevice) decodePayload(m tuyaMsg) ([]byte, error) {
	p := m.Payload
	v := d.ver()
	key := d.key()
	if v >= 3.5 {
		if len(p) >= 4 {
			p = p[4:] // return code
		}
	} else if v >= 3.4 {
		if len(p) == 0 {
			return nil, nil
		}
		var err error
		if p, err = ecbDecrypt(key, p); err != nil {
			return nil, err
		}
	}
	if v < 3.2 && bytes.HasPrefix(p, []byte("3.1")) && len(p) > 19 {
		b, err := base64.StdEncoding.DecodeString(string(p[19:]))
		if err != nil {
			return nil, err
		}
		return ecbDecrypt(key, b)
	}
	if len(p) >= 15 && bytes.HasPrefix(p, []byte("3.")) {
		p = p[15:]
	}
	if v >= 3.2 && v < 3.4 && len(p) > 0 && p[0] != '{' {
		var err error
		if p, err = ecbDecrypt(key, p); err != nil {
			return nil, err
		}
	}
	return bytes.TrimRight(p, "\x00"), nil
}

func (d *TuyaDevice) send(cmd uint32, payload []byte) error {
	f, err := d.encode(cmd, payload)
	if err != nil {
		return err
	}
	d.conn.SetWriteDeadline(time.Now().Add(3 * time.Second))
	_, err = d.conn.Write(f)
	return err
}

func (d *TuyaDevice) recv() (tuyaMsg, error) {
	wait := d.Timeout
	if wait == 0 {
		wait = 4 * time.Second
	}
	d.conn.SetReadDeadline(time.Now().Add(wait))
	f, err := readFrame(d.conn)
	if err != nil {
		return tuyaMsg{}, err
	}
	var key []byte
	if d.ver() >= 3.4 {
		key = d.key()
	}
	m, err := unpackFrame(f, key, true)
	if err != nil {
		return m, err
	}
	if !m.Good {
		return m, errors.New("tuya: reply failed its integrity check (wrong key?)")
	}
	return m, nil
}

func (d *TuyaDevice) negotiate() error {
	nonce := make([]byte, 16)
	if _, err := rand.Read(nonce); err != nil {
		return err
	}
	if err := d.send(tuyaCmdSessStart, nonce); err != nil {
		return err
	}
	var m tuyaMsg
	var err error
	for i := 0; i < 4; i++ {
		if m, err = d.recv(); err != nil {
			return err
		}
		if m.Cmd == tuyaCmdSessResp {
			break
		}
	}
	if m.Cmd != tuyaCmdSessResp {
		return errors.New("no handshake reply")
	}
	p := m.Payload
	if d.ver() >= 3.5 {
		if len(p) >= 4 {
			p = p[4:]
		}
	} else if p, err = ecbDecrypt(d.Key, p); err != nil {
		return err
	}
	if len(p) < 48 {
		return errors.New("short handshake reply")
	}
	remote := p[:16]
	mac := hmac.New(sha256.New, d.Key)
	mac.Write(nonce)
	if !hmac.Equal(mac.Sum(nil), p[16:48]) {
		return errors.New("wrong local key")
	}
	mac = hmac.New(sha256.New, d.Key)
	mac.Write(remote)
	if err := d.send(tuyaCmdSessFinish, mac.Sum(nil)); err != nil {
		return err
	}
	x := make([]byte, 16)
	for i := range x {
		x[i] = nonce[i] ^ remote[i]
	}
	if d.ver() >= 3.5 {
		ct, err := gcmSeal(d.Key, nonce[:12], x, nil)
		if err != nil {
			return err
		}
		d.sessionKey = ct[:16] // the 16 bytes after the IV in IV||ciphertext||tag
	} else {
		k, err := ecbEncrypt(d.Key, x, false)
		if err != nil {
			return err
		}
		d.sessionKey = k
	}
	return nil
}

// Status returns the device's current data points.
func (d *TuyaDevice) Status() (map[string]any, error) {
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	var cmd uint32 = tuyaCmdDPQuery
	payload := fmt.Sprintf(`{"gwId":%q,"devId":%q,"uid":%q,"t":%q}`, d.ID, d.ID, d.ID, ts)
	if d.ver() >= 3.4 {
		cmd, payload = tuyaCmdDPQueryNew, `{}`
	}
	if err := d.send(cmd, []byte(payload)); err != nil {
		return nil, err
	}
	for i := 0; i < 5; i++ {
		m, err := d.recv()
		if err != nil {
			return nil, err
		}
		js, err := d.decodePayload(m)
		if err != nil {
			return nil, err
		}
		if len(js) == 0 {
			continue
		}
		if bytes.Contains(js, []byte("data unvalid")) {
			return nil, errors.New("device needs explicit data point list")
		}
		var r struct {
			DevID string         `json:"devId"`
			DPS   map[string]any `json:"dps"`
			Data  struct {
				DPS map[string]any `json:"dps"`
			} `json:"data"`
		}
		if json.Unmarshal(js, &r) != nil {
			continue
		}
		if r.DevID != "" && r.DevID != d.ID {
			return nil, errors.New("a different device answered")
		}
		if r.DPS != nil {
			return r.DPS, nil
		}
		if r.Data.DPS != nil {
			return r.Data.DPS, nil
		}
	}
	return nil, errors.New("no status reply")
}

// Set writes data points (e.g. {"20": true, "24": "..."}).
func (d *TuyaDevice) Set(dps map[string]any) error {
	b, _ := json.Marshal(dps)
	now := time.Now().Unix()
	var cmd uint32 = tuyaCmdControl
	payload := fmt.Sprintf(`{"devId":%q,"uid":%q,"t":"%d","dps":%s}`, d.ID, d.ID, now, b)
	if d.ver() >= 3.4 {
		cmd = tuyaCmdControlNew
		payload = fmt.Sprintf(`{"protocol":5,"t":%d,"data":{"dps":%s}}`, now, b)
	}
	if err := d.send(cmd, []byte(payload)); err != nil {
		return err
	}
	// Read the acknowledgement (and any status push) so errors surface, but
	// don't fail if the bulb simply stays quiet.
	m, err := d.recv()
	if err != nil {
		if ne, ok := err.(net.Error); ok && ne.Timeout() {
			return nil
		}
		return err
	}
	if m.Retcode != 0 {
		return fmt.Errorf("device rejected the command (code %d)", m.Retcode)
	}
	return nil
}

// ---- bulbs --------------------------------------------------------------------

// Tuya bulbs come in two data-point layouts:
//
//	"B" (most bulbs since ~2019): 20 on/off, 21 mode, 22 brightness 10-1000,
//	    23 white temperature 0-1000, 24 colour "HHHHSSSSVVVV" (hue 0-360, s/v 0-1000)
//	"A" (older): 1 on/off, 2 mode, 3 brightness 25-255, 4 temperature 0-255,
//	    5 colour "rrggbb0hhhssvv"
func bulbLayout(dps map[string]any) string {
	if _, ok := dps["20"]; ok {
		return "B"
	}
	if _, ok := dps["24"]; ok {
		return "B"
	}
	return "A"
}

func rgbToHSV(c RGB) (h, s, v float64) {
	r, g, b := float64(c.R)/255, float64(c.G)/255, float64(c.B)/255
	mx := math.Max(r, math.Max(g, b))
	mn := math.Min(r, math.Min(g, b))
	v = mx
	d := mx - mn
	if mx > 0 {
		s = d / mx
	}
	if d == 0 {
		return 0, s, v
	}
	switch mx {
	case r:
		h = math.Mod((g-b)/d, 6)
	case g:
		h = (b-r)/d + 2
	default:
		h = (r-g)/d + 4
	}
	h *= 60
	if h < 0 {
		h += 360
	}
	return
}

// isWhitish reports colors better shown with the bulb's white LEDs.
func isWhitish(c RGB) bool {
	_, s, v := rgbToHSV(c)
	return s < 0.22 && v > 0.5
}

// bulbDPS works out the data points that show color c (already scaled for
// brightness) on a bulb of the given layout.
func bulbDPS(layout string, c RGB) map[string]any {
	h, s, v := rgbToHSV(c)
	if v < 0.02 {
		if layout == "B" {
			return map[string]any{"20": false}
		}
		return map[string]any{"1": false}
	}
	white := isWhitish(c)
	if layout == "B" {
		if white {
			// warm vs cool from the red/blue balance
			temp := 1000 * clamp01(0.5+(float64(c.B)-float64(c.R))/255)
			return map[string]any{"20": true, "21": "white", "22": int(math.Round(10 + 990*v)), "23": int(math.Round(temp))}
		}
		hex := fmt.Sprintf("%04x%04x%04x", int(math.Round(h)), int(math.Round(s*1000)), int(math.Round(v*1000)))
		return map[string]any{"20": true, "21": "colour", "24": hex}
	}
	if white {
		temp := 255 * clamp01(0.5+(float64(c.B)-float64(c.R))/255)
		return map[string]any{"1": true, "2": "white", "3": int(math.Round(25 + 230*v)), "4": int(math.Round(temp))}
	}
	full := hsvToRGB(h, s, 1)
	hex := fmt.Sprintf("%02x%02x%02x%04x%02x%02x", full.R, full.G, full.B, int(math.Round(h)), int(math.Round(s*255)), int(math.Round(v*255)))
	return map[string]any{"1": true, "2": "colour", "5": hex}
}

func clamp01(x float64) float64 { return math.Max(0, math.Min(1, x)) }

func hsvToRGB(h, s, v float64) RGB {
	c := v * s
	x := c * (1 - math.Abs(math.Mod(h/60, 2)-1))
	m := v - c
	var r, g, b float64
	switch {
	case h < 60:
		r, g, b = c, x, 0
	case h < 120:
		r, g, b = x, c, 0
	case h < 180:
		r, g, b = 0, c, x
	case h < 240:
		r, g, b = 0, x, c
	case h < 300:
		r, g, b = x, 0, c
	default:
		r, g, b = c, 0, x
	}
	to := func(f float64) uint8 { return uint8(math.Round((f + m) * 255)) }
	return RGB{to(r), to(g), to(b)}
}

// ---- color assignment ---------------------------------------------------------

// bulbColor picks the color for the i-th of n enabled bulbs. role is "main",
// "accent" or "auto" (spread the livery's stripes, or main/accent, across bulbs).
func bulbColor(t ThemeDef, role string, i int, brightness int) RGB {
	f := float64(brightness) / 100
	switch role {
	case "main":
		if len(t.Stripes) > 0 {
			c, _ := t.stripeAt(0.5)
			return c.Scale(f)
		}
		return mustHex(t.Base).Scale(f)
	case "accent":
		return mustHex(t.Accent).Scale(f)
	}
	palette := t.Stripes
	if len(palette) == 0 {
		palette = []string{t.Base, t.Accent}
	}
	return mustHex(palette[i%len(palette)]).Scale(f)
}
