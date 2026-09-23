package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/blueship581/carbon-capture-compliance-operations/backend/internal/config"
	"github.com/blueship581/carbon-capture-compliance-operations/backend/internal/dto"
	"github.com/blueship581/carbon-capture-compliance-operations/backend/internal/model"
	"github.com/blueship581/carbon-capture-compliance-operations/backend/internal/repository"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func newEmissionSampleService(t *testing.T) (EmissionSampleService, *gorm.DB) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:sample-revision-"+t.Name()+"?mode=memory&cache=shared&_pragma=busy_timeout(5000)"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open database: %v", err)
	}
	if err := db.AutoMigrate(&model.EmissionSample{}, &model.SampleRevision{}, &model.AuditLog{}); err != nil {
		t.Fatalf("migrate database: %v", err)
	}
	repo := repository.NewEmissionSampleRepository(db)
	security := NewSecurityService(repository.NewSecurityRepository(db), config.Config{})
	return NewEmissionSampleService(repo, security), db
}

func reviseInput(version uint, metric float64, reason string) dto.ReviseEmissionSample {
	return dto.ReviseEmissionSample{
		UpdateEmissionSample: dto.UpdateEmissionSample{
			ExpectedVersion: version, Name: "排放样本修订示例", Description: "修订后的描述",
			Facility: "碳捕集装置合规运行区域3", Owner: "安全主管组", Category: "复核", RiskLevel: "critical",
			MetricValue: metric, MetricUnit: "score", EffectiveAt: time.Now().UTC(),
			Evidence: "分析仪重新校准后复测", RelatedCode: "REL-514-03",
		},
		Reason: reason,
	}
}

func TestVerifiedSampleRevisionAppendsAndOnlyLatestIsCurrent(t *testing.T) {
	svc, _ := newEmissionSampleService(t)
	ctx := context.Background()
	now := time.Now().UTC()

	created, err := svc.Create(ctx, dto.CreateEmissionSample{
		Code: "ES-REV", Name: "排放样本修订示例", Description: "初始采集描述",
		Facility: "碳捕集装置合规运行区域3", Owner: "安全主管组", Category: "复核", RiskLevel: "high",
		MetricValue: 37.5, MetricUnit: "score", EffectiveAt: now, Evidence: "已完成基础证据核对", RelatedCode: "REL-514-03",
	}, "operator", "request-create")
	if err != nil {
		t.Fatalf("create sample: %v", err)
	}

	verified, err := svc.Transition(ctx, created.ID, dto.TransitionRequest{
		Status: "verified", ExpectedVersion: 1, Reason: "核验通过",
	}, "reviewer", "request-verify")
	if err != nil {
		t.Fatalf("verify sample: %v", err)
	}
	if verified.Version != 2 {
		t.Fatalf("expected version 2 after verification, got %d", verified.Version)
	}

	// Revising a sample with no prior revision history backfills a baseline
	// snapshot that preserves the pre-revision values, then appends v3.
	revised, err := svc.Revise(ctx, created.ID, reviseInput(2, 42.0, "校准偏差修正，需要重新关联合规判断"), "operator", "request-revise-1")
	if err != nil {
		t.Fatalf("revise verified sample: %v", err)
	}
	if revised.Version != 3 || len(revised.Revisions) != 2 {
		t.Fatalf("expected baseline + v3 revision, got version=%d revisions=%d", revised.Version, len(revised.Revisions))
	}
	baseline, first := revised.Revisions[0], revised.Revisions[1]
	if baseline.Kind != model.SampleRevisionKindBaseline || baseline.MetricValue != 37.5 || baseline.Current {
		t.Fatalf("baseline snapshot must keep original values and be non-current: %+v", baseline)
	}
	if first.Kind != model.SampleRevisionKindRevision || first.MetricValue != 42.0 || !first.Current ||
		first.Actor != "operator" || first.RequestID != "request-revise-1" ||
		first.Reason != "校准偏差修正，需要重新关联合规判断" {
		t.Fatalf("new revision must hold the amended values and be the current basis: %+v", first)
	}
	if revised.MetricValue != 42.0 {
		t.Fatalf("aggregate should reflect the latest revision values, got %v", revised.MetricValue)
	}

	// A second revision appends v4; no new baseline and only v4 is current.
	again, err := svc.Revise(ctx, created.ID, reviseInput(3, 44.2, "现场复测值更新"), "operator", "request-revise-2")
	if err != nil {
		t.Fatalf("second revision: %v", err)
	}
	if again.Version != 4 || len(again.Revisions) != 3 {
		t.Fatalf("expected three revisions after second amendment, got version=%d revisions=%d", again.Version, len(again.Revisions))
	}
	for index, revision := range again.Revisions {
		if revision.Current != (index == 2) {
			t.Fatalf("only the latest revision may be current: %+v", again.Revisions)
		}
	}
	if again.Revisions[0].MetricValue != 37.5 || again.Revisions[1].MetricValue != 42.0 || again.Revisions[2].MetricValue != 44.2 {
		t.Fatalf("revision history values were overwritten: %+v", again.Revisions)
	}

	// The same result must survive a fresh read after refresh.
	reread, err := svc.Get(ctx, created.ID)
	if err != nil {
		t.Fatalf("reread sample: %v", err)
	}
	if reread.Version != 4 || len(reread.Revisions) != 3 || !reread.Revisions[2].Current || reread.MetricValue != 44.2 {
		t.Fatalf("refreshed read lost revision history: %+v", reread)
	}
}

