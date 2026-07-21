package migration

import (
	"testing"
	"testing/fstest"
)

func TestLoadOrdersAndChecksumsMigrations(t *testing.T) {
	files := fstest.MapFS{"000002_second.up.sql": {Data: []byte("SELECT 2;")}, "000001_first.up.sql": {Data: []byte("SELECT 1;")}}
	migrations, err := Load(files)
	if err != nil {
		t.Fatal(err)
	}
	if len(migrations) != 2 || migrations[0].Version != 1 || migrations[1].Version != 2 || migrations[0].Checksum == "" {
		t.Fatalf("invalid migrations: %#v", migrations)
	}
}
func TestLoadRejectsInvalidName(t *testing.T) {
	_, err := Load(fstest.MapFS{"bad.up.sql": {Data: []byte("SELECT 1;")}})
	if err == nil {
		t.Fatal("expected error")
	}
}
