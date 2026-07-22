package postgres

import (
	"errors"
	"testing"

	"github.com/GulovM/PharmacyCRM/backend/internal/modules/reliability/application/locking"
	"github.com/google/uuid"
)

func TestNormalizedIDsDeduplicatesAndSorts(t *testing.T) {
	first := uuid.MustParse("10000000-0000-0000-0000-000000000001")
	second := uuid.MustParse("20000000-0000-0000-0000-000000000002")
	ids, err := normalizedIDs([]uuid.UUID{second, first, second}, true)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 || ids[0] != first || ids[1] != second {
		t.Fatalf("unexpected normalized IDs: %v", ids)
	}
}

func TestCanonicalLockPlanRejectsUnsafeShapes(t *testing.T) {
	validID := uuid.New()
	for _, testCase := range []struct {
		name       string
		pharmacyID uuid.UUID
		products   []uuid.UUID
		lots       []uuid.UUID
		required   bool
	}{
		{name: "nil pharmacy", products: []uuid.UUID{validID}, required: true},
		{name: "missing products", pharmacyID: validID, required: true},
		{name: "nil product", pharmacyID: validID, products: []uuid.UUID{uuid.Nil}, required: true},
		{name: "lots without products", pharmacyID: validID, lots: []uuid.UUID{validID}},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			_, _, err := validateInventoryPlan(testCase.pharmacyID, testCase.products, testCase.lots, testCase.required)
			if !errors.Is(err, locking.ErrInvalidLockPlan) {
				t.Fatalf("unexpected error: %v", err)
			}
		})
	}
}
