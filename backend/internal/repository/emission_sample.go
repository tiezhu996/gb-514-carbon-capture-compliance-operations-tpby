package repository

import (
	"context"
	"strings"

	"github.com/blueship581/carbon-capture-compliance-operations/backend/internal/dto"
	"github.com/blueship581/carbon-capture-compliance-operations/backend/internal/model"
	"gorm.io/gorm"
)

// EmissionSampleRepository owns all persistence operations for 排放样本.
type EmissionSampleRepository interface {
	List(context.Context, dto.PageQuery) (Page[model.EmissionSample], error)
	Get(context.Context, uint) (model.EmissionSample, error)
	FindByCode(context.Context, string) (model.EmissionSample, error)
	CreateWithRevision(context.Context, *model.EmissionSample, *model.SampleRevision) error
	UpdateWithRevision(context.Context, uint, uint, *model.EmissionSample, *model.SampleRevision) error
	AppendRejection(context.Context, *model.SampleRejection) error
	Delete(context.Context, uint) error
	CountByStatus(context.Context) (map[string]int64, error)
}

type emissionSampleRepository struct {
	store *Store[model.EmissionSample]
	db    *gorm.DB
}

func NewEmissionSampleRepository(db *gorm.DB) EmissionSampleRepository {
	return &emissionSampleRepository{store: NewStore[model.EmissionSample](db), db: db}
}

func (r *emissionSampleRepository) List(ctx context.Context, q dto.PageQuery) (Page[model.EmissionSample], error) {
	page, err := r.store.List(ctx, q)
	if err != nil || len(page.Items) == 0 {
		return page, err
	}
	ids := make([]uint, 0, len(page.Items))
	for _, item := range page.Items {
		ids = append(ids, item.ID)
	}
	var revisions []model.SampleRevision
	if err := r.db.WithContext(ctx).Where("emission_sample_id IN ?", ids).
		Order("version ASC").Find(&revisions).Error; err != nil {
		return Page[model.EmissionSample]{}, err
	}
	bySample := make(map[uint][]model.SampleRevision)
	for _, revision := range revisions {
		bySample[revision.EmissionSampleID] = append(bySample[revision.EmissionSampleID], revision)
	}
	var rejections []model.SampleRejection
	if err := r.db.WithContext(ctx).Where("emission_sample_id IN ?", ids).
		Order("id DESC").Find(&rejections).Error; err != nil {
		return Page[model.EmissionSample]{}, err
	}
	byRejection := make(map[uint][]model.SampleRejection)
	for _, rejection := range rejections {
		byRejection[rejection.EmissionSampleID] = append(byRejection[rejection.EmissionSampleID], rejection)
	}
	for index := range page.Items {
		page.Items[index].Revisions = bySample[page.Items[index].ID]
		page.Items[index].Rejections = byRejection[page.Items[index].ID]
	}
	return page, nil
}
func (r *emissionSampleRepository) Get(ctx context.Context, id uint) (model.EmissionSample, error) {
	var item model.EmissionSample
	err := r.db.WithContext(ctx).
		Preload("Revisions", func(db *gorm.DB) *gorm.DB { return db.Order("version ASC") }).
		Preload("Rejections", func(db *gorm.DB) *gorm.DB { return db.Order("id DESC") }).
		First(&item, id).Error
	return item, err
}
func (r *emissionSampleRepository) FindByCode(ctx context.Context, code string) (model.EmissionSample, error) {
	var item model.EmissionSample
	err := r.db.WithContext(ctx).Where("code = ?", strings.TrimSpace(code)).First(&item).Error
	return item, err
}
func (r *emissionSampleRepository) CreateWithRevision(ctx context.Context, item *model.EmissionSample, revision *model.SampleRevision) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Omit("Revisions", "Rejections").Create(item).Error; err != nil {
			if isDuplicateKey(err) {
				return ErrCodeConflict
			}
			return err
		}
		revision.EmissionSampleID = item.ID
		return tx.Create(revision).Error
	})
}
func (r *emissionSampleRepository) UpdateWithRevision(ctx context.Context, id, version uint, item *model.EmissionSample, revision *model.SampleRevision) error {
	return r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Model(&model.EmissionSample{}).Where("id = ? AND version = ?", id, version).
			Select("*").Omit("id", "code", "created_at", "deleted_at", "Revisions", "Rejections").Updates(item)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrVersionConflict
		}
		revision.EmissionSampleID = id
		return tx.Create(revision).Error
	})
}
func (r *emissionSampleRepository) AppendRejection(ctx context.Context, rejection *model.SampleRejection) error {
	return r.db.WithContext(ctx).Create(rejection).Error
}
func (r *emissionSampleRepository) Delete(ctx context.Context, id uint) error {
	return r.store.Delete(ctx, id)
}
func (r *emissionSampleRepository) CountByStatus(ctx context.Context) (map[string]int64, error) {
	return r.store.CountByStatus(ctx)
}
