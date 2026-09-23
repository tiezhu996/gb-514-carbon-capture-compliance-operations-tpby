package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/blueship581/carbon-capture-compliance-operations/backend/internal/config"
	"github.com/blueship581/carbon-capture-compliance-operations/backend/internal/dto"
	"github.com/blueship581/carbon-capture-compliance-operations/backend/internal/model"
	"github.com/blueship581/carbon-capture-compliance-operations/backend/internal/repository"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func newEmissionSampleTestService(t *testing.T) (EmissionSampleService, context.Context) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := db.AutoMigrate(&model.EmissionSample{}, &model.SampleRevision{}, &model.SampleRejection{}, &model.AuditLog{}); err != nil {
		t.Fatalf("migrate database: %v", err)
	}
	repo := repository.NewEmissionSampleRepository(db)
	security := NewSecurityService(repository.NewSecurityRepository(db), config.Config{})
	return NewEmissionSampleService(repo, security), context.Background()
}

func createVerifiedSample(t *testing.T, svc EmissionSampleService, ctx context.Context) model.EmissionSample {
	t.Helper()
	now := time.Now().UTC()
	created, err := svc.Create(ctx, dto.CreateEmissionSample{
		Code: "ES-REV", Name: "Stack emission sample", Description: "initial sampling",
		Facility: "Capture train A", Owner: "operator", Category: "emissions", RiskLevel: "medium",
		MetricValue: 21.5, MetricUnit: "ppm", EffectiveAt: now,
		Evidence: "analyzer reading before calibration", RelatedCode: "PR-TEST",
	}, "operator", "request-create")
	if err != nil {
		t.Fatalf("create sample: %v", err)
	}
	if created.Version != 1 || len(created.Revisions) != 1 || created.Revisions[0].RequestID != "request-create" {
		t.Fatalf("initial sample revision context missing: %+v", created.Revisions)
	}
	verified, err := svc.Transition(ctx, created.ID, dto.TransitionRequest{
		Status: "verified", ExpectedVersion: created.Version, Reason: "laboratory confirmed the reading",
	}, "operator", "request-verify")
	if err != nil {
		t.Fatalf("verify sample: %v", err)
	}
	if verified.Status != "verified" || len(verified.Revisions) != 2 || verified.Revisions[1].State != "verified" {
		t.Fatalf("verification revision missing: %+v", verified)
	}
	return verified
}

func sampleUpdateInput(sample model.EmissionSample, reason string) dto.UpdateEmissionSample {
	return dto.UpdateEmissionSample{
		ExpectedVersion: sample.Version, Name: sample.Name, Description: "recalibrated sampling",
		Facility: sample.Facility, Owner: sample.Owner, Category: sample.Category, RiskLevel: "high",
		MetricValue: 24.8, MetricUnit: "ppm", EffectiveAt: sample.EffectiveAt,
		Evidence: "calibrated analyzer reading", RelatedCode: sample.RelatedCode, RevisionReason: reason,
	}
}

func TestEmissionSampleVerifiedRevisionLifecycle(t *testing.T) {
	svc, ctx := newEmissionSampleTestService(t)
	verified := createVerifiedSample(t, svc, ctx)

	_, err := svc.Update(ctx, verified.ID, sampleUpdateInput(verified, "  "), "operator", "request-no-reason")
	if !errors.Is(err, ErrRevisionReason) {
		t.Fatalf("expected revision reason rejection, got %v", err)
	}
	reloaded, err := svc.Get(ctx, verified.ID)
	if err != nil {
		t.Fatalf("reload sample: %v", err)
	}
	if reloaded.Version != verified.Version || len(reloaded.Revisions) != 2 || reloaded.MetricValue != 21.5 {
		t.Fatalf("rejected update must not add revisions or change values: %+v", reloaded)
	}
	if len(reloaded.Rejections) != 1 || reloaded.Rejections[0].Reason != ErrRevisionReason.Error() ||
		reloaded.Rejections[0].Actor != "operator" || reloaded.Rejections[0].RequestID != "request-no-reason" {
		t.Fatalf("rejection was not persisted for the sample page: %+v", reloaded.Rejections)
	}

	updated, err := svc.Update(ctx, verified.ID, sampleUpdateInput(verified, "analyzer recalibrated after audit"), "operator", "request-revise")
	if err != nil {
		t.Fatalf("revise verified sample: %v", err)
	}
	if updated.Version != 3 || len(updated.Revisions) != 3 {
		t.Fatalf("expected appended revision v3, got %+v", updated.Revisions)
	}
	latest := updated.Revisions[2]
	if latest.MetricValue != 24.8 || latest.RiskLevel != "high" || latest.Reason != "analyzer recalibrated after audit" ||
		latest.Actor != "operator" || latest.RequestID != "request-revise" {
		t.Fatalf("latest revision is not the compliance basis: %+v", latest)
	}
	if updated.Revisions[0].MetricValue != 21.5 || updated.Revisions[1].MetricValue != 21.5 {
		t.Fatalf("original values must stay preserved: %+v", updated.Revisions)
	}
	if updated.MetricValue != 24.8 {
		t.Fatalf("aggregate must reflect only the latest revision: %+v", updated)
	}

	_, err = svc.Update(ctx, verified.ID, sampleUpdateInput(verified, "stale writer retry"), "operator", "request-stale")
	if !errors.Is(err, repository.ErrVersionConflict) {
		t.Fatalf("expected stale version rejection, got %v", err)
	}
	reloaded, err = svc.Get(ctx, verified.ID)
	if err != nil {
		t.Fatalf("reload after stale update: %v", err)
	}
	if reloaded.Version != 3 || len(reloaded.Revisions) != 3 {
		t.Fatalf("stale update must not add revisions: %+v", reloaded.Revisions)
	}
	if len(reloaded.Rejections) != 2 || reloaded.Rejections[0].RequestID != "request-stale" {
		t.Fatalf("stale version rejection missing from history: %+v", reloaded.Rejections)
	}
}

