// Package supervisor 提供了基于监督者模式的多智能体系统实现。
//
// 在监督者模式中，一个指定的监督者智能体负责协调和管理多个子智能体。
// 监督者可以将任务委派给子智能体并接收它们的响应，而子智能体只能与监督者通信（不能直接相互通信）。
// 这种层次化的结构通过协调的智能体交互实现复杂问题的解决。
package supervisor

import (
	"context"

	"github.com/favbox/eino/adk"
)

// Config 是监督者多智能体系统的配置结构。
type Config struct {
	// Supervisor 指定将作为监督者的智能体，负责协调和管理子智能体。
	Supervisor adk.Agent

	// SubAgents 指定将被监督者协调和管理的子智能体列表。
	SubAgents []adk.Agent
}

// New 使用给定的配置创建一个基于监督者模式的多智能体系统。
//
// 在监督者模式中，一个指定的监督者智能体负责协调多个子智能体。
// 监督者可以将任务委派给子智能体并接收它们的响应，而子智能体只能与监督者通信（不能直接相互通信）。
// 这种层次化的结构通过协调的智能体交互实现复杂问题的解决。
func New(ctx context.Context, conf *Config) (adk.Agent, error) {
	subAgents := make([]adk.Agent, 0, len(conf.SubAgents))
	supervisorName := conf.Supervisor.Name(ctx)
	for _, subAgent := range conf.SubAgents {
		subAgents = append(subAgents, adk.AgentWithDeterministicTransferTo(ctx, &adk.DeterministicTransferConfig{
			Agent:        subAgent,
			ToAgentNames: []string{supervisorName},
		}))
	}

	return adk.SetSubAgents(ctx, conf.Supervisor, subAgents)
}
