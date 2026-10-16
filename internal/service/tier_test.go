package service

import (
	"errors"
	"reflect"
	"testing"

	"github.com/MohakGupta2004/vipin-lms-backend/internal/models"
)

func TestNormalizeTiers(t *testing.T) {
	tests := []struct {
		name    string
		in      []string
		want    []string
		wantErr bool
	}{
		{"nil gives default", nil, []string{"plus", "premium"}, false},
		{"empty stays empty (ultra only)", []string{}, []string{}, false},
		{"dedupes", []string{"plus", "plus", "free"}, []string{"plus", "free"}, false},
		{"drops ultra", []string{"ultra", "premium"}, []string{"premium"}, false},
		{"only ultra is ultra only", []string{"ultra"}, []string{}, false},
		{"rejects unknown", []string{"gold"}, nil, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := normalizeTiers(tc.in, defaultContentTiers)
			if tc.wantErr {
				if !errors.Is(err, ErrInvalidInput) {
					t.Fatalf("err = %v, want ErrInvalidInput", err)
				}
				return
			}
			if err != nil || !reflect.DeepEqual(got, tc.want) {
				t.Errorf("normalizeTiers(%v) = %v, %v; want %v", tc.in, got, err, tc.want)
			}
		})
	}
}

func TestNormalizeTiersDoesNotAliasDefault(t *testing.T) {
	got, _ := normalizeTiers(nil, defaultContentTiers)
	got[0] = "free"
	if defaultContentTiers[0] != "plus" {
		t.Error("normalizeTiers returned the shared default slice")
	}
}

func TestLegacyFreeTiers(t *testing.T) {
	if got := legacyFreeTiers([]string{"plus"}, true, defaultContentTiers); !reflect.DeepEqual(got, []string{"plus", "free"}) {
		t.Errorf("true: got %v", got)
	}
	if got := legacyFreeTiers([]string{"free"}, false, defaultContentTiers); !reflect.DeepEqual(got, []string{"plus", "premium"}) {
		t.Errorf("false from free-only: got %v", got)
	}
	if got := legacyFreeTiers([]string{"free", "premium"}, false, defaultContentTiers); !reflect.DeepEqual(got, []string{"premium"}) {
		t.Errorf("false keeps others: got %v", got)
	}
}

func TestDefaultVideoTiers(t *testing.T) {
	if got := defaultVideoTiers(models.VideoKindOffline); !reflect.DeepEqual(got, []string{"premium"}) {
		t.Errorf("offline default = %v", got)
	}
	if got := defaultVideoTiers(models.VideoKindMeet); len(got) != 0 {
		t.Errorf("meet default = %v, want ultra only", got)
	}
}