func TestEmissionSampleRejectsDuplicateCodeSubmission(t *testing.T) {
	svc, ctx := newEmissionSampleTestService(t)
	verified := createVerifiedSample(t, svc, ctx)

	now := time.Now().UTC()
	duplicate := dto.CreateEmissionSample{
		Code: "es-rev", Name: "Concurrent duplicate sample", Facility: "Capture train B", Owner: "operator",
		Category: "emissions", RiskLevel: "low", MetricValue: 9, MetricUnit: "ppm",
		EffectiveAt: now, Evidence: "concurrent submission", RelatedCode: "PR-TEST",
	}
	_, err := svc.Create(ctx, duplicate, "operator", "request-duplicate")
	if !errors.Is(err, ErrDuplicateCode) {
		t.Fatalf("expected duplicate code rejection, got %v", err)
	}
	reloaded, err := svc.Get(ctx, verified.ID)
	if err != nil {
		t.Fatalf("reload existing sample: %v", err)
	}
	if len(reloaded.Revisions) != 2 {
		t.Fatalf("duplicate submission must not add revisions: %+v", reloaded.Revisions)
	}
	if len(reloaded.Rejections) != 1 || reloaded.Rejections[0].Code != "ES-REV" ||
		reloaded.Rejections[0].RequestID != "request-duplicate" {
		t.Fatalf("duplicate code rejection missing from history: %+v", reloaded.Rejections)
	}
	page, err := svc.List(ctx, dto.PageQuery{Page: 1, PageSize: 20})
	if err != nil {
		t.Fatalf("list samples: %v", err)
	}
	if page.Total != 1 {
		t.Fatalf("duplicate submission must not create a sample, total=%d", page.Total)
	}
	if len(page.Items) != 1 || len(page.Items[0].Revisions) != 2 || len(page.Items[0].Rejections) != 1 {
		t.Fatalf("list must embed revisions and rejections for the sample page: %+v", page.Items)
	}
}

func TestEmissionSampleUnverifiedUpdateKeepsDefaultReason(t *testing.T) {
	svc, ctx := newEmissionSampleTestService(t)
	now := time.Now().UTC()
	created, err := svc.Create(ctx, dto.CreateEmissionSample{
		Code: "ES-DRAFT", Name: "Unverified emission sample", Facility: "Capture train C", Owner: "operator",
		Category: "emissions", RiskLevel: "low", MetricValue: 5, MetricUnit: "ppm",
		EffectiveAt: now, Evidence: "initial reading", RelatedCode: "PR-TEST",
	}, "operator", "request-create-draft")
	if err != nil {
		t.Fatalf("create sample: %v", err)
	}
	updated, err := svc.Update(ctx, created.ID, dto.UpdateEmissionSample{
		ExpectedVersion: created.Version, Name: created.Name, Facility: created.Facility, Owner: created.Owner,
		Category: created.Category, RiskLevel: "medium", MetricValue: 6.5, MetricUnit: "ppm",
		EffectiveAt: created.EffectiveAt, Evidence: "second reading", RelatedCode: created.RelatedCode,
	}, "operator", "request-update-draft")
	if err != nil {
		t.Fatalf("unverified update without reason must pass: %v", err)
	}
	if updated.Version != 2 || len(updated.Revisions) != 2 || updated.Revisions[1].Reason != "updated emission sample fields" {
		t.Fatalf("default revision reason missing: %+v", updated.Revisions)
	}
	if len(updated.Rejections) != 0 {
		t.Fatalf("no rejection expected for unverified update: %+v", updated.Rejections)
	}
}
