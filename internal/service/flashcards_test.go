package service

import (
	"errors"
	"strings"
	"testing"

	"github.com/MohakGupta2004/vipin-lms-backend/internal/models"
)

func TestValidateCardText(t *testing.T) {
	tests := []struct {
		name        string
		front, back string
		wantErr     bool
	}{
		{"ok", "Q", "A", false},
		{"max lengths", strings.Repeat("é", maxFlashcardFrontLength), strings.Repeat("é", maxFlashcardBackLength), false},
		{"empty front", "", "A", true},
		{"empty back", "Q", "", true},
		{"front too long", strings.Repeat("a", maxFlashcardFrontLength+1), "A", true},
		{"back too long", "Q", strings.Repeat("a", maxFlashcardBackLength+1), true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateCardText(tt.front, tt.back)
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if err != nil && !errors.Is(err, ErrInvalidInput) {
				t.Errorf("err = %v, want ErrInvalidInput", err)
			}
		})
	}
}

func TestValidateRating(t *testing.T) {
	for rating, wantErr := range map[int]bool{-1: true, 0: true, 1: false, 3: false, 5: false, 6: true} {
		if err := validateRating(rating); (err != nil) != wantErr {
			t.Errorf("rating %d: err = %v, wantErr %v", rating, err, wantErr)
		}
	}
}

func TestValidateDeckStatus(t *testing.T) {
	for status, wantErr := range map[string]bool{"draft": false, "published": false, "": true, "archived": true} {
		if err := validateDeckStatus(status); (err != nil) != wantErr {
			t.Errorf("status %q: err = %v, wantErr %v", status, err, wantErr)
		}
	}
}

func TestSessionLimit(t *testing.T) {
	tests := []struct {
		in, want int
		wantErr  bool
	}{
		{0, defaultSessionLimit, false},
		{5, 5, false},
		{maxSessionLimit, maxSessionLimit, false},
		{maxSessionLimit + 50, maxSessionLimit, false},
		{-3, 0, true},
	}
	for _, tt := range tests {
		got, err := sessionLimit(tt.in)
		if got != tt.want || (err != nil) != tt.wantErr {
			t.Errorf("sessionLimit(%d) = %d, %v; want %d, err %v", tt.in, got, err, tt.want, tt.wantErr)
		}
	}
}

func TestMarkLockedCards(t *testing.T) {
	cards := func() []models.Flashcard {
		return []models.Flashcard{
			{ID: "plus", Front: "secret q", Back: "secret a", Tiers: []string{"plus"}},
			{ID: "premium", Front: "secret q", Back: "secret a", Tiers: []string{"premium"}},
			{ID: "free", Front: "q", Back: "a", Tiers: []string{"free"}},
		}
	}

	tests := []struct {
		tier   models.Tier
		locked map[string]bool
	}{
		{models.TierFree, map[string]bool{"plus": true, "premium": true}},
		{models.TierPlus, map[string]bool{"premium": true}},
		{models.TierPremium, map[string]bool{"plus": true}},
		{models.TierUltra, map[string]bool{}},
	}
	for _, tc := range tests {
		got := cards()
		markLockedCards(got, tc.tier)
		for _, c := range got {
			locked := tc.locked[c.ID]
			if c.Locked != locked || (locked && (c.Front != "" || c.Back != "")) || (!locked && (c.Front == "" || c.Back == "")) {
				t.Errorf("%s %q: %+v", tc.tier, c.ID, c)
			}
		}
	}
}
