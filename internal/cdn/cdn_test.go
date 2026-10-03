package cdn

import (
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestSignPrefix(t *testing.T) {
	raw := []byte("0123456789abcdef")
	for name, enc := range map[string]string{
		"padded":   base64.URLEncoding.EncodeToString(raw),
		"unpadded": base64.RawURLEncoding.EncodeToString(raw),
	} {
		t.Run(name, func(t *testing.T) {
			s, err := NewSigner("http://1.2.3.4/", "key1", enc)
			if err != nil {
				t.Fatal(err)
			}
			exp := time.Unix(1700000000, 0)
			got := s.SignPrefix("/courses/c/videos/v/hls/", exp)

			policy := "URLPrefix=" + base64.RawURLEncoding.EncodeToString([]byte("http://1.2.3.4/courses/c/videos/v/hls/")) +
				"&Expires=1700000000&KeyName=key1"
			mac := hmac.New(sha1.New, raw)
			mac.Write([]byte(policy))
			want := policy + "&Signature=" + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
			if got != want {
				t.Fatalf("got %q\nwant %q", got, want)
			}
			if u := s.URL("courses/c/x.m3u8"); u != "http://1.2.3.4/courses/c/x.m3u8" {
				t.Fatalf("url = %q", u)
			}
		})
	}
}

func TestNewSignerRejectsBadInput(t *testing.T) {
	good := base64.URLEncoding.EncodeToString([]byte("k"))
	cases := map[string][3]string{
		"no scheme":  {"1.2.3.4", "k", good},
		"empty key":  {"http://x", "k", ""},
		"empty name": {"http://x", "", good},
		"bad base64": {"http://x", "k", "!!!"},
	}
	for name, c := range cases {
		if _, err := NewSigner(c[0], c[1], c[2]); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestVerify(t *testing.T) {
	s, err := NewSigner("http://1.2.3.4", "key1", base64.URLEncoding.EncodeToString([]byte("0123456789abcdef")))
	if err != nil {
		t.Fatal(err)
	}
	now := time.Unix(1700000000, 0)
	signed := s.SignPrefix("/courses/c/videos/v/hls/", now.Add(time.Minute))

	q, _ := url.ParseQuery(signed)
	prefix, query, err := s.Verify(q, now)
	if err != nil || prefix != "/courses/c/videos/v/hls/" || query != signed {
		t.Fatalf("valid: got (%q, %q, %v)", prefix, query, err)
	}

	if _, _, err := s.Verify(q, now.Add(2*time.Minute)); !errors.Is(err, ErrBadSignature) {
		t.Errorf("expired: want ErrBadSignature, got %v", err)
	}

	tampered, _ := url.ParseQuery(signed)
	tampered.Set("URLPrefix", base64.URLEncoding.EncodeToString([]byte("http://1.2.3.4/courses/c/videos/other/hls/")))
	if _, _, err := s.Verify(tampered, now); !errors.Is(err, ErrBadSignature) {
		t.Errorf("tampered prefix: want ErrBadSignature, got %v", err)
	}

	tampered, _ = url.ParseQuery(signed)
	tampered.Set("Expires", "1900000000")
	if _, _, err := s.Verify(tampered, now); !errors.Is(err, ErrBadSignature) {
		t.Errorf("tampered expiry: want ErrBadSignature, got %v", err)
	}

	if _, _, err := s.Verify(url.Values{}, now); !errors.Is(err, ErrBadSignature) {
		t.Errorf("empty: want ErrBadSignature, got %v", err)
	}

	// Only the four "key=" separators; no "=" padding that players might percent-encode.
	if strings.Count(signed, "=") != 4 {
		t.Errorf("signed query has base64 padding: %q", signed)
	}

	// Links signed with padding (before it was dropped) still verify.
	policy := "URLPrefix=" + base64.URLEncoding.EncodeToString([]byte("http://1.2.3.4/courses/c/videos/v/hls/x")) +
		"&Expires=1700000060&KeyName=key1"
	mac := hmac.New(sha1.New, []byte("0123456789abcdef"))
	mac.Write([]byte(policy))
	padded, _ := url.ParseQuery(policy + "&Signature=" + base64.URLEncoding.EncodeToString(mac.Sum(nil)))
	if prefix, _, err := s.Verify(padded, now); err != nil || prefix != "/courses/c/videos/v/hls/x" {
		t.Errorf("padded: got (%q, %v)", prefix, err)
	}
}
