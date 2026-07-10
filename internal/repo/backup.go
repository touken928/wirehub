package repo

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	gormsqlite "github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

const sqliteHeader = "SQLite format 3\x00"

var wireHubTables = []string{
	"admins", "settings", "peer_groups", "group_links", "peers", "dns_records",
	"port_forwards", "service_relays", "relay_group_allows",
}

// ValidateWireHubDatabase checks that path is a configured WireHub SQLite database.
func ValidateWireHubDatabase(path string) error {
	st, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("database file: %w", err)
	}
	if st.Size() < 512 {
		return fmt.Errorf("database file is too small")
	}
	if st.Size() > 128<<20 {
		return fmt.Errorf("database file exceeds 128MB limit")
	}

	head := make([]byte, len(sqliteHeader))
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	if _, err := io.ReadFull(f, head); err != nil {
		_ = f.Close()
		return fmt.Errorf("read database header: %w", err)
	}
	_ = f.Close()
	if string(head) != sqliteHeader {
		return fmt.Errorf("not a SQLite database file")
	}

	db, err := sql.Open("sqlite", path)
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	defer db.Close()
	var quick string
	if err := db.QueryRow(`PRAGMA quick_check`).Scan(&quick); err != nil || quick != "ok" {
		return fmt.Errorf("SQLite quick_check failed: %s", quick)
	}

	for _, table := range wireHubTables {
		var name string
		err := db.QueryRow(
			`SELECT name FROM sqlite_master WHERE type='table' AND name=?`,
			table,
		).Scan(&name)
		if err != nil {
			return fmt.Errorf("missing table %q", table)
		}
	}

	var adminCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM admins`).Scan(&adminCount); err != nil || adminCount == 0 {
		return fmt.Errorf("database has no admin account")
	}

	var settingsCount int
	if err := db.QueryRow(`SELECT COUNT(*) FROM settings`).Scan(&settingsCount); err != nil || settingsCount == 0 {
		return fmt.Errorf("database has no hub settings")
	}

	var endpoint string
	if err := db.QueryRow(`SELECT endpoint FROM settings LIMIT 1`).Scan(&endpoint); err != nil {
		return fmt.Errorf("read settings: %w", err)
	}
	if strings.TrimSpace(endpoint) == "" {
		return fmt.Errorf("hub endpoint is not configured in database")
	}

	return nil
}

func (s *Store) DatabasePath() string {
	s.lease.RLock()
	defer s.lease.RUnlock()
	return s.dbPath
}

func (s *Store) closeDB() error {
	return closeGormDB(s.db)
}

// ExportDatabase writes a consistent snapshot of wirehub.db.
func (s *Store) ExportDatabase(w io.Writer) error {
	s.lease.RLock()
	defer s.lease.RUnlock()
	if hook := s.testErrorHook(func(s *Store) func() error { return s.exportHook }); hook != nil {
		if err := hook(); err != nil {
			return err
		}
	}
	tmp, err := os.CreateTemp(filepath.Dir(s.dbPath), ".wirehub-export-*.db")
	if err != nil {
		return fmt.Errorf("create snapshot: %w", err)
	}
	tmpPath := tmp.Name()
	_ = tmp.Close()
	defer os.Remove(tmpPath)
	if hook := s.testHook(func(s *Store) func() { return s.snapshotHook }); hook != nil {
		hook()
	}
	if err := s.db.Exec("VACUUM INTO ?", tmpPath).Error; err != nil {
		return fmt.Errorf("create SQLite snapshot: %w", err)
	}
	f, err := os.Open(tmpPath)
	if err != nil {
		return fmt.Errorf("open snapshot: %w", err)
	}
	defer f.Close()
	if _, err := io.Copy(w, f); err != nil {
		return fmt.Errorf("copy database: %w", err)
	}
	return nil
}

// ImportDatabase replaces the live database with a validated backup file.
func (s *Store) ImportDatabase(srcPath string) error {
	stagedPath, cleanup, err := prepareImportDatabase(srcPath, s.dbPath)
	if err != nil {
		return err
	}
	defer cleanup()
	s.lease.Lock()
	defer s.lease.Unlock()
	return s.swapDatabaseLocked(stagedPath)
}

func prepareImportDatabase(srcPath, livePath string) (string, func(), error) {
	dir := filepath.Dir(livePath)
	sourceCopy, err := os.CreateTemp(dir, ".wirehub-import-source-*.db")
	if err != nil {
		return "", func() {}, err
	}
	sourcePath := sourceCopy.Name()
	cleanup := func() {
		_ = os.Remove(sourcePath)
		_ = os.Remove(sourcePath + "-wal")
		_ = os.Remove(sourcePath + "-shm")
	}
	if err := copyFile(srcPath, sourcePath); err != nil {
		cleanup()
		_ = sourceCopy.Close()
		return "", func() {}, err
	}
	if err := sourceCopy.Close(); err != nil {
		cleanup()
		return "", func() {}, err
	}
	for _, suffix := range []string{"-wal", "-shm"} {
		if err := copyOptionalFile(srcPath+suffix, sourcePath+suffix); err != nil {
			cleanup()
			return "", func() {}, err
		}
	}
	finalFile, err := os.CreateTemp(dir, ".wirehub-import-final-*.db")
	if err != nil {
		cleanup()
		return "", func() {}, err
	}
	finalPath := finalFile.Name()
	_ = finalFile.Close()
	_ = os.Remove(finalPath)
	cleanupAll := func() {
		cleanup()
		_ = os.Remove(finalPath)
		_ = os.Remove(finalPath + "-wal")
		_ = os.Remove(finalPath + "-shm")
	}

	db, err := gorm.Open(gormsqlite.Open(sourcePath), &gorm.Config{})
	if err != nil {
		cleanupAll()
		return "", func() {}, fmt.Errorf("open database: %w", err)
	}
	if err := migrateDB(db); err != nil {
		_ = closeGormDB(db)
		cleanupAll()
		return "", func() {}, fmt.Errorf("migrate database: %w", err)
	}
	if err := migrateGroupsDB(db); err != nil {
		_ = closeGormDB(db)
		cleanupAll()
		return "", func() {}, fmt.Errorf("migrate groups: %w", err)
	}
	if err := db.Exec("VACUUM INTO ?", finalPath).Error; err != nil {
		_ = closeGormDB(db)
		cleanupAll()
		return "", func() {}, fmt.Errorf("snapshot import: %w", err)
	}
	if err := closeGormDB(db); err != nil {
		cleanupAll()
		return "", func() {}, err
	}
	if err := ValidateWireHubDatabase(finalPath); err != nil {
		cleanupAll()
		return "", func() {}, err
	}
	return finalPath, cleanupAll, nil
}

func (s *Store) swapDatabaseLocked(stagedPath string) error {
	backupBase := s.dbPath + ".import-backup"
	if err := removeDatabaseFiles(backupBase); err != nil {
		return fmt.Errorf("clear import backup: %w", err)
	}
	if err := s.closeDB(); err != nil {
		reopenErr := s.openDB()
		return errors.Join(fmt.Errorf("close live database: %w", err), reopenErr)
	}
	moved := false
	for _, suffix := range databaseSidecars {
		if err := moveOptional(s.dbPath+suffix, backupBase+suffix); err != nil {
			return errors.Join(fmt.Errorf("backup live database: %w", err), s.restoreDatabaseLocked(backupBase, moved))
		}
		if suffix == "" {
			moved = true
		}
	}
	if err := os.Rename(stagedPath, s.dbPath); err != nil {
		return errors.Join(fmt.Errorf("install imported database: %w", err), s.restoreDatabaseLocked(backupBase, moved))
	}
	if err := s.openDB(); err != nil {
		return errors.Join(fmt.Errorf("open imported database: %w", err), s.restoreDatabaseLocked(backupBase, true))
	}
	configured, err := isConfiguredDB(s.db)
	if err != nil || !configured {
		if err == nil {
			err = fmt.Errorf("imported database is not a configured WireHub hub")
		}
		return errors.Join(err, s.restoreDatabaseLocked(backupBase, true))
	}
	if err := removeDatabaseFiles(backupBase); err != nil {
		return fmt.Errorf("remove import backup: %w", err)
	}
	return nil
}

var databaseSidecars = []string{"", "-wal", "-shm"}

func (s *Store) restoreDatabaseLocked(backupBase string, moved bool) error {
	var errs []error
	if s.db != nil {
		if err := closeGormDB(s.db); err != nil {
			errs = append(errs, err)
		}
	}
	if moved {
		if err := removeDatabaseFiles(s.dbPath); err != nil {
			errs = append(errs, err)
		}
		for _, suffix := range databaseSidecars {
			if err := moveOptional(backupBase+suffix, s.dbPath+suffix); err != nil {
				errs = append(errs, err)
			}
		}
	}
	if err := s.openDB(); err != nil {
		errs = append(errs, fmt.Errorf("reopen restored database: %w", err))
	}
	return errors.Join(errs...)
}

func moveOptional(src, dst string) error {
	if _, err := os.Stat(src); errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	_ = os.Remove(dst)
	return os.Rename(src, dst)
}

func removeDatabaseFiles(base string) error {
	var errs []error
	for _, suffix := range databaseSidecars {
		if err := os.Remove(base + suffix); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	syncErr := out.Sync()
	closeErr := out.Close()
	return errors.Join(copyErr, syncErr, closeErr)
}

func copyOptionalFile(src, dst string) error {
	if _, err := os.Stat(src); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return copyFile(src, dst)
}
