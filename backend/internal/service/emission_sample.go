package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/blueship581/carbon-capture-compliance-operations/backend/internal/constants"
	"github.com/blueship581/carbon-capture-compliance-operations/backend/internal/dto"
	"github.com/blueship581/carbon-capture-compliance-operations/backend/internal/model"
	"github.com/blueship581/carbon-capture-compliance-operations/backend/internal/repository"
)

type EmissionSampleService interface {
	List(context.Context, dto.PageQuery) (repository.Page[model.EmissionSample], error)
	Get(context.Context, uint) (model.EmissionSample, error)
	Create(context.Context, dto.CreateEmissionSample, string, string) (model.EmissionSample, error)
	Update(context.Context, uint, dto.UpdateEmissionSample, string, string) (model.EmissionSample, error)
	Transition(context.Context, uint, dto.TransitionRequest, string, string) (model.EmissionSample, error)
	Delete(context.Context, uint, string, string) error
	StatusCounts(context.Context) (map[string]int64, error)
}

type emissionSampleService struct {
	repository repository.EmissionSampleRepository
	security   SecurityService
}

func NewEmissionSampleService(repo repository.EmissionSampleRepository, security SecurityService) EmissionSampleService {
	return &emissionSampleService{repository: repo, security: security}
}

func (s *emissionSampleService) List(ctx context.Context, query dto.PageQuery) (repository.Page[model.EmissionSample], error) {
	return s.repository.List(ctx, query)
}

func (s *emissionSampleService) Get(ctx context.Context, id uint) (model.EmissionSample, error) {
	return s.repository.Get(ctx, id)
}

func (s *emissionSampleService) Create(ctx context.Context, input dto.CreateEmissionSample, actor, requestID string) (model.EmissionSample, error) {
	if err := validateEmissionSampleBusinessFields(input.Code, input.Name, input.Facility, input.Owner); err != nil {
		return model.EmissionSample{}, err
	}
	item := model.EmissionSample{
		BaseModel: model.BaseModel{
			Code: strings.ToUpper(strings.TrimSpace(input.Code)), Name: strings.TrimSpace(input.Name),
			Status: model.EmissionSampleInitialStatus, Version: 1, Description: strings.TrimSpace(input.Description),
		},
		Facility: strings.TrimSpace(input.Facility), Owner: strings.TrimSpace(input.Owner),
		Category: strings.TrimSpace(input.Category), RiskLevel: input.RiskLevel,
		MetricValue: input.MetricValue, MetricUnit: strings.TrimSpace(input.MetricUnit),
		EffectiveAt: input.EffectiveAt.UTC(), Evidence: strings.TrimSpace(input.Evidence),
		RelatedCode: strings.ToUpper(strings.TrimSpace(input.RelatedCode)),
	}
	revision := newSampleRevision(&item, "created emission sample", actor, requestID)
	if err := s.repository.CreateWithRevision(ctx, &item, revision); err != nil {
		if errors.Is(err, repository.ErrCodeConflict) {
			// Reject the whole duplicate-code submission: no sample row and no
			// revision are written, only the refusal is recorded.
			s.recordRejection(ctx, item.Code, ErrDuplicateCode.Error(), actor, requestID)
			return model.EmissionSample{}, ErrDuplicateCode
		}
		return model.EmissionSample{}, fmt.Errorf("create 排放样本: %w", err)
	}
	_ = s.security.Audit(ctx, actor, requestID, "create", "EmissionSample", item.ID, "", item.Status, "created 排放样本")
	return s.repository.Get(ctx, item.ID)
}

func (s *emissionSampleService) Update(ctx context.Context, id uint, input dto.UpdateEmissionSample, actor, requestID string) (model.EmissionSample, error) {
	current, err := s.repository.Get(ctx, id)
	if err != nil {
		return model.EmissionSample{}, err
	}
	if err := validateEmissionSampleBusinessFields(current.Code, input.Name, input.Facility, input.Owner); err != nil {
		return model.EmissionSample{}, err
	}
	reason := strings.TrimSpace(input.RevisionReason)
	if current.Status == model.EmissionSampleVerifiedStatus && reason == "" {
		// Verified samples feed compliance judgement, so every modification must
		// carry a revision reason. The whole request is rejected atomically.
		s.recordRejection(ctx, current.Code, ErrRevisionReason.Error(), actor, requestID)
		return model.EmissionSample{}, ErrRevisionReason
	}
	if reason == "" {
		reason = "updated emission sample fields"
	}
	current.Name = strings.TrimSpace(input.Name)
	current.Description = strings.TrimSpace(input.Description)
	current.Facility = strings.TrimSpace(input.Facility)
	current.Owner = strings.TrimSpace(input.Owner)
	current.Category = strings.TrimSpace(input.Category)
	current.RiskLevel = input.RiskLevel
	current.MetricValue = input.MetricValue
	current.MetricUnit = strings.TrimSpace(input.MetricUnit)
	current.EffectiveAt = input.EffectiveAt.UTC()
	current.Evidence = strings.TrimSpace(input.Evidence)
	current.RelatedCode = strings.ToUpper(strings.TrimSpace(input.RelatedCode))
	current.Version = input.ExpectedVersion + 1
	current.UpdatedAt = time.Now().UTC()
	revision := newSampleRevision(&current, reason, actor, requestID)
	if err := s.repository.UpdateWithRevision(ctx, id, input.ExpectedVersion, &current, revision); err != nil {
		if errors.Is(err, repository.ErrVersionConflict) {
			s.recordRejection(ctx, current.Code, repository.ErrVersionConflict.Error(), actor, requestID)
		}
		return model.EmissionSample{}, fmt.Errorf("update 排放样本: %w", err)
	}
	_ = s.security.Audit(ctx, actor, requestID, "update", "EmissionSample", id, current.Status, current.Status, "updated business fields")
	return s.repository.Get(ctx, id)
}