func TestVerifiedSampleRevisionRejectsMissingReason(t *testing.T) {
	svc, db := newEmissionSampleService(t)
	ctx := context.Background()
	sample := seedVerifiedSample(t, db)

	input := reviseInput(sample.Version, 50.0, "   ")
	_, err := svc.Revise(ctx, sample.ID, input, "operator", "request-no-reason")
	if !errors.Is(err, ErrRevisionReason) {
		t.Fatalf("expected missing reason rejection, got %v", err)
	}
	assertNoRevisionRow(t, db, sample.ID)
	assertUnchanged(t, db, sample.ID, 37.5, sample.Version)
}

func TestVerifiedSampleRevisionRejectsStaleVersion(t *testing.T) {
	svc, db := newEmissionSampleService(t)
	ctx := context.Background()
	sample := seedVerifiedSample(t, db)

	first, err := svc.Revise(ctx, sample.ID, reviseInput(sample.Version, 40.0, "第一次修订"), "operator", "request-first")
	if err != nil {
		t.Fatalf("first revision: %v", err)
	}
	_, err = svc.Revise(ctx, sample.ID, reviseInput(sample.Version, 41.0, "过期版本号的第二次修订"), "operator", "request-stale")
	if !errors.Is(err, repository.ErrVersionConflict) {
		t.Fatalf("expected version conflict, got %v", err)
	}
	assertRevisionCount(t, db, sample.ID, 2)
	assertUnchanged(t, db, sample.ID, 40.0, first.Version)
}

func TestVerifiedSampleRevisionRejectsConcurrentSameCodeSubmit(t *testing.T) {
	svc, db := newEmissionSampleService(t)
	sample := seedVerifiedSample(t, db)

	start := make(chan struct{})
	var wg sync.WaitGroup
	results := make(chan error, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(metric float64, requestID string) {
			defer wg.Done()
			<-start
			_, err := svc.Revise(context.Background(), sample.ID, reviseInput(sample.Version, metric, "并发修订"), "operator", requestID)
			results <- err
		}(48.0+float64(i), "request-concurrent-"+string(rune('A'+i)))
	}
	close(start)
	wg.Wait()
	close(results)

	var accepted, rejected int
	for err := range results {
		switch {
		case err == nil:
			accepted++
		case errors.Is(err, repository.ErrVersionConflict):
			rejected++
		default:
			t.Fatalf("unexpected concurrent revision error: %v", err)
		}
	}
	if accepted != 1 || rejected != 1 {
		t.Fatalf("expected exactly one accepted and one rejected revision, got accepted=%d rejected=%d", accepted, rejected)
	}
	assertRevisionCount(t, db, sample.ID, 2)
}

