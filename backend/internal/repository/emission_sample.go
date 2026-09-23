package repository

import (
	"context"
	"strings"

	"github.com/blueship581/carbon-capture-compliance-operations/backend/internal/dto"
	"github.com/blueship581/carbon-capture-compliance-operations/backend/internal/model"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// EmissionSampleRepository owns all persistence operations for 排放样本.
type EmissionSampleRepository interface {
	List(context.Context, dto.PageQuery) (Page[model.EmissionSample], error)
	Get(context.Context, uint) (model.EmissionSample, error)
	Create(context.Context, *model.EmissionSample) error
	Update(context.Context, uint, uint, *model.EmissionSample) error
	Revise(context.Context, uint, uint, *model.EmissionSample, *model.SampleRevision, *model.SampleRevision) error
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
	for index := range page.Items {
		page.Items[index].Revisions = markCurrentRevision(bySample[page.Items[index].ID])
	}
	return page, nil
}

func (r *emissionSampleRepository) Get(ctx context.Context, id uint) (model.EmissionSample, error) {
	var item model.EmissionSample
	err := r.db.WithContext(ctx).Preload("Revisions", func(db *gorm.DB) *gorm.DB {
		return db.Order("version ASC")
	}).First(&item, id).Error
	if err == nil {
		item.Revisions = markCurrentRevision(item.Revisions)
	}
	return item, err
}

func (r *emissionSampleRepository) Create(ctx context.Context, item *model.EmissionSample) error {
	return r.store.Create(ctx, item)
}

func (r *emissionSampleRepository) Update(ctx context.Context, id, version uint, item *model.EmissionSample) error {
	return r.store.Update(ctx, id, version, item)
}

// Revise applies a verified-sample amendment and its append-only revisions in
// one transaction. The aggregate row is locked first so two requests for the
// same sample code serialize: the loser sees a stale version and the whole
// request is rejected without a new revision row. When baseline is non-nil it
// preserves the pre-amendment values before the new revision is appended.
func (r *emissionSampleRepository) Revise(ctx context.Context, id, expectedVersion uint, item *model.EmissionSample, revision, baseline *model.SampleRevision) error {
	return mapConcurrentSubmitError(r.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var locked model.EmissionSample
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).First(&locked, id).Error; err != nil {
			return err
		}
		if locked.Version != expectedVersion {
			return ErrVersionConflict
		}
		if baseline != nil {
			var baselineCount int64
			if err := tx.Model(&model.SampleRevision{}).Where("emission_sample_id = ?", id).Count(&baselineCount).Error; err != nil {
				return err
			}
			if baselineCount == 0 {
				baseline.EmissionSampleID = id
				if err := tx.Create(baseline).Error; err != nil {
					return err
				}
			}
		}
		result := tx.Model(&model.EmissionSample{}).Where("id = ? AND version = ?", id, expectedVersion).
			Select("*").Omit("id", "code", "created_at", "deleted_at", "Revisions").Updates(item)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 0 {
			return ErrVersionConflict
		}
		revision.EmissionSampleID = id
		return tx.Create(revision).Error
	}))
}

// mapConcurrentSubmitError normalizes lock failures from concurrent writes to
// the same sample into ErrVersionConflict so the whole request is rejected and
// no revision is appended. PostgreSQL serializes the FOR UPDATE row lock; the
// SQLite development driver surfaces the same race as a locked/deadlocked table.
func mapConcurrentSubmitError(err error) error {
	if err == nil {
		return nil
	}
	message := strings.ToLower(err.Error())
	if strings.Contains(message, "database table is locked") || strings.Contains(message, "deadlock") ||
		strings.Contains(message, "database is locked") {
		return ErrVersionConflict
	}
	return err
}

func (r *emissionSampleRepository) Delete(ctx context.Context, id uint) error {
	return r.store.Delete(ctx, id)
}
func (r *emissionSampleRepository) CountByStatus(ctx context.Context) (map[string]int64, error) {
	return r.store.CountByStatus(ctx)
}

// markCurrentRevision flags the highest-version revision, which is the only
// revision allowed to serve as the compliance association basis.
func markCurrentRevision(revisions []model.SampleRevision) []model.SampleRevision {
	for index := range revisions {
		revisions[index].Current = index == len(revisions)-1
	}
	return revisions
}
