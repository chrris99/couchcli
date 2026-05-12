package philips

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// PairService handles the JointSpace pairing handshake (request → PIN →
// grant) and the heuristic for when pairing is required.
type PairService service

// Pair endpoints + scopes.
const (
	pairRequestPath = "/6/pair/request"
	pairGrantPath   = "/6/pair/grant"

	// pinTimeout caps how long we wait for the user to enter the PIN. The
	// server-side window is typically 60s.
	pinTimeout = 90 * time.Second
)

// Default scopes — the same triplet bcyran/philipstv uses.
var defaultScopes = []string{"read", "write", "control"}

// DeviceInfo describes the *client* (us) to the TV. Visible in the TV's
// "paired devices" list. Field tags match JointSpace JSON exactly.
type DeviceInfo struct {
	ID         string `json:"id"`
	DeviceName string `json:"device_name"`
	DeviceOS   string `json:"device_os"`
	AppID      string `json:"app_id"`
	AppName    string `json:"app_name"`
	Type       string `json:"type"`
}

// PairResult is the durable credential pair the caller stores in their config.
type PairResult struct {
	DeviceID string // request digest username
	AuthKey  string // request digest password
}

// PinPrompter is provided by the caller to collect the PIN from the user. The
// callback receives a context with a deadline equal to the pair window; it
// must return promptly when the context is done.
type PinPrompter func(ctx context.Context) (string, error)

// Typed errors mapped to friendly CLI messages.
var (
	ErrPINRejected    = errors.New("PIN rejected")
	ErrPairingRefused = errors.New("pairing refused")
	ErrTimeout        = errors.New("pairing timed out")
	ErrNotPairable    = errors.New("TV does not support pairing")
)

// pairRequestBody is the body of POST /6/pair/request.
type pairRequestBody struct {
	Scope  []string   `json:"scope"`
	Device DeviceInfo `json:"device"`
}

// pairRequestResp is the response of POST /6/pair/request.
type pairRequestResp struct {
	ErrorID   string `json:"error_id"`
	ErrorText string `json:"error_text,omitempty"`
	AuthKey   string `json:"auth_key"`
	Timestamp int64  `json:"timestamp"`
	Timeout   int    `json:"timeout"`
}

// pairGrantAuth is the auth sub-object of POST /6/pair/grant.
type pairGrantAuth struct {
	PIN            string `json:"pin"`
	AuthTimestamp  int64  `json:"auth_timestamp"`
	AuthSignature  string `json:"auth_signature"`
}

// pairGrantBody is the body of POST /6/pair/grant.
type pairGrantBody struct {
	Auth   pairGrantAuth `json:"auth"`
	Device DeviceInfo    `json:"device"`
}

// pairGrantResp is the response of POST /6/pair/grant.
type pairGrantResp struct {
	ErrorID   string `json:"error_id"`
	ErrorText string `json:"error_text,omitempty"`
}

