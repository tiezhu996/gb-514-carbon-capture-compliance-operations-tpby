package service

import "errors"

var (
	ErrInvalidTransition = errors.New("requested status transition is not allowed")
	ErrInvalidInput      = errors.New("business input validation failed")
	ErrUnauthorized      = errors.New("invalid username or password")
	ErrInactiveUser      = errors.New("user account is inactive")
	ErrReviewerRequired  = errors.New("reviewer or admin role is required for this decision")
	ErrDecisionLocked    = errors.New("compliance decision fields are locked after review begins")
	ErrRevisionReason    = errors.New("a revision reason is required when modifying a verified sample")
	ErrDuplicateCode     = errors.New("a record with this code already exists")
)
