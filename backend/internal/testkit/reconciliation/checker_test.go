package reconciliation

import (
	"errors"
	"testing"

	"github.com/google/uuid"
)

func TestNormalizeRequiredIDs(t *testing.T) {
	id := uuid.New()
	ids, err := normalizeRequiredIDs([]uuid.UUID{id, id})
	if err != nil || len(ids) != 1 || ids[0] != id {
		t.Fatalf("ids=%v err=%v", ids, err)
	}
	for _, input := range [][]uuid.UUID{nil, {uuid.Nil}} {
		if _, err := normalizeRequiredIDs(input); !errors.Is(err, ErrInvalidScope) {
			t.Fatalf("input=%v err=%v", input, err)
		}
	}
}

func TestEmptyReportIsClean(t *testing.T) {
	if !(Report{}).Clean() {
		t.Fatal("empty report must be clean")
	}
	if (Report{Violations: []Violation{{Kind: BalanceMismatch}}}).Clean() {
		t.Fatal("report with a violation must not be clean")
	}
}
