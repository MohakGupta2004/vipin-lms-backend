package service

import (
	"testing"

	"github.com/MohakGupta2004/vipin-lms-backend/internal/models"
)

func TestRandomCodeIsSixDigits(t *testing.T) {
	for i := 0; i < 200; i++ {
		c, err := randomCode()
		if err != nil || !validCode(c) {
			t.Fatalf("bad code %q err=%v", c, err)
		}
	}
}

func TestValidCode(t *testing.T) {
	for code, want := range map[string]bool{"123456": true, "000000": true, "12345": false, "1234567": false, "12345a": false, "": false, "١٢٣٤٥٦": false} {
		if validCode(code) != want {
			t.Errorf("validCode(%q) != %v", code, want)
		}
	}
}

func TestHashCodeBindsUserAndPurpose(t *testing.T) {
	s := &VerificationService{hmacKey: []byte("k")}
	base := s.hashCode("u1", models.PurposePasswordReset, "123456")
	if base != s.hashCode("u1", models.PurposePasswordReset, "123456") {
		t.Error("not deterministic")
	}
	if base == s.hashCode("u2", models.PurposePasswordReset, "123456") ||
		base == s.hashCode("u1", models.PurposeEmailVerification, "123456") ||
		base == s.hashCode("u1", models.PurposePasswordReset, "123457") {
		t.Error("hash must differ by user, purpose and code")
	}
}
