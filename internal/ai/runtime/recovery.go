package runtime

import (
	"fmt"
	"strings"

	"SentinelOps/internal/ai/workflow"
)

// RecoveryDecision 是纯 selector 的结果；它不包含客户端可覆盖字段。
type RecoveryDecision struct {
	Mode           workflow.RecoveryMode
	CheckpointID   string
	ImmutableQuery string
	ParkReason     string
}

// SelectRecovery 只根据数据库恢复真值和当前 Runtime hash 选择 Resume / Replay / parked。
func SelectRecovery(facts workflow.RecoveryFacts, currentCompatibilityHash string) (RecoveryDecision, error) {
	if strings.TrimSpace(facts.RunID) == "" {
		return RecoveryDecision{}, fmt.Errorf("recovery Run ID is required")
	}
	if err := validateSnapshotHash("current runtime compatibility hash", currentCompatibilityHash); err != nil {
		return RecoveryDecision{}, err
	}
	if err := validateSnapshotHash("stored runtime compatibility hash", facts.RuntimeCompatibilityHash); err != nil {
		return RecoveryDecision{}, err
	}

	if facts.Status == workflow.RunStatusParked {
		switch facts.ParkReason {
		case workflow.ParkReasonEffectUnknown, workflow.ParkReasonCheckpointMissing, workflow.ParkReasonCheckpointCorrupt:
			return RecoveryDecision{Mode: workflow.RecoveryModeParked, ParkReason: facts.ParkReason}, nil
		case workflow.ParkReasonRuntimeIncompatible:
			// exact Runtime 恢复后继续验证 checkpoint / dependency，不能只凭 hash 解锁。
		default:
			return RecoveryDecision{}, fmt.Errorf("unsupported parked recovery reason %q", facts.ParkReason)
		}
	} else if facts.Status != "" && facts.Status != workflow.RunStatusRunning {
		return RecoveryDecision{}, fmt.Errorf("recovery selector requires running or parked Run, got %q", facts.Status)
	}

	if facts.RuntimeCompatibilityHash != currentCompatibilityHash {
		return RecoveryDecision{Mode: workflow.RecoveryModeParked, ParkReason: workflow.ParkReasonRuntimeIncompatible}, nil
	}

	switch facts.Checkpoint.State {
	case workflow.RecoveryCheckpointValid:
		if strings.TrimSpace(facts.Checkpoint.ID) == "" {
			return RecoveryDecision{}, fmt.Errorf("valid recovery checkpoint is missing ID")
		}
		return RecoveryDecision{Mode: workflow.RecoveryModeResume, CheckpointID: facts.Checkpoint.ID}, nil
	case workflow.RecoveryCheckpointMissing:
		if facts.HasPublishedApproval || facts.HasEffect {
			return RecoveryDecision{Mode: workflow.RecoveryModeParked, ParkReason: workflow.ParkReasonCheckpointMissing}, nil
		}
		if strings.TrimSpace(facts.ImmutableQuery) == "" {
			return RecoveryDecision{}, fmt.Errorf("replay requires immutable query")
		}
		return RecoveryDecision{Mode: workflow.RecoveryModeReplay, ImmutableQuery: facts.ImmutableQuery}, nil
	case workflow.RecoveryCheckpointCorrupt:
		return RecoveryDecision{Mode: workflow.RecoveryModeParked, ParkReason: workflow.ParkReasonCheckpointCorrupt}, nil
	default:
		return RecoveryDecision{}, fmt.Errorf("unknown recovery checkpoint state %q", facts.Checkpoint.State)
	}
}
