package jointspace

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestSignKnownVector(t *testing.T) {
	secret := []byte("topsecret")
	var ts int64 = 1700000000
	pin := "1234"
	mac := hmac.New(sha256.New, secret)
	_, _ = fmt.Fprintf(mac, "%d%s", ts, pin)
	want := base64.StdEncoding.EncodeToString([]byte(hex.EncodeToString(mac.Sum(nil))))

	got := sign(secret, ts, pin)
	if got != want {
		t.Fatalf("sign mismatch:\n got=%s\nwant=%s", got, want)
	}
	// base64 of a 64-char hex string is exactly 88 characters (no padding).
	if len(got) != 88 {
		t.Errorf("sign length=%d want 88", len(got))
	}
}

// pairServer simulates a TV serving /6/pair/request and /6/pair/grant. When
// expectedPIN is non-empty the grant endpoint returns INVALID_PIN unless the
// submitted PIN matches.
type pairServer struct {
	expectedPIN  string
	timestamp    int64
	authKey      string
	requestCount int32
	grantCount   int32
	digestReqs   int32 // grant requests that carried Authorization
}

func newPairServer() *pairServer {
	return &pairServer{
		expectedPIN: "1234",
		timestamp:   1700000000,
		authKey:     "ak-deadbeef",
	}
}

func (p *pairServer) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/6/pair/request":
			atomic.AddInt32(&p.requestCount, 1)
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error_id":  "SUCCESS",
				"auth_key":  p.authKey,
				"timestamp": p.timestamp,
				"timeout":   60,
			})
		case "/6/pair/grant":
			atomic.AddInt32(&p.grantCount, 1)
			// First request: 401 with a digest challenge.
			if r.Header.Get("Authorization") == "" {
				w.Header().Set("WWW-Authenticate", `Digest realm="OpenTV", nonce="abc123", qop="auth"`)
				http.Error(w, `{"error_id":"UNAUTHORIZED"}`, http.StatusUnauthorized)
				return
			}
			atomic.AddInt32(&p.digestReqs, 1)
			body := struct {
				Auth pairGrantAuth `json:"auth"`
			}{}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body.Auth.PIN != p.expectedPIN {
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"error_id":   "INVALID_PIN",
					"error_text": "wrong PIN",
				})
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"error_id": "SUCCESS"})
		default:
			http.NotFound(w, r)
		}
	})
}

func TestPairFlow_HappyPath(t *testing.T) {
	ps := newPairServer()
	srv := httptest.NewServer(ps.handler())
	defer srv.Close()

	c := newTestClient(t, srv)
	dev := DeviceInfo{
		ID:         "abcd1234abcd5678",
		DeviceName: "test",
		DeviceOS:   "darwin",
		AppID:      "1",
		AppName:    "couchcli",
		Type:       "native",
	}

	res, err := c.Pair.Run(context.Background(), dev, func(ctx context.Context) (string, error) {
		return "1234", nil
	})
	if err != nil {
		t.Fatalf("Pair: %v", err)
	}
	if res.DeviceID != dev.ID {
		t.Errorf("DeviceID=%q want %q", res.DeviceID, dev.ID)
	}
	if res.AuthKey != ps.authKey {
		t.Errorf("AuthKey=%q want %q", res.AuthKey, ps.authKey)
	}
	if atomic.LoadInt32(&ps.requestCount) != 1 {
		t.Errorf("request count=%d want 1", ps.requestCount)
	}
	// grant: one un-auth'd 401 + one auth'd retry via httpauth.Digest.
	if atomic.LoadInt32(&ps.digestReqs) != 1 {
		t.Errorf("digest grant count=%d want 1", ps.digestReqs)
	}
}

func TestPairFlow_BadPIN(t *testing.T) {
	ps := newPairServer()
	srv := httptest.NewServer(ps.handler())
	defer srv.Close()

	c := newTestClient(t, srv)
	_, err := c.Pair.Run(context.Background(), DeviceInfo{ID: "id1"}, func(ctx context.Context) (string, error) {
		return "9999", nil
	})
	if !errors.Is(err, ErrPINRejected) {
		t.Fatalf("got %v, want ErrPINRejected", err)
	}
}

func TestPairFlow_PromptTimeoutWithShortCtx(t *testing.T) {
	srv := httptest.NewServer(newPairServer().handler())
	defer srv.Close()
	c := newTestClient(t, srv)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := c.Pair.Run(ctx, DeviceInfo{ID: "id1"}, func(ctx context.Context) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	})
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("got %v, want ErrTimeout", err)
	}
}