func TestVerifiedSampleRejectsPlainUpdate(t *testing.T) {
	svc, db := newEmissionSampleService(t)
	ctx := context.Background()
	sample := seedVerifiedSample(t, db)

	input := dto.UpdateEmissionSample{
		ExpectedVersion: sample.Version, Name: sample.Name, Facility: sample.Facility, Owner: sample.Owner,
		Category: sample.Category, RiskLevel: sample.RiskLevel, MetricValue: 99, MetricUnit: sample.MetricUnit,
		EffectiveAt: sample.EffectiveAt, Evidence: sample.Evidence, RelatedCode: sample.RelatedCode,
	}
	_, err := svc.Update(ctx, sample.ID, input, "operator", "request-plain-update")
	if !errors.Is(err, ErrVerifiedRevision) {
		t.Fatalf("verified samples must reject plain updates, got %v", err)
	}
	assertUnchanged(t, db, sample.ID, 37.5, sample.Version)

	// Non-verified samples keep the original update behavior.
	collected, err := svc.Create(ctx, dto.CreateEmissionSample{
		Code: "ES-PLAIN", Name: "普通样本", Facility: "作业区", Owner: "操作员", Category: "常规",
		RiskLevel: "low", MetricValue: 10, MetricUnit: "ppm", EffectiveAt: time.Now().UTC(),
	}, "operator", "request-create-plain")
	if err != nil {
		t.Fatalf("create collected sample: %v", err)
	}
	input.ExpectedVersion = collected.Version
	updated, err := svc.Update(ctx, collected.ID, input, "operator", "request-update-plain")
	if err != nil {
		t.Fatalf("collected sample update should still work: %v", err)
	}
	if updated.Version != 2 {
		t.Fatalf("expected plain update to bump version to 2, got %d", updated.Version)
	}
}

func seedVerifiedSample(t *testing.T, db *gorm.DB) model.EmissionSample {
	t.Helper()
	now := time.Now().UTC()
	item := model.EmissionSample{
		BaseModel: model.BaseModel{
			Code: "ES-VERIFIED", Name: "排放样本修订示例", Status: model.EmissionSampleStatusVerified,
			Version: 1, Description: "已核验样本",
		},
		Facility: "碳捕集装置合规运行区域3", Owner: "安全主管组", Category: "复核", RiskLevel: "high",
		MetricValue: 37.5, MetricUnit: "score", EffectiveAt: now, Evidence: "已完成核验", RelatedCode: "REL-514-03",
	}
	if err := db.Create(&item).Error; err != nil {
		t.Fatalf("seed verified sample: %v", err)
	}
	return item
}

func assertRevisionCount(t *testing.T, db *gorm.DB, id uint, want int) {
	t.Helper()
	var count int64
	if err := db.Model(&model.SampleRevision{}).Where("emission_sample_id = ?", id).Count(&count).Error; err != nil {
		t.Fatalf("count revisions: %v", err)
	}
	if int(count) != want {
		t.Fatalf("expected %d revision rows, got %d", want, count)
	}
}

func assertNoRevisionRow(t *testing.T, db *gorm.DB, id uint) {
	t.Helper()
	assertRevisionCount(t, db, id, 0)
}

func assertUnchanged(t *testing.T, db *gorm.DB, id uint, metric float64, version uint) {
	t.Helper()
	var item model.EmissionSample
	if err := db.First(&item, id).Error; err != nil {
		t.Fatalf("reload sample: %v", err)
	}
	if item.MetricValue != metric || item.Version != version {
		t.Fatalf("rejected request changed the sample: metric=%v version=%d", item.MetricValue, item.Version)
	}
}
