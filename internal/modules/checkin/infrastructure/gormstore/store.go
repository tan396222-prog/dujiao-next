package gormstore

import (
	"errors"

	checkindomain "github.com/dujiao-next/internal/modules/checkin/domain"

	"gorm.io/gorm"
)

// Store 保存每日签到记录。
type Store struct {
	db *gorm.DB
}

func New(db *gorm.DB) *Store {
	return &Store{db: db}
}

func (s *Store) WithTx(tx *gorm.DB) *Store {
	if tx == nil {
		return s
	}
	return &Store{db: tx}
}

func (s *Store) Transaction(fn func(tx *gorm.DB) error) error {
	if s == nil || s.db == nil {
		return errors.New("check-in store unavailable")
	}
	return s.db.Transaction(fn)
}

func (s *Store) Find(userID uint, date string) (*checkindomain.CheckIn, error) {
	if s == nil || s.db == nil || userID == 0 || date == "" {
		return nil, nil
	}
	var row checkindomain.CheckIn
	err := s.db.Where("user_id = ? AND checkin_date = ? AND deleted_at IS NULL", userID, date).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &row, nil
}

func (s *Store) Insert(row *checkindomain.CheckIn) error {
	if s == nil || s.db == nil || row == nil {
		return errors.New("check-in insert unavailable")
	}
	return s.db.Create(row).Error
}
