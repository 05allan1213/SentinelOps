// registry.go 子 Agent 注册表：维护 IntentType → SubAgent 的全局映射。
// subagents 包中各 *_subagent.go 通过 init() 调用 RegisterSubAgent 自动注册。
package core

import "sync"

var (
	agents  = make(map[IntentType]SubAgent) // IntentType → SubAgent 映射
	agentMu sync.RWMutex                    // 读写锁，保证并发安全
)

// RegisterSubAgent 注册子 Agent，由 subagents 包中各 init() 调用
func RegisterSubAgent(agent SubAgent) {
	agentMu.Lock()
	defer agentMu.Unlock()
	agents[agent.Name()] = agent
}

// GetSubAgent 按 IntentType 获取 SubAgent，未注册返回 nil
func GetSubAgent(t IntentType) SubAgent {
	agentMu.RLock()
	defer agentMu.RUnlock()
	return agents[t]
}
