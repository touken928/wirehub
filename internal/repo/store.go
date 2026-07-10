package repo

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sync"

	"github.com/touken928/wirehub/internal/config"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	gormsqlite "github.com/glebarez/sqlite"
)

type Store struct {
	db                 *gorm.DB
	dbPath             string
	lease              sync.RWMutex
	ipAllocationMu     sync.Mutex
	hookMu             sync.RWMutex
	openHook           func() error
	exportHook         func() error
	listMapDetailsHook func()
	snapshotHook       func()
}

func New(cfg *config.RuntimeConfig) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(cfg.DatabasePath), 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}

	s := &Store{dbPath: cfg.DatabasePath}
	if err := s.openDB(); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) openDB() error {
	db, err := gorm.Open(gormsqlite.Open(s.dbPath), &gorm.Config{
		Logger: logger.New(log.New(os.Stdout, "\r\n", log.LstdFlags), logger.Config{
			LogLevel:                  logger.Warn,
			IgnoreRecordNotFoundError: true,
		}),
	})
	if err != nil {
		return fmt.Errorf("open database: %w", err)
	}
	if err := migrateDB(db); err != nil {
		_ = closeGormDB(db)
		return err
	}
	if err := migrateGroupsDB(db); err != nil {
		_ = closeGormDB(db)
		return err
	}
	openHook := s.testErrorHook(func(s *Store) func() error { return s.openHook })
	if openHook != nil {
		if err := openHook(); err != nil {
			_ = closeGormDB(db)
			return err
		}
	}
	s.db = db
	return nil
}

func (s *Store) migrate() error {
	return migrateDB(s.db)
}

func migrateDB(db *gorm.DB) error {
	return db.AutoMigrate(&Admin{}, &Settings{}, &PeerGroup{}, &GroupLink{}, &Peer{}, &DNSRecord{}, &PortForward{}, &ServiceMap{}, &MapGroupAllow{})
}

func closeGormDB(db *gorm.DB) error {
	if db == nil {
		return nil
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	return sqlDB.Close()
}

// SetExportFailureForTest injects a deterministic snapshot failure for tests.
func (s *Store) SetExportFailureForTest(err error) {
	s.hookMu.Lock()
	defer s.hookMu.Unlock()
	s.exportHook = func() error { return err }
}

// SetSnapshotHookForTest installs a barrier immediately before VACUUM INTO.
func (s *Store) SetSnapshotHookForTest(hook func()) {
	s.hookMu.Lock()
	defer s.hookMu.Unlock()
	s.snapshotHook = hook
}

// SetListMapDetailsHookForTest installs a barrier after the composite read lease.
func (s *Store) SetListMapDetailsHookForTest(hook func()) {
	s.hookMu.Lock()
	defer s.hookMu.Unlock()
	s.listMapDetailsHook = hook
}

// SetOpenHookForTest injects a deterministic reopen failure for tests.
func (s *Store) SetOpenHookForTest(hook func() error) {
	s.hookMu.Lock()
	defer s.hookMu.Unlock()
	s.openHook = hook
}

func (s *Store) testHook(selectHook func(*Store) func()) func() {
	s.hookMu.RLock()
	defer s.hookMu.RUnlock()
	return selectHook(s)
}

func (s *Store) testErrorHook(selectHook func(*Store) func() error) func() error {
	s.hookMu.RLock()
	defer s.hookMu.RUnlock()
	return selectHook(s)
}

func (s *Store) GetSettings() (*Settings, error) {
	s.lease.RLock()
	defer s.lease.RUnlock()
	return getSettingsDB(s.db)
}

func getSettingsDB(db *gorm.DB) (*Settings, error) {
	var settings Settings
	if err := db.First(&settings).Error; err != nil {
		return nil, err
	}
	return &settings, nil
}

func (s *Store) UpdateSettings(settings *Settings) error {
	s.lease.RLock()
	defer s.lease.RUnlock()
	return s.db.Save(settings).Error
}

func (s *Store) GetAdminByUsername(username string) (*Admin, error) {
	s.lease.RLock()
	defer s.lease.RUnlock()
	var admin Admin
	if err := s.db.Where("username = ?", username).First(&admin).Error; err != nil {
		return nil, err
	}
	return &admin, nil
}

func (s *Store) GetAdminByID(id uint) (*Admin, error) {
	s.lease.RLock()
	defer s.lease.RUnlock()
	var admin Admin
	if err := s.db.First(&admin, id).Error; err != nil {
		return nil, err
	}
	return &admin, nil
}
