package gormstore

import (
	"context"
	"errors"

	"github.com/dujiao-next/internal/modules/backup/contract"
	"github.com/dujiao-next/internal/modules/backup/domain"
	"gorm.io/gorm"
)

type Store struct{ db *gorm.DB }

func New(db *gorm.DB) *Store { return &Store{db: db} }

func (s *Store) Create(ctx context.Context, record *domain.Record) error {
	return s.db.WithContext(ctx).Create(record).Error
}

func (s *Store) Save(ctx context.Context, record *domain.Record) error {
	// Updates must not recreate a concurrently deleted record as GORM Save can do.
	result := s.db.WithContext(ctx).Model(&domain.Record{}).Where("id = ?", record.ID).Select("*").Updates(record)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

func (s *Store) Get(ctx context.Context, id string) (*domain.Record, error) {
	var record domain.Record
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&record).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, domain.ErrNotFound
		}
		return nil, err
	}
	return &record, nil
}

func (s *Store) List(ctx context.Context, limit, offset int) ([]domain.Record, int64, error) {
	limit = min(max(limit, 1), 200)
	offset = max(offset, 0)
	var count int64
	if err := s.db.WithContext(ctx).Model(&domain.Record{}).Count(&count).Error; err != nil {
		return nil, 0, err
	}
	records := make([]domain.Record, 0)
	err := s.db.WithContext(ctx).Order("created_at DESC, id DESC").Limit(limit).Offset(offset).Find(&records).Error
	return records, count, err
}

func (s *Store) Running(ctx context.Context) ([]domain.Record, error) {
	records := make([]domain.Record, 0)
	err := s.db.WithContext(ctx).Where("status = ?", domain.StatusRunning).Order("created_at ASC, id ASC").Find(&records).Error
	return records, err
}

func (s *Store) Successful(ctx context.Context) ([]domain.Record, error) {
	records := make([]domain.Record, 0)
	err := s.db.WithContext(ctx).Where("status = ?", domain.StatusSucceeded).Order("created_at DESC, id DESC").Find(&records).Error
	return records, err
}

func (s *Store) Delete(ctx context.Context, id string) error {
	result := s.db.WithContext(ctx).Where("id = ?", id).Delete(&domain.Record{})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return domain.ErrNotFound
	}
	return nil
}

var _ contract.RecordStore = (*Store)(nil)
