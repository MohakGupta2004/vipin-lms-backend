package models

import (
	"fmt"
	"slices"
	"strings"
)

// Tier is how much of a course a user may open. A user has one tier per course: the owner and
// anyone in a free course get ultra, an enrolled student gets the tier on their enrollment, and
// everyone else previewing a published course is free.
type Tier string

const (
	TierFree    Tier = "free"
	TierPlus    Tier = "plus"
	TierPremium Tier = "premium"
	TierUltra   Tier = "ultra"
)

// IsEnrollmentTier reports whether t can be given to an enrollment.
func (t Tier) IsEnrollmentTier() bool {
	return t == TierPlus || t == TierPremium || t == TierUltra
}

// CanAccess reports whether a user on tier t may open an item tagged with tags. Ultra opens
// everything; a tag of free opens it for everyone; otherwise the user's own tier must be tagged.
func CanAccess(t Tier, tags []string) bool {
	if t == TierUltra {
		return true
	}
	return slices.Contains(tags, string(TierFree)) || slices.Contains(tags, string(t))
}

// TierSQL returns an SQL condition that holds for rows of col (a text[] of tiers) the tier in
// parameter number param may open. It mirrors CanAccess.
func TierSQL(col, param string) string {
	return "(" + param + " = 'ultra' OR 'free' = ANY(" + col + ") OR " + param + " = ANY(" + col + "))"
}

// tierScanner reads a Postgres text[] of tiers. The tier names are plain words, so the text form
// {free,plus} needs no unquoting.
type tierScanner struct{ dst *[]string }

// scanTiers returns a Scan destination that fills dst with the tiers of a text[] column.
func scanTiers(dst *[]string) *tierScanner { return &tierScanner{dst: dst} }

func (s *tierScanner) Scan(src any) error {
	var text string
	switch v := src.(type) {
	case nil:
	case string:
		text = v
	case []byte:
		text = string(v)
	default:
		return fmt.Errorf("cannot scan %T into tiers", src)
	}
	text = strings.Trim(text, "{}")
	tiers := []string{}
	if text != "" {
		tiers = strings.Split(text, ",")
	}
	*s.dst = tiers
	return nil
}

// tiersParam is the argument for a $n::text[] parameter. A nil list becomes an empty array, never NULL.
func tiersParam(tiers []string) []string {
	if tiers == nil {
		return []string{}
	}
	return tiers
}

// userTierSQL is an SQL expression for the tier user $1 has in the course aliased c: ultra for its
// owner, the tier of a valid enrollment, ultra in a free course they may preview, otherwise free. It
// mirrors the service's authorizeTier.
const userTierSQL = `COALESCE(
		CASE WHEN c.instructor_id = $1 THEN 'ultra' END,
		(SELECT e.tier FROM enrollments e
			WHERE e.course_id = c.id AND e.user_id = $1 AND c.status = 'published'
			  AND e.status IN ('active', 'completed')
			  AND (e.expires_at IS NULL OR e.expires_at > now())),
		CASE WHEN c.is_free AND ` + coursePreviewableSQL + ` THEN 'ultra' END,
		'free')`
