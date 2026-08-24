package policy

import "fmt"

// RiskLevel 是服务端 Catalog 使用的规范风险等级。
type RiskLevel string

const (
	RiskL0 RiskLevel = "L0"
	RiskL1 RiskLevel = "L1"
	RiskL2 RiskLevel = "L2"
)

// Validate 对未知风险等级 fail-closed。
func (l RiskLevel) Validate() error {
	switch l {
	case RiskL0, RiskL1, RiskL2:
		return nil
	default:
		return fmt.Errorf("unknown risk level %q", l)
	}
}
