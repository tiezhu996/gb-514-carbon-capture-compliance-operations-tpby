package model

import "time"

// EmissionSample models 排放样本 as an independently versioned aggregate. The fields
// cover ownership, operational context, evidence and measured risk so later
// changes naturally span persistence, service and UI layers.
type EmissionSample struct {
	BaseModel
	Facility    string            `json:"facility" gorm:"size:120;index"`
	Owner       string            `json:"owner" gorm:"size:120;index"`
	Category    string            `json:"category" gorm:"size:80;index"`
	RiskLevel   string            `json:"riskLevel" gorm:"size:32;index"`
	MetricValue float64           `json:"metricValue"`
	MetricUnit  string            `json:"metricUnit" gorm:"size:24"`
	EffectiveAt time.Time         `json:"effectiveAt"`
	Evidence    string            `json:"evidence" gorm:"size:2000"`
	RelatedCode string            `json:"relatedCode" gorm:"size:64;index"`
	Revisions   []SampleRevision  `json:"revisions" gorm:"foreignKey:EmissionSampleID;constraint:OnDelete:CASCADE"`
	Rejections  []SampleRejection `json:"rejections" gorm:"foreignKey:EmissionSampleID;constraint:OnDelete:CASCADE"`
}

func (item *EmissionSample) GetBase() *BaseModel { return &item.BaseModel }

func (item EmissionSample) TableName() string { return "emission_samples" }

var EmissionSampleInitialStatus = "collected"

// EmissionSampleVerifiedStatus marks samples whose measured values feed
// compliance judgement; modifying them requires an explicit revision reason.
const EmissionSampleVerifiedStatus = "verified"

// SampleRevision is an append-only sample snapshot. Every accepted change keeps
// the previous measured values untouched and appends the next revision, so only
// the latest revision may serve as the basis for compliance judgement.
type SampleRevision struct {
	ID               uint      `json:"id" gorm:"primaryKey"`
	EmissionSampleID uint      `json:"emissionSampleId" gorm:"uniqueIndex:idx_sample_revision_version;not null"`
	Version          uint      `json:"version" gorm:"uniqueIndex:idx_sample_revision_version;not null"`
	State            string    `json:"state" gorm:"size:40;not null"`
	RiskLevel        string    `json:"riskLevel" gorm:"size:32;not null"`
	MetricValue      float64   `json:"metricValue"`
	MetricUnit       string    `json:"metricUnit" gorm:"size:24"`
	Evidence         string    `json:"evidence" gorm:"size:2000;not null"`
	Reason           string    `json:"reason" gorm:"size:500;not null"`
	Actor            string    `json:"actor" gorm:"size:80;not null;index"`
	RequestID        string    `json:"requestId" gorm:"size:64;not null;index"`
	CreatedAt        time.Time `json:"createdAt" gorm:"index"`
}

// SampleRejection is an append-only record of a refused sample write. It keeps
// why the request was rejected (missing revision reason, stale version or
// duplicate code) so the sample page can show the cause after a refresh.
type SampleRejection struct {
	ID               uint      `json:"id" gorm:"primaryKey"`
	EmissionSampleID uint      `json:"emissionSampleId" gorm:"index;not null"`
	Code             string    `json:"code" gorm:"size:64;not null;index"`
	Reason           string    `json:"reason" gorm:"size:500;not null"`
	Actor            string    `json:"actor" gorm:"size:80;not null;index"`
	RequestID        string    `json:"requestId" gorm:"size:64;not null;index"`
	CreatedAt        time.Time `json:"createdAt" gorm:"index"`
}
