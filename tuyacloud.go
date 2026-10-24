package main

// Minimal Tuya cloud (OpenAPI) client, used once to read each device's local
// key from the person's own Tuya developer project. Day-to-day control is
// local only. Request signing follows https://developer.tuya.com/en/docs/iot/new-singnature

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Tuya data centers, keyed by the region names the panel shows.
var tuyaRegions = map[string]string{
	"us":   "openapi.tuyaus.com",      // Western America
	"us-e": "openapi-ueaz.tuyaus.com", // Eastern America
	"eu":   "openapi.tuyaeu.com",      // Central Europe
	"eu-w": "openapi-weaz.tuyaeu.com", // Western Europe
	"in":   "openapi.tuyain.com",      // India
	"sg":   "openapi-sg.iotbing.com",  // Singapore
	"cn":   "openapi.tuyacn.com",      // China
}

type tuyaCloud struct {
	host, id, secret, token string
	client                  *http.Client
}

type TuyaCloudDevice struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	LocalKey string `json:"local_key"`
	Category string `json:"category"`
	Product  string `json:"product_name"`
	Online   bool   `json:"online"`
	UID      string `json:"uid"`
}

func newTuyaCloud(region, id, secret string) (*tuyaCloud, error) {
	host, ok := tuyaRegions[region]
	if !ok {
		return nil, fmt.Errorf("unknown region %q", region)
	}
	if id == "" || secret == "" {
		return nil, errors.New("Access ID and Access Secret are both needed")
	}
	return &tuyaCloud{host: host, id: strings.TrimSpace(id), secret: strings.TrimSpace(secret), client: &http.Client{Timeout: 15 * time.Second}}, nil
}

// tuyaSign builds the request signature: HMAC-SHA256(secret,
// client_id + [access_token] + t + METHOD\nSHA256(body)\n\nPATH?sorted_query), upper-case hex.
func tuyaSign(id, secret, token, t, method, body, pathQuery string) string {
	sum := sha256.Sum256([]byte(body))
	str := id + token + t + method + "\n" + hex.EncodeToString(sum[:]) + "\n\n" + pathQuery
	m := hmac.New(sha256.New, []byte(secret))
	m.Write([]byte(str))
	return strings.ToUpper(hex.EncodeToString(m.Sum(nil)))
}

func (c *tuyaCloud) get(path string, query map[string]string, out any) error {
	keys := make([]string, 0, len(query))
	for k := range query {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var signQ, urlQ []string
	for _, k := range keys {
		signQ = append(signQ, k+"="+query[k])
		urlQ = append(urlQ, url.QueryEscape(k)+"="+url.QueryEscape(query[k]))
	}
	pq, uq := path, path
	if len(keys) > 0 {
		pq += "?" + strings.Join(signQ, "&")
		uq += "?" + strings.Join(urlQ, "&")
	}
	t := strconv.FormatInt(time.Now().UnixMilli(), 10)
	req, err := http.NewRequest("GET", "https://"+c.host+uq, nil)
	if err != nil {
		return err
	}
	req.Header.Set("client_id", c.id)
	req.Header.Set("t", t)
	req.Header.Set("sign_method", "HMAC-SHA256")
	req.Header.Set("sign", tuyaSign(c.id, c.secret, c.token, t, "GET", "", pq))
	if c.token != "" {
		req.Header.Set("access_token", c.token)
	}
	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return err
	}
	var env struct {
		Success bool            `json:"success"`
		Code    int             `json:"code"`
		Msg     string          `json:"msg"`
		Result  json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(b, &env); err != nil {
		return fmt.Errorf("Tuya cloud: unexpected reply (HTTP %d)", resp.StatusCode)
	}
	if !env.Success {
		return tuyaCloudError(env.Code, env.Msg)
	}
	return json.Unmarshal(env.Result, out)
}

// tuyaCloudError turns common Tuya error codes into advice.
func tuyaCloudError(code int, msg string) error {
	switch code {
	case 1004:
		return errors.New("Tuya says the signature is wrong: check the Access Secret")
	case 1005, 2009:
		return errors.New("Tuya doesn't recognise that Access ID")
	case 1106, 1100:
		return errors.New("Tuya refused access: make sure your Smart Life account is linked to the cloud project (Devices > Link App Account)")
	case 28841105, 28841101:
		return errors.New("the cloud project's API trial has expired: extend it in the Tuya developer site (Cloud > Cloud Services > IoT Core)")
	case 2406:
		return errors.New("wrong data center: pick the region your cloud project uses")
	}
	if msg == "" {
		msg = "request failed"
	}
	return fmt.Errorf("Tuya cloud: %s (code %d)", msg, code)
}

func (c *tuyaCloud) login() error {
	c.token = ""
	var r struct {
		AccessToken string `json:"access_token"`
	}
	if err := c.get("/v1.0/token", map[string]string{"grant_type": "1"}, &r); err != nil {
		return err
	}
	if r.AccessToken == "" {
		return errors.New("Tuya cloud: no access token")
	}
	c.token = r.AccessToken
	return nil
}

// Devices lists every device linked to the project, with local keys.
func (c *tuyaCloud) Devices() ([]TuyaCloudDevice, error) {
	if err := c.login(); err != nil {
		return nil, err
	}
	var all []TuyaCloudDevice
	q := map[string]string{"size": "50"}
	for page := 0; page < 20; page++ {
		var r struct {
			Devices    []TuyaCloudDevice `json:"devices"`
			HasMore    bool              `json:"has_more"`
			LastRowKey string            `json:"last_row_key"`
		}
		if err := c.get("/v1.0/iot-01/associated-users/devices", q, &r); err != nil {
			return nil, err
		}
		all = append(all, r.Devices...)
		if !r.HasMore || r.LastRowKey == "" {
			break
		}
		q["last_row_key"] = r.LastRowKey
	}
	// The per-user listing is the one guaranteed to carry local keys.
	users := map[string]bool{}
	for _, d := range all {
		if d.UID != "" {
			users[d.UID] = true
		}
	}
	byID := map[string]int{}
	for i, d := range all {
		byID[d.ID] = i
	}
	for uid := range users {
		q := map[string]string{"page_size": "75", "source_type": "tuyaUser", "source_id": uid}
		for page := 0; page < 20; page++ {
			var r struct {
				List       []TuyaCloudDevice `json:"list"`
				HasMore    bool              `json:"has_more"`
				LastRowKey string            `json:"last_row_key"`
			}
			if err := c.get("/v1.3/iot-03/devices", q, &r); err != nil {
				break
			}
			for _, d := range r.List {
				if i, ok := byID[d.ID]; ok {
					if d.LocalKey != "" {
						all[i].LocalKey = d.LocalKey
					}
				} else {
					byID[d.ID] = len(all)
					all = append(all, d)
				}
			}
			if !r.HasMore || r.LastRowKey == "" {
				break
			}
			q["last_row_key"] = r.LastRowKey
		}
	}
	return all, nil
}

// isLightCategory reports Tuya product categories that are lights.
func isLightCategory(cat string) bool {
	switch cat {
	case "dj", "dd", "xdd", "fwd", "dc", "fsd", "tyndj", "gyd", "sxd", "tgq", "mbd":
		return true
	}
	return false
}
