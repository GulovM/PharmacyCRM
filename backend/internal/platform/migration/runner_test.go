package migration

import (
	"testing"
	"testing/fstest"
)

func TestLoadOrdersAndChecksumsMigrations(t *testing.T) {
	files := fstest.MapFS{
		"000002_second.up.sql": {Data: []byte("-- Verification query: SELECT true;\nSELECT 2;")},
		"000001_first.up.sql":  {Data: []byte("-- Verification query: SELECT true;\nSELECT 1;")},
	}
	migrations, err := Load(files)
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) != 2 || migrations[0].Version != 1 || migrations[1].Version != 2 || migrations[0].Checksum == "" || migrations[0].VerificationSQL != "SELECT true;" {
		t.Fatalf("invalid migrations: %#v", migrations)
	}
}
func TestLoadRejectsInvalidName(t *testing.T) {
	_, err := Load(fstest.MapFS{"bad.up.sql": {Data: []byte("-- Verification query: SELECT true;\nSELECT 1;")}})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestLoadRejectsMissingVerificationQuery(t *testing.T) {
	_, err := Load(fstest.MapFS{"000002_second.up.sql": {Data: []byte("SELECT 1;")}})
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestLoadPreservesLegacySchemaMetadataMigration(t *testing.T) {
	migrations, err := Load(fstest.MapFS{
		"000001_schema_metadata.up.sql": {Data: []byte("CREATE TABLE pharmacycrm_schema_metadata (singleton boolean);")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) != 1 || migrations[0].VerificationSQL != legacySchemaMetadataVerification {
		t.Fatalf("unexpected legacy migration: %#v", migrations)
	}
}

func TestLoadAcceptsKnownEarlierSupersededVerification(t *testing.T) {
	migrations, err := Load(fstest.MapFS{
		"000001_first.up.sql":  {Data: []byte("-- Verification query: SELECT true;\nSELECT 1;")},
		"000002_second.up.sql": {Data: []byte("-- Supersedes verification: 1\n-- Verification query: SELECT true;\nSELECT 2;")},
	})
	if err != nil || len(migrations) != 2 {
		t.Fatalf("migrations=%#v err=%v", migrations, err)
	}
}

func TestLoadRejectsInvalidSupersededVerification(t *testing.T) {
	for name, declaration := range map[string]string{
		"unknown":   "1",
		"self":      "2",
		"future":    "3",
		"duplicate": "1,1",
		"malformed": "x",
	} {
		t.Run(name, func(t *testing.T) {
			files := fstest.MapFS{
				"000002_second.up.sql": {Data: []byte("-- Supersedes verification: " + declaration + "\n-- Verification query: SELECT true;\nSELECT 2;")},
			}
			if _, err := Load(files); err == nil {
				t.Fatal("expected invalid superseded verification error")
			}
		})
	}
}
