package service

import (
	"testing"

	"github.com/MohakGupta2004/vipin-lms-backend/internal/models"
)

func TestMarkLockedNotes(t *testing.T) {
	notes := func() []models.Note {
		return []models.Note{
			{ID: "plus+premium", Tiers: []string{"plus", "premium"}},
			{ID: "free note", Tiers: []string{"free"}},
			{ID: "premium only", Tiers: []string{"premium"}},
			{ID: "ultra only", Tiers: []string{}},
		}
	}

	tests := []struct {
		tier   models.Tier
		locked map[string]bool
	}{
		{models.TierFree, map[string]bool{"plus+premium": true, "free note": false, "premium only": true, "ultra only": true}},
		{models.TierPlus, map[string]bool{"plus+premium": false, "free note": false, "premium only": true, "ultra only": true}},
		{models.TierPremium, map[string]bool{"plus+premium": false, "free note": false, "premium only": false, "ultra only": true}},
		{models.TierUltra, map[string]bool{}},
	}
	for _, tc := range tests {
		got := notes()
		markLockedNotes(got, tc.tier)
		for _, n := range got {
			if n.Locked != tc.locked[n.ID] {
				t.Errorf("%s %q: locked = %v, want %v", tc.tier, n.ID, n.Locked, tc.locked[n.ID])
			}
		}
	}
}
