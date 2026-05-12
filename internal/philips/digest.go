package philips

// Hand-rolled HTTP Digest Access Authentication client (RFC 7616 / 2617).
// Philips TVs require Digest auth with algorithm=MD5, qop=auth on the
// /6/pair/grant endpoint and all subsequent secured requests. We keep this
// in-tree rather than pulling a runtime dep because the surface is tiny and
// the protocol is fixed.

import (
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
)

// digestAuth holds the parsed server challenge plus the credentials used to
// answer subsequent requests on the same connection.
type digestAuth struct {
	user, pass string

	realm     string
	nonce     string
	opaque    string
	qop       string // "auth" (the only mode we support)
	algorithm string // "MD5" (default if absent)

	nc uint32 // client nonce counter (atomic)
}

// errBadChallenge is returned when the WWW-Authenticate header does not look
// like a Digest challenge we know how to answer.
var errBadChallenge = errors.New("not a digest challenge")

// parseDigestChallenge parses a WWW-Authenticate header value into a
// digestAuth seeded with the server-provided realm/nonce/opaque/qop/algorithm.
// The caller fills in user and pass before calling authorize.
func parseDigestChallenge(header string) (*digestAuth, error) {
	// Header format: `Digest realm="...", nonce="...", qop="auth", ...`
	rest := strings.TrimSpace(header)
	if !strings.HasPrefix(strings.ToLower(rest), "digest ") {
		return nil, errBadChallenge
	}
	rest = strings.TrimSpace(rest[len("Digest"):])

	params := map[string]string{}
	for len(rest) > 0 {
		eq := strings.IndexByte(rest, '=')
		if eq < 0 {
			break
		}
		key := strings.TrimSpace(rest[:eq])
		rest = rest[eq+1:]
		var val string
		if len(rest) > 0 && rest[0] == '"' {
			// Quoted value.
			end := strings.IndexByte(rest[1:], '"')
			if end < 0 {
				break
			}
			val = rest[1 : 1+end]
			rest = rest[1+end+1:]
		} else {
			// Unquoted token, terminated by comma or end.
			end := strings.IndexByte(rest, ',')
			if end < 0 {
				val = strings.TrimSpace(rest)
				rest = ""
			} else {
				val = strings.TrimSpace(rest[:end])
				rest = rest[end:]
			}
		}
		params[strings.ToLower(key)] = val
		// Skip the comma separator and any whitespace.
		rest = strings.TrimLeft(rest, ", \t")
	}

	d := &digestAuth{
		realm:     params["realm"],
		nonce:     params["nonce"],
		opaque:    params["opaque"],
		qop:       pickQOP(params["qop"]),
		algorithm: params["algorithm"],
	}
	if d.algorithm == "" {
		d.algorithm = "MD5"
	}
	if d.nonce == "" {
		return nil, errBadChallenge
	}
	return d, nil
}

// pickQOP chooses "auth" if the server advertises it (it may list "auth,auth-int").
func pickQOP(qop string) string {
	for _, opt := range strings.Split(qop, ",") {
		if strings.TrimSpace(opt) == "auth" {
			return "auth"
		}
	}
	return strings.TrimSpace(qop)
}

// authorize builds the Authorization header value for a given method+URI.
// Increments the internal nc counter; safe for concurrent use.
func (d *digestAuth) authorize(method, uri string) (string, error) {
	if d == nil {
		return "", errors.New("nil digestAuth")
	}
	if strings.ToUpper(d.algorithm) != "MD5" {
		return "", fmt.Errorf("unsupported digest algorithm %q", d.algorithm)
	}

	ha1 := md5hex(d.user + ":" + d.realm + ":" + d.pass)
	ha2 := md5hex(method + ":" + uri)

	ncVal := atomic.AddUint32(&d.nc, 1)
	nc := fmt.Sprintf("%08x", ncVal)
	cnonce, err := newCNonce()
	if err != nil {
		return "", err
	}

	var response string
	if d.qop == "auth" {
		response = md5hex(ha1 + ":" + d.nonce + ":" + nc + ":" + cnonce + ":auth:" + ha2)
	} else {
		// RFC 2069 legacy fallback (no qop).
		response = md5hex(ha1 + ":" + d.nonce + ":" + ha2)
	}

	var b strings.Builder
	b.WriteString(`Digest username="`)
	b.WriteString(d.user)
	b.WriteString(`", realm="`)
	b.WriteString(d.realm)
	b.WriteString(`", nonce="`)
	b.WriteString(d.nonce)
	b.WriteString(`", uri="`)
	b.WriteString(uri)
	b.WriteString(`", algorithm=MD5, response="`)
	b.WriteString(response)
	b.WriteString(`"`)
	if d.opaque != "" {
		b.WriteString(`, opaque="`)
		b.WriteString(d.opaque)
		b.WriteString(`"`)
	}
	if d.qop == "auth" {
		b.WriteString(`, qop=auth, nc=`)
		b.WriteString(nc)
		b.WriteString(`, cnonce="`)
		b.WriteString(cnonce)
		b.WriteString(`"`)
	}
	return b.String(), nil
}

func md5hex(s string) string {
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

func newCNonce() (string, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(b[:]), nil
}
