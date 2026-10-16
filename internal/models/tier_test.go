package models

import "testing"

func TestCanAccess(t *testing.T) {
	tests := []struct {
		name string
		tier Tier
		tags []string
		want bool
	}{
		{"ultra opens an empty set", TierUltra, nil, true},
		{"ultra opens plus only", TierUltra, []string{"plus"}, true},
		{"free tier opens free item", TierFree, []string{"free"}, true},
		{"free tier shut out of plus", TierFree, []string{"plus", "premium"}, false},
		{"free tier shut out of ultra only", TierFree, []string{}, false},
		{"plus opens free item", TierPlus, []string{"free"}, true},
		{"plus opens plus item", TierPlus, []string{"plus"}, true},
		{"plus shut out of premium only", TierPlus, []string{"premium"}, false},
		{"premium opens plus+premium", TierPremium, []string{"plus", "premium"}, true},
		{"premium shut out of plus only", TierPremium, []string{"plus"}, false},
		{"premium shut out of ultra only", TierPremium, nil, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := CanAccess(tc.tier, tc.tags); got != tc.want {
				t.Errorf("CanAccess(%q, %v) = %v, want %v", tc.tier, tc.tags, got, tc.want)
			}
		})
	}
}

func TestTierSQL(t *testing.T) {
	got := TierSQL("f.tiers", "$3")
	want := "($3 = 'ultra' OR 'free' = ANY(f.tiers) OR $3 = ANY(f.tiers))"
	if got != want {
		t.Errorf("TierSQL = %q, want %q", got, want)
	}
}
