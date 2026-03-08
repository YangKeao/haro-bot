package tools

import (
	"context"
	"errors"
)

type ApprovalDecision string

const (
	ApprovalAllow ApprovalDecision = "allow"
	ApprovalDeny  ApprovalDecision = "deny"
	ApprovalStop  ApprovalDecision = "stop"
)

var (
	ErrApprovalDenied  = errors.New("access denied by user")
	ErrApprovalStopped = errors.New("operation stopped by user")
)

type ApprovalRequest struct {
	SessionID int64
	UserID    int64
	Tool      string
	Path      string
	Reason    string
}

type Approver interface {
	RequestApproval(ctx context.Context, req ApprovalRequest) (ApprovalDecision, error)
}
