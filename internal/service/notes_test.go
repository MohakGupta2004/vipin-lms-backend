package service

import (
	"testing"

	"github.com/MohakGupta2004/vipin-lms-backend/internal/models"
)

func TestMarkLockedNotes(t *testing.T) {
	notes := func() []models.Note {
		return []models.Note{
			{ID: "paid"},
			{ID: "free note", IsFree: true},
		}
	}

	previewed := notes()
	markLockedNotes(previewed, true)
	for _, n := range previewed {
		if want := n.ID == "paid"; n.Locked != want {
			t.Errorf("preview %q: locked = %v, want %v", n.ID, n.Locked, want)
		}
	}

	full := notes()
	markLockedNotes(full, false)
	for _, n := range full {
		if n.Locked {
			t.Errorf("full access %q: must not be locked", n.ID)
		}
	}
}
