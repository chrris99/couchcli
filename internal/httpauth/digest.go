// Package httpauth provides http.RoundTripper implementations for HTTP
// authentication schemes.
package httpauth

import (
	"crypto/md5"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
)

// Digest is an http.RoundTripper that answers HTTP Digest challenges (RFC 7616,
// MD5 / qop=auth) with the configured credentials. The first request to a host
// draws a 401 carrying the server challenge; Digest parses it, caches it, and
// transparently retries. Later requests are pre-authorized from the cached
// challenge, so they cost a single round trip; if the cached nonce has gone
// stale the retry picks up the fresh challenge from the 401.
//
// A Digest value carries one credential pair. Install it as an *http.Client's
// Transport; it is safe for concurrent use.
type Digest struct {
	Username string
	Password string
	// Base is the underlying transport. A nil Base uses http.DefaultTransport.
	Base http.RoundTripper

	mu     sync.Mutex
	cached *challenge
}

func (d *Digest) base() http.RoundTripper {
	if d.Base != nil {
		return d.Base
	}
	return http.DefaultTransport
}

func (d *Digest) snapshot() *challenge {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.cached
}

func (d *Digest) store(c *challenge) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.cached = c
}

func (d *Digest) RoundTrip(req *http.Request) (*http.Response, error) {
	base := d.base()

	// On first attempt pre-authorize from a cached challenge if any,
	// otherwise send bare and let the server challenge.
	first := req
	if c := d.snapshot(); c != nil {
		authed, err := d.authorized(req, c)
		if err != nil {
			return nil, err
		}
		first = authed
	}
	resp, err := base.RoundTrip(first)
	if err != nil || resp.StatusCode != http.StatusUnauthorized {
		return resp, err
	}

	// 401: parse the (possibly refreshed) challenge, cache it, and retry once.
	c, err := parseChallenge(resp.Header.Get("WWW-Authenticate"))
	if err != nil {
		// Not a Digest challenge we understand — hand the 401 back untouched.
		return resp, nil
	}
	resp.Body.Close()
	d.store(c)

	authed, err := d.authorized(req, c)
	if err != nil {
		return nil, err
	}
	return base.RoundTrip(authed)
}

// authorized returns a clone of req carrying an Authorization header for c.
func (d *Digest) authorized(req *http.Request, c *challenge) (*http.Request, error) {
	clone := req.Clone(req.Context())
	if req.Body != nil {
		if req.GetBody == nil {
			return nil, errors.New("httpauth: request body cannot be rewound for digest retry")
		}
		body, err := req.GetBody()
		if err != nil {
			return nil, fmt.Errorf("httpauth: rewind body: %w", err)
		}
		clone.Body = body
	}
	header, err := c.authorize(d.Username, d.Password, req.Method, req.URL.RequestURI())
	if err != nil {
		return nil, err
	}
	clone.Header.Set("Authorization", header)
	return clone, nil
}

// challenge is a parsed WWW-Authenticate Digest challenge plus the client nonce
// counter used to answer it. It is immutable after parseChallenge except for
// nc, which is bumped atomically per authorized request.
type challenge struct {
	realm     string
	nonce     string
	opaque    string
	qop       string // "auth"; empty falls back to RFC 2069 legacy mode
	algorithm string // "MD5"

	nc atomic.Uint32
}

var errBadChallenge = errors.New("httpauth: not a digest challenge")

// parseChallenge parses a WWW-Authenticate header value seeded with the
// server's realm/nonce/opaque/qop/algorithm.
func parseChallenge(header string) (*challenge, error) {
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
			end := strings.IndexByte(rest[1:], '"')
			if end < 0 {
				break
			}
			val = rest[1 : 1+end]
			rest = rest[1+end+1:]
		} else {
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
		rest = strings.TrimLeft(rest, ", \t")
	}

	c := &challenge{
		realm:     params["realm"],
		nonce:     params["nonce"],
		opaque:    params["opaque"],
		qop:       pickQOP(params["qop"]),
		algorithm: params["algorithm"],
	}
	if c.algorithm == "" {
		c.algorithm = "MD5"
	}
	if c.nonce == "" {
		return nil, errBadChallenge
	}
	return c, nil
}

// pickQOP chooses "auth" when the server advertises it (the value may list
// "auth,auth-int").
func pickQOP(qop string) string {
	for opt := range strings.SplitSeq(qop, ",") {
		if strings.TrimSpace(opt) == "auth" {
			return "auth"
		}
	}
	return strings.TrimSpace(qop)
}

// authorize builds an Authorization header value for method+uri. It bumps the
// nonce counter and is safe for concurrent use.
func (c *challenge) authorize(user, pass, method, uri string) (string, error) {
	if strings.ToUpper(c.algorithm) != "MD5" {
		return "", fmt.Errorf("httpauth: unsupported digest algorithm %q", c.algorithm)
	}

	ha1 := md5hex(user + ":" + c.realm + ":" + pass)
	ha2 := md5hex(method + ":" + uri)

	nc := fmt.Sprintf("%08x", c.nc.Add(1))
	cnonce, err := newCNonce()
	if err != nil {
		return "", err
	}

	var response string
	if c.qop == "auth" {
		response = md5hex(ha1 + ":" + c.nonce + ":" + nc + ":" + cnonce + ":auth:" + ha2)
	} else {
		response = md5hex(ha1 + ":" + c.nonce + ":" + ha2)
	}

	var b strings.Builder
	b.WriteString(`Digest username="`)
	b.WriteString(user)
	b.WriteString(`", realm="`)
	b.WriteString(c.realm)
	b.WriteString(`", nonce="`)
	b.WriteString(c.nonce)
	b.WriteString(`", uri="`)
	b.WriteString(uri)
	b.WriteString(`", algorithm=MD5, response="`)
	b.WriteString(response)
	b.WriteString(`"`)
	if c.opaque != "" {
		b.WriteString(`, opaque="`)
		b.WriteString(c.opaque)
		b.WriteString(`"`)
	}
	if c.qop == "auth" {
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
