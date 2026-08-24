package runtime

import (
	"errors"
	"fmt"
)

// ErrRuntimeIncompatible 表示 Snapshot v1 不是精确相等；P12 决定 parked 转换。
var ErrRuntimeIncompatible = errors.New("runtime snapshot is incompatible")

// RequireExactCompatibility 实施 v1 唯一兼容规则，不做猜测或子集兼容。
func RequireExactCompatibility(stored, current string) error {
	if err := validateSnapshotHash("stored runtime compatibility hash", stored); err != nil {
		return err
	}
	if err := validateSnapshotHash("current runtime compatibility hash", current); err != nil {
		return err
	}
	if stored != current {
		return fmt.Errorf("%w: stored=%s current=%s", ErrRuntimeIncompatible, stored, current)
	}
	return nil
}
