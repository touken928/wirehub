package repo

import (
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"
)

func (s *Store) ListGroups() ([]PeerGroup, error) {
	s.lease.RLock()
	defer s.lease.RUnlock()
	var groups []PeerGroup
	err := s.db.Order("name asc").Find(&groups).Error
	return groups, err
}

func (s *Store) GetGroup(id uint) (*PeerGroup, error) {
	s.lease.RLock()
	defer s.lease.RUnlock()
	return getGroupDB(s.db, id)
}

func getGroupDB(db *gorm.DB, id uint) (*PeerGroup, error) {
	var g PeerGroup
	if err := db.First(&g, id).Error; err != nil {
		return nil, err
	}
	return &g, nil
}

func (s *Store) CreateGroup(name string, posX, posY float64) (*PeerGroup, error) {
	s.lease.RLock()
	defer s.lease.RUnlock()
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("group name is required")
	}
	g := &PeerGroup{Name: name, PosX: posX, PosY: posY}
	if err := s.db.Create(g).Error; err != nil {
		return nil, err
	}
	return g, nil
}

func (s *Store) UpdateGroup(g *PeerGroup) error {
	s.lease.RLock()
	defer s.lease.RUnlock()
	return s.db.Save(g).Error
}

func (s *Store) RenameGroup(id uint, name string) (*PeerGroup, error) {
	s.lease.RLock()
	defer s.lease.RUnlock()
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("group name is required")
	}
	g, err := getGroupDB(s.db, id)
	if err != nil {
		return nil, err
	}
	if g.Name == name {
		return g, nil
	}
	var count int64
	if err := s.db.Model(&PeerGroup{}).Where("name = ? AND id != ?", name, id).Count(&count).Error; err != nil {
		return nil, err
	}
	if count > 0 {
		return nil, fmt.Errorf("group name already exists")
	}
	g.Name = name
	if err := s.db.Save(g).Error; err != nil {
		return nil, err
	}
	return g, nil
}

func (s *Store) DeleteGroup(id uint) error {
	s.lease.RLock()
	defer s.lease.RUnlock()
	var count int64
	if err := s.db.Model(&Peer{}).Where("group_id = ?", id).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return fmt.Errorf("group still has peers")
	}
	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("from_group_id = ? OR to_group_id = ?", id, id).Delete(&GroupLink{}).Error; err != nil {
			return err
		}
		if err := tx.Where("group_id = ?", id).Delete(&MapGroupAllow{}).Error; err != nil {
			return err
		}
		return tx.Delete(&PeerGroup{}, id).Error
	})
}

func (s *Store) ListGroupLinks() ([]GroupLink, error) {
	s.lease.RLock()
	defer s.lease.RUnlock()
	var links []GroupLink
	err := s.db.Find(&links).Error
	return links, err
}

func normalizeLinkPair(fromID, toID uint, bidirectional bool) (uint, uint) {
	if bidirectional && fromID > toID {
		return toID, fromID
	}
	return fromID, toID
}

func (s *Store) deleteLinksBetween(tx *gorm.DB, a, b uint) error {
	return tx.Where(
		"(from_group_id = ? AND to_group_id = ?) OR (from_group_id = ? AND to_group_id = ?)",
		a, b, b, a,
	).Delete(&GroupLink{}).Error
}

func (s *Store) UpsertGroupLink(fromID, toID uint, bidirectional bool) error {
	s.lease.RLock()
	defer s.lease.RUnlock()
	if fromID == toID {
		return fmt.Errorf("cannot link a group to itself")
	}
	fromID, toID = normalizeLinkPair(fromID, toID, bidirectional)

	return s.db.Transaction(func(tx *gorm.DB) error {
		if err := s.deleteLinksBetween(tx, fromID, toID); err != nil {
			return err
		}
		return tx.Model(&GroupLink{}).Create(map[string]any{
			"from_group_id": fromID,
			"to_group_id":   toID,
			"bidirectional": bidirectional,
		}).Error
	})
}

// FindGroupLink returns the single link between two groups, if any.
func (s *Store) FindGroupLink(a, b uint) (*GroupLink, error) {
	s.lease.RLock()
	defer s.lease.RUnlock()
	var link GroupLink
	err := s.db.Where(
		"(from_group_id = ? AND to_group_id = ?) OR (from_group_id = ? AND to_group_id = ?)",
		a, b, b, a,
	).First(&link).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &link, nil
}

func (s *Store) HasGroupLink(fromID, toID uint) (bool, error) {
	s.lease.RLock()
	defer s.lease.RUnlock()
	var count int64
	err := s.db.Model(&GroupLink{}).Where(
		"(from_group_id = ? AND to_group_id = ?) OR (from_group_id = ? AND to_group_id = ?)",
		fromID, toID, toID, fromID,
	).Count(&count).Error
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (s *Store) DeleteGroupLink(fromID, toID uint) error {
	s.lease.RLock()
	defer s.lease.RUnlock()
	return s.db.Where(
		"(from_group_id = ? AND to_group_id = ?) OR (from_group_id = ? AND to_group_id = ?)",
		fromID, toID, toID, fromID,
	).Delete(&GroupLink{}).Error
}

func (s *Store) CountPeersByGroup() (map[uint]int64, error) {
	s.lease.RLock()
	defer s.lease.RUnlock()
	type row struct {
		GroupID uint
		Count   int64
	}
	var rows []row
	if err := s.db.Model(&Peer{}).Select("group_id, count(*) as count").Group("group_id").Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make(map[uint]int64, len(rows))
	for _, r := range rows {
		out[r.GroupID] = r.Count
	}
	return out, nil
}

func (s *Store) MigrateGroups() error {
	s.lease.RLock()
	defer s.lease.RUnlock()
	return migrateGroupsDB(s.db)
}

func migrateGroupsDB(db *gorm.DB) error {
	if err := db.AutoMigrate(&PeerGroup{}, &GroupLink{}); err != nil {
		return err
	}
	if db.Migrator().HasColumn("peer_groups", "passive") {
		if err := db.Exec("ALTER TABLE peer_groups DROP COLUMN passive").Error; err != nil {
			return err
		}
	}
	if db.Migrator().HasTable("port_forward_dmzs") {
		if err := db.Migrator().DropTable("port_forward_dmzs"); err != nil {
			return err
		}
	}
	if !db.Migrator().HasColumn(&Peer{}, "group_id") {
		if err := db.Migrator().AddColumn(&Peer{}, "GroupID"); err != nil {
			return err
		}
	}

	var unassigned int64
	if err := db.Model(&Peer{}).Where("group_id IS NULL OR group_id = 0").Count(&unassigned).Error; err != nil {
		return err
	}
	if unassigned == 0 {
		return nil
	}

	var defaultGroup PeerGroup
	err := db.Where("name = ?", "default").First(&defaultGroup).Error
	if err != nil {
		defaultGroup = PeerGroup{Name: "default", PosX: 0, PosY: 0}
		if createErr := db.Create(&defaultGroup).Error; createErr != nil {
			return createErr
		}
	}
	return db.Model(&Peer{}).Where("group_id IS NULL OR group_id = 0").Update("group_id", defaultGroup.ID).Error
}
