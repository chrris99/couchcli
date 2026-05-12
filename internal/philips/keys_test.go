package philips

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestKeys_Standby_PostsStandbyKey(t *testing.T) {
	var captured map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/6/input/key" {
			http.NotFound(w, r)
			return
		}
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &captured)
	}))
	defer srv.Close()

	c := newTestClient(t, srv)
	if err := c.Keys.Standby(context.Background()); err != nil {
		t.Fatalf("Keys.Standby: %v", err)
	}
	if captured["key"] != KeyStandby {
		t.Errorf("captured=%+v, want key=%q", captured, KeyStandby)
	}
}