func (s *emissionSampleService) Transition(ctx context.Context, id uint, input dto.TransitionRequest, actor, requestID string) (model.EmissionSample, error) {
	current, err := s.repository.Get(ctx, id)
	if err != nil {
		return model.EmissionSample{}, err
	}
	target := strings.TrimSpace(input.Status)
	if !constants.CanTransition(constants.EmissionSampleTransitions, current.Status, target) {
		return model.EmissionSample{}, fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, current.Status, target)
	}
	before := current.Status
	current.Status = target
	current.Version = input.ExpectedVersion + 1
	current.UpdatedAt = time.Now().UTC()
	revision := newSampleRevision(&current, input.Reason, actor, requestID)
	if err := s.repository.UpdateWithRevision(ctx, id, input.ExpectedVersion, &current, revision); err != nil {
		return model.EmissionSample{}, fmt.Errorf("transition 排放样本: %w", err)
	}
	if err := s.security.Audit(ctx, actor, requestID, "transition", "EmissionSample", id, before, target, input.Reason); err != nil {
		return model.EmissionSample{}, fmt.Errorf("persist transition audit: %w", err)
	}
	return s.repository.Get(ctx, id)
}

func (s *emissionSampleService) Delete(ctx context.Context, id uint, actor, requestID string) error {
	current, err := s.repository.Get(ctx, id)
	if err != nil {
		return err
	}
	if err := s.repository.Delete(ctx, id); err != nil {
		return err
	}
	return s.security.Audit(ctx, actor, requestID, "delete", "EmissionSample", id, current.Status, "deleted", "soft deleted 排放样本")
}

func (s *emissionSampleService) StatusCounts(ctx context.Context) (map[string]int64, error) {
	return s.repository.CountByStatus(ctx)
}

func validateEmissionSampleBusinessFields(code, name, facility, owner string) error {
	if strings.TrimSpace(code) == "" || strings.TrimSpace(name) == "" || strings.TrimSpace(facility) == "" || strings.TrimSpace(owner) == "" {
		return ErrInvalidInput
	}
	return nil
}

// newSampleRevision snapshots the values that compliance judgement may rely on.
// Only the revision carrying the aggregate's current version stays authoritative.
func newSampleRevision(item *model.EmissionSample, reason, actor, requestID string) *model.SampleRevision {
	return &model.SampleRevision{
		Version: item.Version, State: strings.TrimSpace(item.Status), RiskLevel: strings.TrimSpace(item.RiskLevel),
		MetricValue: item.MetricValue, MetricUnit: strings.TrimSpace(item.MetricUnit),
		Evidence: strings.TrimSpace(item.Evidence), Reason: strings.TrimSpace(reason),
		Actor: strings.TrimSpace(actor), RequestID: strings.TrimSpace(requestID), CreatedAt: time.Now().UTC(),
	}
}

// recordRejection persists why a sample write was refused so the sample page can
// show the cause after a refresh. The rejection itself never alters revisions.
func (s *emissionSampleService) recordRejection(ctx context.Context, code, reason, actor, requestID string) {
	code = strings.TrimSpace(code)
	var sampleID uint
	if existing, err := s.repository.FindByCode(ctx, code); err == nil {
		sampleID = existing.ID
	}
	rejection := &model.SampleRejection{
		EmissionSampleID: sampleID, Code: code, Reason: strings.TrimSpace(reason),
		Actor: strings.TrimSpace(actor), RequestID: strings.TrimSpace(requestID), CreatedAt: time.Now().UTC(),
	}
	if err := s.repository.AppendRejection(ctx, rejection); err != nil {
		return
	}
	_ = s.security.Audit(ctx, actor, requestID, "reject", "EmissionSample", sampleID, "", "", reason)
}
