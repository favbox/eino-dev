/*
 * instruction.go - 智能体转移指令生成
 *
 * 核心功能：
 *   - 提供标准化的智能体转移决策指令模板
 *   - 根据可用智能体列表动态生成传移指令。
 *
 * 设计特点：
 *   - 指令模板化：使用格式化字符传动态生成指令
 *   - 决策规则清晰: 明确指导智能体何时自行回答、何时转移任务
 *   - 自动化生成: 根据智能体名称和描述自动构建转移指令
 */

package adk

import (
	"context"
	"fmt"
	"strings"
)

const (
	TransferToAgentInstruction = `Available other agents: %s

Decision rule:
- If you're best suited for the question according to your description: ANSWER
- If another agent is better according its description: CALL '%s' function with their agent name

When transferring: OUTPUT ONLY THE FUNCTION CALL`
)

// genTransferToAgentInstruction 根据智能体列表生成转移指令。
// 遍历所有可用智能体，提取名称和描述，填充到指令模板中，生成完整的转移决策指令。
func genTransferToAgentInstruction(ctx context.Context, agents []Agent) string {
	var sb strings.Builder
	for _, agent := range agents {
		sb.WriteString(fmt.Sprintf("\n- Agent name: %s\n  Agent description: %s",
			agent.Name(ctx), agent.Description(ctx)))
	}

	return fmt.Sprintf(TransferToAgentInstruction, sb.String(), TransferToAgentToolName)
}
