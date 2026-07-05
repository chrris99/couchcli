package httpauth

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestParseChallenge(t *testing.T) {
	h := `Digest realm="OpenTV", nonce="abc123", opaque="xyz", qop="auth", algorithm=MD5`
	c, err := parseChallenge(h)
	if err != nil {
		t.Fatalf("parseChallenge: %v", err)
	}
	if c.realm != "OpenTV" || c.nonce != "abc123" || c.opaque != "xyz" || c.qop != "auth" || c.algorithm != "MD5" {
		t.Fatalf("unexpected challenge: %+v", c)
	}
}

func TestParseChallenge_MultiQOP(t *testing.T) {
	c, err := parseChallenge(`Digest realm="r", nonce="n", qop="auth,auth-int"`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if c.qop != "auth" {
		t.Fatalf("expected qop=auth, got %q", c.qop)
	}
}

func TestParseChallenge_RejectsNonDigest(t *testing.T) {
	if _, err := parseChallenge(`Basic realm="x"`); err == nil {
		t.Fatal("expected error for non-digest scheme")
	}
}

func TestAuthorize_ContainsExpectedFields(t *testing.T) {
	c, err := parseChallenge(`Digest realm="r", nonce="n", qop="auth"`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	hdr, err := c.authorize("alice", "s3cret", "POST", "/6/pair/grant")
	if err != nil {
		t.Fatalf("authorize: %v", err)
	}
	for _, want := range []string{
		`username="alice"`,
		`realm="r"`,
		`nonce="n"`,
		`uri="/6/pair/grant"`,
		`algorithm=MD5`,
		`qop=auth`,
		`nc=00000001`,
		`cnonce="`,
		`response="`,
	} {
		if !strings.Contains(hdr, want) {
			t.Errorf("missing %q in header:\n%s", want, hdr)
		}
	}
}

func TestAuthorize_NCIncrements(t *testing.T) {
	c, _ := parseChallenge(`Digest realm="r", nonce="n", qop="auth"`)
	h1, _ := c.authorize("u", "p", "GET", "/")
	h2, _ := c.authorize("u", "p", "GET", "/")
	if !strings.Contains(h1, "nc=00000001") || !strings.Contains(h2, "nc=00000002") {
		t.Fatalf("nc did not increment:\n%s\n%s", h1, h2)
	}
}

func TestAuthorize_KnownVectorMD5QOPAuth(t *testing.T) {
	c, _ := parseChallenge(`Digest realm="r", nonce="n", qop="auth"`)
	hdr, err := c.authorize("u", "p", "POST", "/x")
	if err != nil {
		t.Fatal(err)
	}
	resp := extractField(hdr, "response")
	nc := extractField(hdr, "nc")
	cnonce := extractField(hdr, "cnonce")
	ha1 := md5hex("u:r:p")
	ha2 := md5hex("POST:/x")
	expected := md5hex(ha1 + ":n:" + nc + ":" + cnonce + ":auth:" + ha2)
	if resp != expected {
		t.Fatalf("response mismatch: got %s want %s", resp, expected)
	}
}

// TestDigestRoundTrip exercises the full transport: an unauthenticated request
// draws a 401 + challenge, the retry carries a valid Digest header, and a
// body-carrying POST survives the rewind. A second call reuses the cached
// challenge and pre-authorizes, so it hits the server only once.
func TestDigestRoundTrip(t *testing.T) {
	var reqs int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqs++
		if r.Header.Get("Authorization") == "" {
			w.Header().Set("WWW-Authenticate", `Digest realm="test", nonce="deadbeef", qop="auth", algorithm=MD5`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if !strings.Contains(r.Header.Get("Authorization"), `response="`) {
			t.Errorf("authorized request missing response field: %s", r.Header.Get("Authorization"))
		}
		body, _ := io.ReadAll(r.Body)
		w.Write(body) // echo, to prove the body survived the retry
	}))
	defer srv.Close()

	client := &http.Client{Transport: &Digest{Username: "u", Password: "p"}}

	resp, err := client.Post(srv.URL, "application/json", strings.NewReader(`{"k":"v"}`))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	body, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if string(body) != `{"k":"v"}` {
		t.Fatalf("body not echoed through retry: %q", body)
	}
	if reqs != 2 {
		t.Fatalf("cold request: want 2 server hits (401 + retry), got %d", reqs)
	}

	// Second request should pre-authorize from the cached challenge.
	resp2, err := client.Post(srv.URL, "application/json", strings.NewReader(`{}`))
	if err != nil {
		t.Fatalf("second post: %v", err)
	}
	resp2.Body.Close()
	if reqs != 3 {
		t.Fatalf("cached request: want 1 additional server hit, got %d total", reqs)
	}
}

func extractField(hdr, key string) string {
	if i := strings.Index(hdr, key+`="`); i >= 0 {
		rest := hdr[i+len(key)+2:]
		if end := strings.IndexByte(rest, '"'); end >= 0 {
			return rest[:end]
		}
	}
	if i := strings.Index(hdr, key+"="); i >= 0 {
		rest := hdr[i+len(key)+1:]
		end := strings.IndexAny(rest, ", \t")
		if end < 0 {
			return rest
		}
		return rest[:end]
	}
	return ""
}