// Run executes the full pair handshake on this Service's Client. The client
// must be a Secure (HTTPS:1926) endpoint with NO digest credentials yet
// attached. Run sets digest credentials on it as part of step 2 (grant) so
// the client is immediately reusable.
func (s *PairService) Run(ctx context.Context, dev DeviceInfo, prompt PinPrompter) (PairResult, error) {
	c := s.client
	if dev.ID == "" {
		id, err := genDeviceID()
		if err != nil {
			return PairResult{}, fmt.Errorf("generate device id: %w", err)
		}
		dev.ID = id
	}

	// 1. POST /6/pair/request (no auth).
	var reqResp pairRequestResp
	body := pairRequestBody{Scope: defaultScopes, Device: dev}
	if err := c.Do(ctx, http.MethodPost, pairRequestPath, body, &reqResp); err != nil {
		// Map APIError to our typed sentinels when possible.
		if ae := asAPIError(err); ae != nil {
			return PairResult{}, mapRequestError(ae)
		}
		return PairResult{}, fmt.Errorf("pair request: %w", err)
	}
	if reqResp.ErrorID != "SUCCESS" {
		return PairResult{}, mapRequestError(&APIError{ID: reqResp.ErrorID, Text: reqResp.ErrorText})
	}
	if reqResp.AuthKey == "" || reqResp.Timestamp == 0 {
		return PairResult{}, fmt.Errorf("pair request: missing auth_key/timestamp in response")
	}

	// 2. Prompt user for PIN with a bounded deadline.
	window := pinTimeout
	if reqResp.Timeout > 0 {
		serverWindow := time.Duration(reqResp.Timeout) * time.Second
		if serverWindow < window {
			window = serverWindow
		}
	}
	pinCtx, cancel := context.WithTimeout(ctx, window)
	defer cancel()
	pin, err := prompt(pinCtx)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			return PairResult{}, ErrTimeout
		}
		return PairResult{}, err
	}
	pin = strings.TrimSpace(pin)
	if pin == "" {
		return PairResult{}, fmt.Errorf("empty PIN")
	}

	// 3. Compute auth signature: base64(hex(HMAC-SHA256(secret, ts+pin))).
	signature := sign(philipsSecret, reqResp.Timestamp, pin)

	// 4. POST /6/pair/grant with digest auth (user=dev.ID, pass=auth_key).
	c.WithDigest(dev.ID, reqResp.AuthKey)
	var grantResp pairGrantResp
	grantBody := pairGrantBody{
		Auth: pairGrantAuth{
			PIN:            pin,
			AuthTimestamp:  reqResp.Timestamp,
			AuthSignature:  signature,
		},
		Device: dev,
	}
	if err := c.Do(ctx, http.MethodPost, pairGrantPath, grantBody, &grantResp); err != nil {
		if ae := asAPIError(err); ae != nil {
			return PairResult{}, mapGrantError(ae)
		}
		return PairResult{}, fmt.Errorf("pair grant: %w", err)
	}
	if grantResp.ErrorID != "SUCCESS" {
		return PairResult{}, mapGrantError(&APIError{ID: grantResp.ErrorID, Text: grantResp.ErrorText})
	}

	return PairResult{DeviceID: dev.ID, AuthKey: reqResp.AuthKey}, nil
}

// Required reports whether Run must be executed before this client can be
// used: explicit "digest_auth_pairing" hint wins, otherwise any secure
// (HTTPS:1926) endpoint at API v6+ needs it.
func (s *PairService) Required(info SystemInfo) bool {
	if info.PairingType == "digest_auth_pairing" {
		return true
	}
	return s.client.Secure() && info.APIVersionMajor >= 6
}

// sign computes the JointSpace pair-grant signature.
//
//	signature = base64( hex( HMAC-SHA256(secret, ts+pin) ) )
//
// timestamp is decimal-encoded without padding; pin is the user-typed string.
func sign(secret []byte, timestamp int64, pin string) string {
	mac := hmac.New(sha256.New, secret)
	fmt.Fprintf(mac, "%d%s", timestamp, pin)
	hexed := hex.EncodeToString(mac.Sum(nil))
	return base64.StdEncoding.EncodeToString([]byte(hexed))
}

// genDeviceID returns 16 lowercase hex characters from crypto/rand.
func genDeviceID() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}

func asAPIError(err error) *APIError {
	var ae *APIError
	if errors.As(err, &ae) {
		return ae
	}
	return nil
}

func mapRequestError(ae *APIError) error {
	switch ae.ID {
	case "CONCURRENT_PAIRING":
		return fmt.Errorf("%w: another pairing is in progress on the TV", ErrPairingRefused)
	case "NOT_AUTHORIZED":
		return fmt.Errorf("%w: %s", ErrNotPairable, ae.Text)
	default:
		return fmt.Errorf("%w: %s", ErrPairingRefused, ae.Text)
	}
}

func mapGrantError(ae *APIError) error {
	switch ae.ID {
	case "INVALID_PIN":
		return ErrPINRejected
	case "TIMEOUT":
		return ErrTimeout
	default:
		return fmt.Errorf("%w: %s", ErrPairingRefused, ae.Text)
	}
}
