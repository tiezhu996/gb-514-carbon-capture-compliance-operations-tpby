package service

import (
	"context"
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
	Revise(context.Context, uint, dto.ReviseEmissionSample, string, string) (model.EmissionSample, error)
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
	if err := s.repository.Create(ctx, &item); err != nil {
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
	if current.Status == model.EmissionSampleStatusVerified {
		return model.EmissionSample{}, ErrVerifiedRevision
	}
	if err := validateEmissionSampleBusinessFields(current.Code, input.Name, input.Facility, input.Owner); err != nil {
		return model.EmissionSample{}, err
	}
	applyEmissionSampleUpdate(&current, input)
	current.Version = input.ExpectedVersion + 1
	current.UpdatedAt = time.Now().UTC()
	if err := s.repository.Update(ctx, id, input.ExpectedVersion, &current); err != nil {
		return model.EmissionSample{}, fmt.Errorf("update 排放样本: %w", err)
	}
	_ = s.security.Audit(ctx, actor, requestID, "update", "EmissionSample", id, current.Status, current.Status, "updated business fields")
	return s.repository.Get(ctx, id)
}

// Revise amends a verified sample. The original values are preserved as an
// append-only baseline snapshot and the new values become the next revision.
// A missing reason, stale expected version, or concurrent submit for the same
// sample code rejects the entire request and adds no revision.
func (s *emissionSampleService) Revise(ctx context.Context, id uint, input dto.ReviseEmissionSample, actor, requestID string) (model.EmissionSample, error) {
	current, err := s.repository.Get(ctx, id)
	if err != nil {
		return model.EmissionSample{}, err
	}
	if current.Status != model.EmissionSampleStatusVerified {
		return model.EmissionSample{}, ErrVerifiedRevision
	}
	if strings.TrimSpace(input.Reason) == "" {
		return model.EmissionSample{}, ErrRevisionReason
	}
	if err := validateEmissionSampleBusinessFields(current.Code, input.Name, input.Facility, input.Owner); err != nil {
		return model.EmissionSample{}, err
	}

	var baseline *model.SampleRevision
	if len(current.Revisions) == 0 {
		baseline = sampleRevisionFrom(current, model.SampleRevisionKindBaseline,
			"baseline snapshot of pre-revision verified values", "system", "baseline-"+current.Code)
	}
	previousVersion := current.Version
	applyEmissionSampleUpdate(&current, input.UpdateEmissionSample)
	current.Version = input.ExpectedVersion + 1
	current.UpdatedAt = time.Now().UTC()
	revision := sampleRevisionFrom(current, model.SampleRevisionKindRevision, input.Reason, actor, requestID)
	if err := s.repository.Revise(ctx, id, input.ExpectedVersion, &current, revision, baseline); err != nil {
		return model.EmissionSample{}, fmt.Errorf("revise 排放样本: %w", err)
	}
	if err := s.security.Audit(ctx, actor, requestID, "revise", "EmissionSample", id,
		fmt.Sprintf("v%d", previousVersion), fmt.Sprintf("v%d", current.Version),
		"revised verified sample: "+strings.TrimSpace(input.Reason)); err != nil {
		return model.EmissionSample{}, fmt.Errorf("persist revision audit: %w", err)
	}
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
	if err := s.repository.Update(ctx, id, input.ExpectedVersion, &current); err != nil {
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

func applyEmissionSampleUpdate(current *model.EmissionSample, input dto.UpdateEmissionSample) {
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
}

func sampleRevisionFrom(item model.EmissionSample, kind, reason, actor, requestID string) *model.SampleRevision {
	return &model.SampleRevision{
		Version: item.Version, Kind: kind,
		Name: item.Name, Description: item.Description, Facility: item.Facility, Owner: item.Owner,
		Category: item.Category, RiskLevel: item.RiskLevel, MetricValue: item.MetricValue,
		MetricUnit: item.MetricUnit, EffectiveAt: item.EffectiveAt, Evidence: item.Evidence,
		RelatedCode: item.RelatedCode, Reason: strings.TrimSpace(reason), Actor: strings.TrimSpace(actor),
		RequestID: strings.TrimSpace(requestID), CreatedAt: time.Now().UTC(),
	}
}
