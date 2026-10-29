package storage

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"strings"
	"testing"
	"time"

	gcs "cloud.google.com/go/storage"
)

func TestUploadFormPolicy(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	pemKey := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})

	opts, err := uploadFormOptions("courses/c/videos/v/hls/", "video/iso.segment", "public, max-age=60", MaxHLSFileSize, time.Hour)
	if err != nil {
		t.Fatal(err)
	}
	opts.GoogleAccessID = "api@project.iam.gserviceaccount.com"
	opts.PrivateKey = pemKey
	policy, err := gcs.GenerateSignedPostPolicyV4("bucket", "", opts)
	if err != nil {
		t.Fatal(err)
	}

	if _, ok := policy.Fields["key"]; ok {
		t.Error(`fields must not fix "key": the browser names each file`)
	}
	if policy.Fields["content-type"] != "video/iso.segment" || policy.Fields["cache-control"] != "public, max-age=60" {
		t.Errorf("fields = %v", policy.Fields)
	}
	raw, err := base64.StdEncoding.DecodeString(policy.Fields["policy"])
	if err != nil {
		t.Fatal(err)
	}
	var doc struct {
		Conditions []json.RawMessage `json:"conditions"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	conds := make([]string, len(doc.Conditions))
	for i, c := range doc.Conditions {
		conds[i] = string(c)
	}
	all := strings.Join(conds, " ")
	for _, want := range []string{
		`["starts-with","$key","courses/c/videos/v/hls/"]`,
		`["content-length-range",1,67108864]`,
		`{"content-type":"video/iso.segment"}`,
		`{"bucket":"bucket"}`,
	} {
		if !strings.Contains(all, want) {
			t.Errorf("policy is missing %s; conditions: %s", want, all)
		}
	}
	if strings.Contains(all, `{"key"`) {
		t.Errorf("policy pins a single key: %s", all)
	}
}

func TestUploadFormOptionsRejectsUnsafePrefix(t *testing.T) {
	for _, prefix := range []string{"", "/", "courses/c/videos/v/hls"} {
		if _, err := uploadFormOptions(prefix, "video/mp4", "", 1, time.Hour); err == nil {
			t.Errorf("prefix %q accepted", prefix)
		}
	}
	if _, err := uploadFormOptions("a/", "", "", 1, time.Hour); err == nil {
		t.Error("empty content type accepted")
	}
}
