// =================================================================================
// Code generated and maintained by GoFrame CLI tool. DO NOT EDIT.
// =================================================================================

package runtime

import (
	"context"

	v1 "SentinelOps/api/runtime/v1"
)

type IRuntimeV1 interface {
	ListRuns(context.Context, *v1.ListRunsReq) (*v1.ListRunsRes, error)
	GetRun(context.Context, *v1.GetRunReq) (*v1.GetRunRes, error)
	GetTimeline(context.Context, *v1.GetTimelineReq) (*v1.TimelineRes, error)
	RunEvents(context.Context, *v1.RunEventsReq) (*v1.RunEventsRes, error)
	GetAttempts(context.Context, *v1.GetAttemptsReq) (*v1.AttemptsRes, error)
	GetCheckpoints(context.Context, *v1.GetCheckpointsReq) (*v1.CheckpointsRes, error)
	GetApprovals(context.Context, *v1.GetApprovalsReq) (*v1.ApprovalsRes, error)
	GetEffects(context.Context, *v1.GetEffectsReq) (*v1.EffectsRes, error)
	GetEvidence(context.Context, *v1.GetEvidenceReq) (*v1.EvidenceRes, error)
	ExpandEvidence(context.Context, *v1.ExpandEvidenceReq) (*v1.ExpandEvidenceRes, error)
	GetContext(context.Context, *v1.GetContextReq) (*v1.GetContextRes, error)
	GetTraces(context.Context, *v1.GetTracesReq) (*v1.TracesRes, error)
	RecoverRun(context.Context, *v1.RecoverRunReq) (*v1.OperationAcceptedRes, error)
	GetOperation(context.Context, *v1.GetOperationReq) (*v1.GetOperationRes, error)
	GetCapabilities(context.Context, *v1.GetCapabilitiesReq) (*v1.CapabilitiesRes, error)
	GetSafety(context.Context, *v1.GetSafetyReq) (*v1.SafetyRes, error)
	GetWorkerHealth(context.Context, *v1.GetWorkerHealthReq) (*v1.WorkerHealthRes, error)
	GetEval(context.Context, *v1.GetEvalReq) (*v1.EvalRes, error)
	GetRelease(context.Context, *v1.GetReleaseReq) (*v1.ReleaseRes, error)
	GetRetention(context.Context, *v1.GetRetentionReq) (*v1.RetentionRes, error)
}
