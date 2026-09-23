package model

import "time"

// EmissionSample models 排放样本 as an independently versioned aggregate. The fields
// cover ownership, operational context, evidence and measured risk so later
// changes naturally span persistence, service and UI layers.
type EmissionSample struct {
	BaseModel
	Facility    string           `json:"facility" gorm:"size:120;index"`
	Owner       string           `json:"owner" gorm:"size:120;index"`
	Category    string           `json:"category" gorm:"size:80;index"`
	RiskLevel   string           `json:"riskLevel" gorm:"size:32;index"`
	MetricValue float64          `json:"metricValue"`
	MetricUnit  string           `json:"metricUnit" gorm:"size:24"`
	EffectiveAt time.Time        `json:"effectiveAt"`
	Evidence    string           `json:"evidence" gorm:"size:2000"`
	RelatedCode string           `json:"relatedCode" gorm:"size:64;index"`
	Revisions   []SampleRevision `json:"revisions" gorm:"foreignKey:EmissionSampleID;constraint:OnDelete:CASCADE"`
}

func (item *EmissionSample) GetBase() *BaseModel { return &item.BaseModel }

func (item EmissionSample) TableName() string { return "emission_samples" }

var EmissionSampleInitialStatus = "collected"

const EmissionSampleStatusVerified = "verified"

// SampleRevision kinds: "revision" rows are operator-submitted amendments of a
// verified sample; "baseline" rows are backfilled snapshots that preserve the
// pre-amendment values when the first revision is created.
const (
	SampleRevisionKindRevision = "revision"
	SampleRevisionKindBaseline = "baseline"
)

// SampleRevision is an append-only snapshot of 排放样本 values. Every verified
// amendment appends a new row and never overwrites earlier ones. Only the
// highest-version revision (Current == true) may be used as the compliance
// association basis.
type SampleRevision struct {
	ID               uint      `json:"id" gorm:"primaryKey"`
	EmissionSampleID uint      `json:"emissionSampleId" gorm:"uniqueIndex:idx_sample_revision_version;not null"`
	Version          uint      `json:"version" gorm:"uniqueIndex:idx_sample_revision_version;not null"`
	Kind             string    `json:"kind" gorm:"size:16;not null;default:revision"`
	Name             string    `json:"name" gorm:"size:160;not null"`
	Description      string    `json:"description" gorm:"size:1000"`
	Facility         string    `json:"facility" gorm:"size:120;not null"`
	Owner            string    `json:"owner" gorm:"size:120;not null"`
	Category         string    `json:"category" gorm:"size:80;not null"`
	RiskLevel        string    `json:"riskLevel" gorm:"size:32;not null"`
	MetricValue      float64   `json:"metricValue"`
	MetricUnit       string    `json:"metricUnit" gorm:"size:24"`
	EffectiveAt      time.Time `json:"effectiveAt"`
	Evidence         string    `json:"evidence" gorm:"size:2000"`
	RelatedCode      string    `json:"relatedCode" gorm:"size:64"`
	Reason           string    `json:"reason" gorm:"size:500;not null"`
	Actor            string    `json:"actor" gorm:"size:80;not null;index"`
	RequestID        string    `json:"requestId" gorm:"size:64;not null;index"`
	CreatedAt        time.Time `json:"createdAt" gorm:"index"`
	Current          bool      `json:"current" gorm:"-"`
}
