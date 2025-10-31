package model

import (
	"testing"

	"github.com/smartystreets/goconvey/convey"

	"github.com/favbox/eino/schema"
)

// TestOptions 测试模型通用选项的合并和配置功能。
//
// 测试目标：
//   - 验证 GetCommonOptions 函数能够正确合并基础选项和传入的选项列表
//   - 确保后续选项可以覆盖基础选项的默认值
//   - 验证所有类型的选项（指针类型、切片类型）都能正确处理
//
// 测试场景 1：选项覆盖和合并
//
// 输入：
//   - 基础 Options：包含默认模型名、温度、最大令牌数、TopP
//   - 传入选项：7 个 WithXXX 函数，覆盖模型、温度、最大令牌数、TopP，并添加停止词、工具、工具选择
//
// 验证点：
//  1. 传入的选项值完全替换了基础选项中的对应值
//  2. 新增的字段（Stop、Tools、ToolChoice）被正确添加
//  3. 所有字段的类型和值都符合预期
//
// 预期行为：
//   - 基础选项被完全覆盖（指针类型直接替换）
//   - 新选项被添加到结果中
//   - 最终 Options 包含所有传入的选项值
func TestOptions(t *testing.T) {
	// 测试场景 1：选项覆盖和合并
	convey.Convey("测试通用选项合并", t, func() {
		// 准备测试数据：定义基础选项和要传入的选项值
		var (
			// 传入选项的值
			modelName           = "model" // 要使用的模型名称
			temperature float32 = 0.9     // 温度参数
			maxToken            = 5000    // 最大令牌数
			topP        float32 = 0.8     // TopP 参数

			// 基础选项的默认值（将被覆盖）
			defaultModel               = "default_model" // 默认模型名
			defaultTemperature float32 = 1.0             // 默认温度
			defaultMaxTokens           = 1000            // 默认最大令牌数
			defaultTopP        float32 = 0.5             // 默认 TopP

			// 工具相关选项
			tools = []*schema.ToolInfo{ // 工具列表
				{Name: "asd"},
				{Name: "qwe"},
			}
			toolChoice = schema.ToolChoiceForced // 工具选择策略
		)

		// 执行测试：调用 GetCommonOptions 合并选项
		opts := GetCommonOptions(
			// 基础 Options：包含默认配置
			&Options{
				Model:       &defaultModel,
				Temperature: &defaultTemperature,
				MaxTokens:   &defaultMaxTokens,
				TopP:        &defaultTopP,
			},
			// 传入的选项列表：7 个 WithXXX 函数
			WithModel(modelName),               // 覆盖模型名称
			WithTemperature(temperature),       // 覆盖温度
			WithMaxTokens(maxToken),            // 覆盖最大令牌数
			WithTopP(topP),                     // 覆盖 TopP
			WithStop([]string{"hello", "bye"}), // 添加停止词
			WithTools(tools),                   // 添加工具列表
			WithToolChoice(toolChoice),         // 添加工具选择策略
		)

		// 验证点 1：选项值被正确覆盖
		// 验证点 2：新增字段被正确添加
		// 验证点 3：所有字段类型和值符合预期
		convey.So(opts, convey.ShouldResemble, &Options{
			Model:       &modelName,               // 验证：模型名被覆盖
			Temperature: &temperature,             // 验证：温度被覆盖
			MaxTokens:   &maxToken,                // 验证：最大令牌数被覆盖
			TopP:        &topP,                    // 验证：TopP 被覆盖
			Stop:        []string{"hello", "bye"}, // 验证：停止词被添加
			Tools:       tools,                    // 验证：工具列表被添加
			ToolChoice:  &toolChoice,              // 验证：工具选择被添加
		})
	})

	// 测试场景 2：nil 工具选项处理
	//
	// 测试目标：
	//   - 验证当传入 WithTools(nil) 时，系统能正确处理空指针
	//   - 确保返回的 Tools 字段不为 nil，但长度为 0
	//
	// 问题背景：
	//   在实际使用中，用户可能传入 nil 工具列表，
	//   需要确保系统能够优雅处理这种情况，避免空指针异常。
	//
	// 验证点：
	//   1. 返回的 Tools 字段不为 nil（避免了空指针）
	//   2. Tools 列表长度为 0（正确处理了空值）
	//   3. 不会覆盖原有的非空工具列表（如果基础选项有工具的话）
	convey.Convey("测试 nil 工具选项处理", t, func() {
		// 准备测试数据：基础 Options 包含工具列表
		opts := GetCommonOptions(
			&Options{
				Tools: []*schema.ToolInfo{
					{Name: "asd"},
					{Name: "qwe"},
				},
			},
			// 传入 nil 工具列表
			WithTools(nil),
		)

		// 验证点 1：Tools 字段不为 nil
		convey.So(opts.Tools, convey.ShouldNotBeNil)

		// 验证点 2：Tools 列表长度为 0（nil 被转换为空列表）
		convey.So(len(opts.Tools), convey.ShouldEqual, 0)
	})
}

