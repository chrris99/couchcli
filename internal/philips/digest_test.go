package philips

import (
	"strings"
	"testing"
)

func TestParseDigestChallenge(t *testing.T) {
	h := `Digest realm="OpenTV", nonce="abc123", opaque="xyz", qop="auth", algorithm=MD5`
	d, err := parseDigestChallenge(h)
	if err != nil {
		t.Fatalf("parseDigestChallenge: %v", err)
	}
	if d.realm != "OpenTV" || d.nonce != "abc123" || d.opaque != "xyz" || d.qop != "auth" || d.algorithm != "MD5" {
		t.Fatalf("unexpected challenge: %+v", d)
	}
}

func TestParseDigestChallenge_MultiQOP(t *testing.T) {
	h := `Digest realm="r", nonce="n", qop="auth,auth-int"`
	d, err := parseDigestChallenge(h)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if d.qop != "auth" {
		t.Fatalf("expected qop=auth, got %q", d.qop)
	}
}

func TestParseDigestChallenge_RejectsNonDigest(t *testing.T) {
	if _, err := parseDigestChallenge(`Basic realm="x"`); err == nil {
		t.Fatal("expected error for non-digest scheme")
	}
}

func TestAuthorize_ContainsExpectedFields(t *testing.T) {
	d, err := parseDigestChallenge(`Digest realm="r", nonce="n", qop="auth"`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	d.user = "alice"
	d.pass = "s3cret"

	hdr, err := d.authorize("POST", "/6/pair/grant")
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
	d, _ := parseDigestChallenge(`Digest realm="r", nonce="n", qop="auth"`)
	d.user = "u"
	d.pass = "p"
	h1, _ := d.authorize("GET", "/")
	h2, _ := d.authorize("GET", "/")
	if !strings.Contains(h1, "nc=00000001") || !strings.Contains(h2, "nc=00000002") {
		t.Fatalf("nc did not increment:\n%s\n%s", h1, h2)
	}
}

func TestAuthorize_KnownVectorMD5QOPAuth(t *testing.T) {
	// Verify response computation against a hand-computed expectation by
	// replaying the same inputs through md5hex.
	d, _ := parseDigestChallenge(`Digest realm="r", nonce="n", qop="auth"`)
	d.user = "u"
	d.pass = "p"
	hdr, err := d.authorize("POST", "/x")
	if err != nil {
		t.Fatal(err)
	}
	// Extract response value and verify it equals md5(HA1:n:nc:cnonce:auth:HA2)
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

func extractField(hdr, key string) string {
	// Quoted value: key="value"
	if i := strings.Index(hdr, key+`="`); i >= 0 {
		rest := hdr[i+len(key)+2:]
		if end := strings.IndexByte(rest, '"'); end >= 0 {
			return rest[:end]
		}
	}
	// Unquoted: key=value
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
