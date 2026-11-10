// Package license activates and validates openv6 Pro license keys sold
// through Polar (one-time purchase, a key works on 1 or 3 machines).
//
// The Polar customer-portal endpoints used here need no secret, so they are
// safe to call from an installed app. A key is checked online when it is
// activated and then once a day; when the check cannot reach Polar the last
// good result stays valid for a grace period, so a network outage never
// stops someone's proxies.
package license

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Polar organization selling openv6 and the license benefits that count as
// openv6 Pro (other products of the same organization do not).
var (
	OrgID    = "48aba47f-4780-4e21-9be6-e32fdb455403"
	Benefits = map[string]int{
		"e2f37fe8-4ecd-430d-a02e-8b8265487fa8": 1, // openv6 Pro (1 machine)
		"33bd84ab-1bfd-4e58-a5d2-6463f9dc0da6": 3, // openv6 Pro (3 machines)
	}
	APIBase = "https://api.polar.sh"
	BuyURL  = "https://thephuc.xyz/openv6/#pricing"
)

const (
	// Grace: how long a key stays valid without reaching Polar.
	Grace = 30 * 24 * time.Hour
	// Recheck: how often an active key is validated online.
	Recheck = 24 * time.Hour

	FreeProxies = 3
	FreeNodes   = 1
)

// State is what the panel stores about the installed key.
type State struct {
	Key          string `json:"key,omitempty"`
	ActivationID string `json:"activation_id,omitempty"`
	Status       string `json:"status"` // free | active | invalid
	Machines     int    `json:"machines,omitempty"`
	Email        string `json:"email,omitempty"`
	DisplayKey   string `json:"display_key,omitempty"`
	ValidatedAt  int64  `json:"validated_at,omitempty"` // last successful online check
	CheckedAt    int64  `json:"checked_at,omitempty"`   // last attempt
	Error        string `json:"error,omitempty"`
}

// Pro reports whether Pro features are unlocked right now. A last check in
// the future (clock moved back) does not extend the grace period.
func (s State) Pro(now time.Time) bool {
	age := now.Sub(time.Unix(s.ValidatedAt, 0))
	return s.Status == "active" && s.Key != "" && age < Grace && age > -24*time.Hour
}

// ErrRejected means Polar answered and the key is not usable (wrong key,
// revoked, activation limit reached, another product's key).
type ErrRejected struct {
	Msg    string
	Status int
}

func (e ErrRejected) Error() string { return e.Msg }

var client = &http.Client{Timeout: 20 * time.Second}

type activationResp struct {
	ID         string     `json:"id"`
	LicenseKey keyPayload `json:"license_key"`
}

type keyPayload struct {
	Activation *struct {
		Meta map[string]any `json:"meta"`
	} `json:"activation"`
	BenefitID  string `json:"benefit_id"`
	DisplayKey string `json:"display_key"`
	Status     string `json:"status"`
	Customer   struct {
		Email string `json:"email"`
	} `json:"customer"`
}

func post(ctx context.Context, path string, body any, out any) error {
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(APIBase, "/")+path, bytes.NewReader(b))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "openv6")
	res, err := client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(res.Body, 1<<20))
	switch {
	case res.StatusCode >= 200 && res.StatusCode < 300:
		if out != nil {
			return json.Unmarshal(data, out)
		}
		return nil
	case res.StatusCode == 403 || res.StatusCode == 404 || res.StatusCode == 422 || res.StatusCode == 400:
		var e struct {
			Detail any    `json:"detail"`
			Error  string `json:"error"`
		}
		if json.Unmarshal(data, &e) != nil || (e.Detail == nil && e.Error == "") {
			// not Polar (captive portal, proxy, firewall): not a verdict on the key
			return fmt.Errorf("license server unreachable (HTTP %d from something in between)", res.StatusCode)
		}
		msg := fmt.Sprint(e.Detail)
		switch res.StatusCode {
		case 404:
			msg = "license key not found, revoked or not valid for this machine"
		case 403:
			msg = "license key cannot be activated: " + msg + " (activation limit reached? deactivate it on another machine first)"
		}
		return ErrRejected{Msg: msg, Status: res.StatusCode}
	default:
		return fmt.Errorf("license server: HTTP %d", res.StatusCode)
	}
}

func checkBenefit(k keyPayload) (int, error) {
	n, ok := Benefits[k.BenefitID]
	if !ok {
		return 0, ErrRejected{Msg: "this key belongs to another product, not openv6"}
	}
	if k.Status != "" && k.Status != "granted" {
		return 0, ErrRejected{Msg: "license key is " + k.Status}
	}
	return n, nil
}

// Activate reserves one machine slot for key.
func Activate(ctx context.Context, key, label, machineID string) (State, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return State{}, ErrRejected{Msg: "enter a license key"}
	}
	var r activationResp
	err := post(ctx, "/v1/customer-portal/license-keys/activate", map[string]any{
		"key": key, "organization_id": OrgID, "label": label,
		"meta": map[string]any{"machine": machineID},
	}, &r)
	if err != nil {
		return State{}, err
	}
	n, err := checkBenefit(r.LicenseKey)
	if err != nil {
		_ = Deactivate(ctx, key, r.ID)
		return State{}, err
	}
	now := time.Now().Unix()
	return State{Key: key, ActivationID: r.ID, Status: "active", Machines: n, Email: r.LicenseKey.Customer.Email,
		DisplayKey: r.LicenseKey.DisplayKey, ValidatedAt: now, CheckedAt: now}, nil
}

// Validate re-checks an activated key online and returns the new state.
// A network error keeps the previous result (grace period); only a
// definite "not found / not allowed" from Polar revokes it. machine must
// match the machine the key was activated on.
func Validate(ctx context.Context, st State, machine string) State {
	if st.Key == "" {
		return State{Status: "free"}
	}
	st.CheckedAt = time.Now().Unix()
	var k keyPayload
	err := post(ctx, "/v1/customer-portal/license-keys/validate", map[string]any{
		"key": st.Key, "organization_id": OrgID, "activation_id": st.ActivationID,
	}, &k)
	var rej ErrRejected
	switch {
	case errors.As(err, &rej) && (rej.Status == 403 || rej.Status == 404):
		st.Status, st.Error = "invalid", rej.Msg
		return st
	case errors.As(err, &rej):
		st.Error = "license server answered unexpectedly: " + rej.Msg
		return st
	case err != nil:
		st.Error = "could not reach the license server: " + err.Error()
		return st
	}
	n, err := checkBenefit(k)
	if err != nil {
		st.Status, st.Error = "invalid", err.Error()
		return st
	}
	if k.Activation != nil && machine != "" {
		if m, _ := k.Activation.Meta["machine"].(string); m != "" && m != machine {
			st.Status, st.Error = "invalid", "this license was activated on another machine; activate it again here"
			return st
		}
	}
	st.Status, st.Machines, st.ValidatedAt, st.Error = "active", n, time.Now().Unix(), ""
	if k.DisplayKey != "" {
		st.DisplayKey = k.DisplayKey
	}
	if k.Customer.Email != "" {
		st.Email = k.Customer.Email
	}
	return st
}

// Deactivate frees this machine's slot so the key can be used elsewhere.
func Deactivate(ctx context.Context, key, activationID string) error {
	if key == "" || activationID == "" {
		return nil
	}
	return post(ctx, "/v1/customer-portal/license-keys/deactivate", map[string]any{
		"key": key, "organization_id": OrgID, "activation_id": activationID,
	}, nil)
}