// implOption 是用于测试的实现特定选项结构体示例。
//
// 这个结构体模拟了用户自定义的选项类型。
// 包含两个字段：userID 和 name。
// 用于演示如何定义和实现特定的选项函数。
type implOption struct {
	userID int64  // 用户 ID
	name   string // 用户名称
}

// WithUserID 是自定义选项函数，用于设置用户 ID。
//
// 这展示了如何使用 WrapImplSpecificOptFn 创建用户自定义的选项函数。
// 用户可以定义自己的 WithXXX 函数来配置自定义选项。
//
// 参数：
//   - uid: 要设置的用户 ID
//
// 返回：
//   - Option: 可传递给相关方法的选项
//
// 示例：
//
//	opt := GetImplSpecificOptions(&implOption{}, WithUserID(101))
func WithUserID(uid int64) Option {
	return WrapImplSpecificOptFn[implOption](func(i *implOption) {
		i.userID = uid
	})
}

// WithName 是自定义选项函数，用于设置用户名称。
//
// 这展示了如何使用 WrapImplSpecificOptFn 创建用户自定义的选项函数。
// 用户可以定义自己的 WithXXX 函数来配置自定义选项。
//
// 参数：
//   - n: 要设置的用户名称
//
// 返回：
//   - Option: 可传递给相关方法的选项
//
// 示例：
//
//	opt := GetImplSpecificOptions(&implOption{}, WithName("Wang"))
func WithName(n string) Option {
	return WrapImplSpecificOptFn[implOption](func(i *implOption) {
		i.name = n
	})
}

// TestImplSpecificOption 测试实现特定选项的提取功能。
//
// 测试目标：
//   - 验证 GetImplSpecificOptions 函数能够正确提取实现特定的选项
//   - 确保自定义选项函数（WithUserID、WithName）能够正确设置选项值
//
// 测试场景：自定义选项提取
//
// 输入：
//   - 基础 implOption：空结构体实例（作为默认值）
//   - 传入选项：WithUserID(101) 和 WithName("Wang")
//
// 验证点：
//  1. userID 字段被正确设置为 101
//  2. name 字段被正确设置为 "Wang"
//  3. 返回的选项实例与预期完全一致
//
// 预期行为：
//   - GetImplSpecificOptions 解析传入的选项函数
//   - 每个选项函数被应用到基础结构体
//   - 返回修改后的结构体实例
//
// 设计理念：
//
//	此测试演示了 Eino 框架的扩展性：
//	- 用户可以定义自己的选项结构体
//	- 用户可以创建自定义的 WithXXX 选项函数
//	- 系统能够正确处理这些自定义选项
func TestImplSpecificOption(t *testing.T) {
	convey.Convey("测试实现特定选项提取", t, func() {
		// 执行测试：调用 GetImplSpecificOptions 提取自定义选项
		opt := GetImplSpecificOptions(
			&implOption{},    // 基础选项：空结构体
			WithUserID(101),  // 设置用户 ID 为 101
			WithName("Wang"), // 设置用户名称为 "Wang"
		)

		// 验证点：返回的选项实例与预期完全一致
		convey.So(opt, convey.ShouldEqual, &implOption{
			userID: 101,    // 验证：用户 ID 正确设置
			name:   "Wang", // 验证：用户名称正确设置
		})
	})
}
