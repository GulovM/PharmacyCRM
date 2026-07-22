package locking

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
)

var (
	ErrInvalidLockPlan   = errors.New("invalid canonical lock plan")
	ErrLockTargetMissing = errors.New("canonical lock target is missing")
)

// InventoryPlan is the narrow lock plan used by sale, receipt, write-off,
// adjustment, and reversal inventory mutations. Products must be locked before
// their lots; callers cannot supply table names or arbitrary ordering.
type InventoryPlan struct {
	PharmacyID         uuid.UUID
	PharmacyProductIDs []uuid.UUID
	StockLotIDs        []uuid.UUID
}

// SaleReturnPlan follows the published business order: pharmacy, root sale,
// pharmacy products, source allocations, and finally stock lots.
type SaleReturnPlan struct {
	PharmacyID          uuid.UUID
	SaleID              uuid.UUID
	PharmacyProductIDs  []uuid.UUID
	SourceAllocationIDs []uuid.UUID
	StockLotIDs         []uuid.UUID
}

type Pharmacy struct {
	ID      uuid.UUID
	Status  string
	Version int64
}

type Sale struct {
	ID         uuid.UUID
	PharmacyID uuid.UUID
	Status     string
}

type PharmacyProduct struct {
	ID                          uuid.UUID
	PharmacyID                  uuid.UUID
	ProductPresentationID       uuid.UUID
	InnerUnitSaleAllowed        bool
	DefaultPackagePriceDirams   int64
	DefaultInnerUnitPriceDirams *int64
	Status                      string
	Version                     int64
}

type SourceAllocation struct {
	ID                uuid.UUID
	SaleItemID        uuid.UUID
	StockLotID        uuid.UUID
	QuantityBaseUnits int64
}

type StockLot struct {
	ID                         uuid.UUID
	PharmacyProductID          uuid.UUID
	ExpirationDate             time.Time
	ReceivedAt                 time.Time
	QuantityBaseUnits          int64
	BaseUnitsPerPackage        int64
	PackageRetailPriceDirams   int64
	InnerUnitRetailPriceDirams *int64
	Status                     string
	Version                    int64
}

type InventoryLocks struct {
	Pharmacy         Pharmacy
	PharmacyProducts []PharmacyProduct
	StockLots        []StockLot
}

type SaleReturnLocks struct {
	Pharmacy          Pharmacy
	Sale              Sale
	PharmacyProducts  []PharmacyProduct
	SourceAllocations []SourceAllocation
	StockLots         []StockLot
}

type Repository interface {
	LockInventory(context.Context, InventoryPlan) (InventoryLocks, error)
	LockSaleReturn(context.Context, SaleReturnPlan) (SaleReturnLocks, error)
}
