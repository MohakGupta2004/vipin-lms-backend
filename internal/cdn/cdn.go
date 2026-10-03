// Package cdn signs Cloud CDN URLs so private bucket objects can be served through the CDN.
package cdn

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// ErrBadSignature means signed query parameters are missing, tampered with or expired.
var ErrBadSignature = errors.New("cdn: invalid or expired signature")

// Signer creates Cloud CDN signed URL parameters. The key is decoded once and never exposed.
type Signer struct {
	domain  string
	keyName string
	key     []byte
}

// NewSigner builds a Signer. domain is the CDN origin (http(s)://host, no path), keyName the signed
// URL key's name on the backend bucket and base64Key its secret (base64url, padded or not).
func NewSigner(domain, keyName, base64Key string) (*Signer, error) {
	domain = strings.TrimRight(strings.TrimSpace(domain), "/")
	if !strings.HasPrefix(domain, "http://") && !strings.HasPrefix(domain, "https://") {
		return nil, errors.New("cdn: domain must start with http:// or https://")
	}
	if strings.TrimSpace(keyName) == "" {
		return nil, errors.New("cdn: key name is empty")
	}
	base64Key = strings.TrimRight(strings.TrimSpace(base64Key), "=")
	if base64Key == "" {
		return nil, errors.New("cdn: signing key is empty")
	}
	key, err := base64.RawURLEncoding.DecodeString(base64Key)
	if err != nil {
		return nil, fmt.Errorf("cdn: signing key is not base64url: %w", err)
	}
	return &Signer{domain: domain, keyName: strings.TrimSpace(keyName), key: key}, nil
}

// SignPrefix returns query parameters valid for every URL under domain+prefix until expires.
func (s *Signer) SignPrefix(prefix string, expires time.Time) string {
	policy := fmt.Sprintf("URLPrefix=%s&Expires=%d&KeyName=%s",
		base64.URLEncoding.EncodeToString([]byte(s.domain+prefix)), expires.Unix(), s.keyName)
	mac := hmac.New(sha1.New, s.key)
	mac.Write([]byte(policy))
	return policy + "&Signature=" + base64.URLEncoding.EncodeToString(mac.Sum(nil))
}

// Verify checks query parameters made by SignPrefix and returns the signed path prefix
// (for example "/courses/c/videos/v/hls/") plus the parameters re-encoded in canonical order.
func (s *Signer) Verify(q url.Values, now time.Time) (prefix, query string, err error) {
	encodedPrefix := q.Get("URLPrefix")
	if q.Get("KeyName") != s.keyName || encodedPrefix == "" {
		return "", "", ErrBadSignature
	}
	expires, err := strconv.ParseInt(q.Get("Expires"), 10, 64)
	if err != nil || now.Unix() >= expires {
		return "", "", ErrBadSignature
	}
	gotSig, err := base64.URLEncoding.DecodeString(q.Get("Signature"))
	if err != nil {
		return "", "", ErrBadSignature
	}
	policy := fmt.Sprintf("URLPrefix=%s&Expires=%d&KeyName=%s", encodedPrefix, expires, s.keyName)
	mac := hmac.New(sha1.New, s.key)
	mac.Write([]byte(policy))
	if !hmac.Equal(gotSig, mac.Sum(nil)) {
		return "", "", ErrBadSignature
	}
	full, err := base64.URLEncoding.DecodeString(encodedPrefix)
	if err != nil || !strings.HasPrefix(string(full), s.domain+"/") {
		return "", "", ErrBadSignature
	}
	return strings.TrimPrefix(string(full), s.domain), policy + "&Signature=" + q.Get("Signature"), nil
}

// URL returns the unsigned CDN URL of an object path.
func (s *Signer) URL(path string) string {
	return s.domain + "/" + strings.TrimLeft(path, "/")
}
