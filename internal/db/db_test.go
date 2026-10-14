package db

import (
	"os"
	"path/filepath"
	"testing"
)

// SEC-18: the database and its WAL files are readable by the server's user
// only, both when created and when an existing file was 0644.
func TestOpen_RestrictsFilePermissions(t *testing.T) {
	for name, existing := range map[string]bool{"new database": false, "existing 0644 database": true} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "test.db")
			if existing {
				if err := os.WriteFile(path, nil, 0o644); err != nil {
					t.Fatal(err)
				}
				if err := os.Chmod(path, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			conn, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			// Migrations have written, so the WAL files exist.
			for _, p := range []string{path, path + "-wal", path + "-shm"} {
				fi, err := os.Stat(p)
				if err != nil {
					t.Fatalf("stat %s: %v", filepath.Base(p), err)
				}
				if mode := fi.Mode().Perm(); mode != 0o600 {
					t.Errorf("%s mode = %o, want 600", filepath.Base(p), mode)
				}
			}
		})
	}
}
