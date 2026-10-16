package service

import (
	"fmt"
	"slices"

	"github.com/MohakGupta2004/vipin-lms-backend/internal/models"
)

// Tags used when a request leaves tiers out. Ultra is always implied, so it never appears.
var (
	defaultContentTiers = []string{string(models.TierPlus), string(models.TierPremium)} // notes, quizzes, flashcards
	defaultOfflineTiers = []string{string(models.TierPremium)}                          // offline recordings
	defaultMeetTiers    = []string{}                                                    // meet recordings: ultra only
)

// normalizeTiers cleans the tiers a client sent: duplicates and ultra (always implied) are dropped,
// and unknown values are rejected. A nil input means "not sent" and gives def.
func normalizeTiers(in []string, def []string) ([]string, error) {
	if in == nil {
		return slices.Clone(def), nil
	}
	out := make([]string, 0, len(in))
	for _, t := range in {
		switch models.Tier(t) {
		case models.TierUltra:
			continue
		case models.TierFree, models.TierPlus, models.TierPremium:
			if !slices.Contains(out, t) {
				out = append(out, t)
			}
		default:
			return nil, fmt.Errorf("%w: tiers may only be free, plus or premium", ErrInvalidInput)
		}
	}
	return out, nil
}

// legacyFreeTiers maps the deprecated isFree flag onto tiers for clients that still send it:
// true adds free, false removes it. If that leaves nothing the default applies.
func legacyFreeTiers(current []string, isFree bool, def []string) []string {
	out := slices.DeleteFunc(slices.Clone(current), func(t string) bool { return t == string(models.TierFree) })
	if isFree {
		return append(out, string(models.TierFree))
	}
	if len(out) == 0 && slices.Contains(current, string(models.TierFree)) {
		return slices.Clone(def)
	}
	return out
}

// defaultVideoTiers is the access a new video gets when the request does not say.
func defaultVideoTiers(kind string) []string {
	if kind == models.VideoKindMeet {
		return defaultMeetTiers
	}
	return defaultOfflineTiers
}
