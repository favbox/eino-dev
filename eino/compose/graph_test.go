package compose

import (
	"context"
	"fmt"
	"io"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/favbox/eino/callbacks"
	"github.com/favbox/eino/components/model"
	"github.com/favbox/eino/components/prompt"
	"github.com/favbox/eino/schema"
)

type chatModel struct {
	msgs []*schema.Message
}

func (c *chatModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	return c.msgs[0], nil
}

func (c *chatModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	sr, sw := schema.Pipe[*schema.Message](len(c.msgs))
	go func() {
		defer sw.Close()
		for _, msg := range c.msgs {
			sw.Send(msg, nil)
		}
	}()
	return sr, nil
}

func TestSingleGraph(t *testing.T) {
	const (
		nodeOfModel  = "model"
		nodeOfPrompt = "prompt"
	)

	ctx := context.Background()

	// 图
	g := NewGraph[map[string]any, *schema.Message]()

	// 提示词
	pt := prompt.FromMessages(
		schema.FString,
		schema.UserMessage("{location}的天气怎么样?"),
	)

	// 聊天模型
	cm := &chatModel{msgs: []*schema.Message{
		schema.AssistantMessage("天气很好", nil),
	}}

	// 添加图节点和边
	_ = g.AddChatTemplateNode("prompt", pt)
	_ = g.AddChatModelNode(nodeOfModel, cm, WithNodeName("MockChatModel"))
	_ = g.AddEdge(START, nodeOfPrompt)
	_ = g.AddEdge(nodeOfPrompt, nodeOfModel)
	_ = g.AddEdge(nodeOfModel, END)

	// 将图编译为可执行产物
	start := time.Now()
	r, err := g.Compile(context.Background(), WithMaxRunSteps(10))
	fmt.Println("编译耗时", time.Since(start))
	assert.NoError(t, err)

	// 非流->非流：同步调用
	in := map[string]any{"location": "suzhou"}
	start = time.Now()
	ret, err := r.Invoke(ctx, in)
	fmt.Println("同步耗时", time.Since(start))
	assert.NoError(t, err)
	fmt.Println("同步结果：", ret)

	// 非流->流：流式调用
	start = time.Now()
	s, err := r.Stream(ctx, in)
	fmt.Println("同步耗时", time.Since(start))
	assert.NoError(t, err)
	ret, _ = concatStreamReader(s)
	fmt.Println("流式结果：", ret)

	// 通过管道将入参写入并返回流读取器
	start = time.Now()
	sr, sw := schema.Pipe[map[string]any](1)
	_ = sw.Send(in, nil)
	sw.Close()
	fmt.Println("管道发送耗时", time.Since(start))

	// 流->流
	start = time.Now()
	s, err = r.Transform(ctx, sr)
	fmt.Println("流转耗时", time.Since(start))
	assert.NoError(t, err)
	ret, _ = concatStreamReader(s)
	fmt.Println("流转结果：", ret)

	// 错误测试
	in = map[string]any{"不对应的 key": "suzhou"}
	_, err = r.Invoke(ctx, in)
	assert.Errorf(t, err, "找不到 key：location")

	_, err = r.Stream(ctx, in)
	assert.Errorf(t, err, "找不到 key：location")

	sr, sw = schema.Pipe[map[string]any](1)
	_ = sw.Send(in, nil)
	sw.Close()
	_, err = r.Transform(ctx, sr)
	assert.Errorf(t, err, "找不到 key：location")
}

// person 接口定义 - 用于测试接口类型的类型转换能力
// 验证 Graph 节点可以接受实现该接口的任意类型
type person interface {
	Say() string
}

// doctor 结构体 - person 接口的具体实现
// 用于验证结构体到接口的隐式转换
type doctor struct {
	say string
}

// Say 实现 person 接口的方法
// 直接返回结构体持有的字符串值
func (d *doctor) Say() string {
	return d.say
}

// TestGraphWithImplementableType 测试 Graph 对接口类型的支持能力
// 验证类型转换链：string → *doctor → person接口 → string
// 关键场景：
//   - 接口作为节点输入类型
//   - 结构体自动转换为接口类型
//   - 流式和非流式执行模式
//   - 运行时步骤限制机制
func TestGraphWithImplementableType(t *testing.T) {
	const (
		node1 = "1st"
		node2 = "2nd"
	)

	ctx := context.Background()

	g := NewGraph[string, string]()

	// node1: 输入string，输出*doctor（实现 person 接口的类型）
	// 创建 doctor 实例，将输入字符串作为say字段值
	err := g.AddLambdaNode(node1, InvokableLambda(func(ctx context.Context, input string) (output *doctor, err error) {
		return &doctor{say: input}, nil
	}))
	assert.NoError(t, err)

	// node2: 输入 person 接口，输出 string（通过接口方法获取结果）
	// 验证 Graph 能正确处理接口类型输入和隐式类型转换
	err = g.AddLambdaNode(node2, InvokableLambda(func(ctx context.Context, input person) (output string, err error) {
		return input.Say(), nil
	}))
	assert.NoError(t, err)

	// 构建 DAG：START → node1 → node2 → END
	_ = g.AddEdge(START, node1)
	_ = g.AddEdge(node1, node2)
	_ = g.AddEdge(node2, END)

	// 编译 Graph，限制最大执行步骤为 10
	r, err := g.Compile(ctx, WithMaxRunSteps(10))
	assert.NoError(t, err)

	// 验证 1-2：验证步骤限制机制（步骤数=1，无法完成完整执行路径）
	_, err = r.Invoke(ctx, "你怎么样", WithRuntimeMaxSteps(1))
	assert.Error(t, err)
	assert.ErrorContains(t, err, "exceeds max steps")

	_, err = r.Invoke(ctx, "你怎么样", WithRuntimeMaxSteps(1))
	assert.Error(t, err)
	assert.ErrorContains(t, err, "exceeds max steps")

	// 测试 3：正常非流式执行（string → *doctor → person → string）
	out, err := r.Invoke(ctx, "你怎么样")
	assert.NoError(t, err)
	assert.Equal(t, "你怎么样", out)

	// 测试 4：流式执行模式验证
	outStream, err := r.Stream(ctx, "我很好")
	assert.NoError(t, err)
	defer outStream.Close()
	say, err := outStream.Recv()
	assert.NoError(t, err)
	assert.Equal(t, "我很好", say)
}

// 测试嵌套图（子图）的功能
// 验证以下关键能力：
//   - 主图可以包含子图节点
//   - 子图具有独立的执行流程
//   - 数据在子图和子图之间正确传递
//   - 四种执行模式（Invoke、Stream、Collect、TransForm）都支持嵌套图
//   - 回调处理器在嵌套图中的传递和执行
//
// 测试结构：
//   - 主图 g: string → sub_graph → *schema.Message
//   - 子图 sg: map[string]any → model → *schema.Message
//   - lambda1: string → map[string]any (提取 location 字段)
//   - lambda2: 处理模型输出，添加"after lambda 2:"前缀
func TestNestedGraph(t *testing.T) {
	const (
		nodeOfLambda1  = "lambda1"
		nodeOfLambda2  = "lambda2"
		nodeOfSubGraph = "sub_graph"
		nodeOfModel    = "model"
		nodeOfPrompt   = "prompt"
	)

	g := NewGraph[string, *schema.Message]()
	sg := NewGraph[map[string]any, *schema.Message]()

	// === 构建子图 sg ===
	// 	子图实现：prompt -> model
	_ = sg.AddChatTemplateNode(
		"prompt",
		prompt.FromMessages(schema.FString,
			schema.UserMessage("{location}天气怎么样?"),
		))
	_ = sg.AddChatModelNode(
		nodeOfModel,
		&chatModel{msgs: []*schema.Message{
			schema.AssistantMessage("天气还不错", nil),
		}},
		WithNodeName("MockChatModel"))
	_ = sg.AddEdge(START, nodeOfPrompt)
	_ = sg.AddEdge(nodeOfPrompt, nodeOfModel)
	_ = sg.AddEdge(nodeOfModel, END)

	// === 构建主题 g ===
	// 	主图实现：lambda1 -> sub_graph -> lambda2
	_ = g.AddLambdaNode(nodeOfLambda1,
		InvokableLambda[string, map[string]any](
			func(ctx context.Context, input string) (output map[string]any, err error) {
				return map[string]any{"location": input}, nil
			}),
		WithNodeName("Lambda1"))
	_ = g.AddGraphNode(nodeOfSubGraph,
		sg,
		WithNodeName("SubGraphName"))
	_ = g.AddLambdaNode(nodeOfLambda2,
		InvokableLambda[*schema.Message, *schema.Message](
			func(ctx context.Context, input *schema.Message) (output *schema.Message, err error) {
				input.Content = fmt.Sprintf("after lambda 2: %s", input.Content)
				return input, nil
			}))
	_ = g.AddEdge(START, nodeOfLambda1)
	_ = g.AddEdge(nodeOfLambda1, nodeOfSubGraph)
	_ = g.AddEdge(nodeOfSubGraph, nodeOfLambda2)
	_ = g.AddEdge(nodeOfLambda2, END)

	ctx := context.Background()
	r, err := g.Compile(ctx,
		WithMaxRunSteps(10),
		WithGraphName("GraphName"),
	)
	assert.NoError(t, err)

	// === 创建回调处理器 ===
	ck := "depth"
	cb := callbacks.NewHandlerBuilder().
		OnStartFn(func(ctx context.Context, info *callbacks.RunInfo, input callbacks.CallbackInput) context.Context {
			v, ok := ctx.Value(ck).(int)
			if ok {
				v++
			}
			return context.WithValue(ctx, ck, v)
		}).OnStartWithStreamInputFn(func(ctx context.Context, info *callbacks.RunInfo, input *schema.StreamReader[callbacks.CallbackInput]) context.Context {
		input.Close()

		v, ok := ctx.Value(ck).(int)
		if ok {
			v++
		}
		return context.WithValue(ctx, ck, v)
	}).Build()

	// 测试 1：Invoke 模式（非流式调用）
	_, err = r.Invoke(ctx, "苏州", WithCallbacks(cb))
	assert.NoError(t, err)

	rs, err := r.Stream(ctx, "苏州", WithCallbacks(cb))
	assert.NoError(t, err)
	for {
		_, err = rs.Recv()
		if err == io.EOF {
			break
		}
		assert.NoError(t, err)
	}

	sr, sw := schema.Pipe[string](5)
	_ = sw.Send("苏州", nil)
	sw.Close()
	_, err = r.Collect(ctx, sr, WithCallbacks(cb))
	assert.NoError(t, err)

	sr, sw = schema.Pipe[string](5)
	_ = sw.Send("苏州", nil)
	sw.Close()
	rt, err := r.Transform(ctx, sr, WithCallbacks(cb))
	assert.NoError(t, err)
	for {
		_, err = rt.Recv()
		if err == io.EOF {
			break
		}
		assert.NoError(t, err)
	}
}

func TestValidate(t *testing.T) {
	// 测试不匹配的节点
	g := NewGraph[string, string]()
	err := g.AddLambdaNode("1", InvokableLambda(func(ctx context.Context, input string) (output string, err error) { return "", nil }))
	assert.NoError(t, err)

	err = g.AddLambdaNode("2", InvokableLambda(func(ctx context.Context, input int) (output string, err error) { return "", nil }))

	err = g.AddEdge("1", "2")
	assert.ErrorContains(t, err, "graph edge[1]-[2]: start node's output type[string] and end node's input type[int] mismatch")

	// 测试不匹配的透传节点
	g = NewGraph[string, string]()
	err = g.AddLambdaNode("1", InvokableLambda(func(ctx context.Context, input string) (output string, err error) { return "", nil }))
	assert.NoError(t, err)

	err = g.AddPassthroughNode("2")
	assert.NoError(t, err)

	err = g.AddLambdaNode("3", InvokableLambda(func(ctx context.Context, input int) (output string, err error) { return "", nil }))
	assert.NoError(t, err)

	err = g.AddEdge("1", "2")
	assert.NoError(t, err)
	err = g.AddEdge("2", "3")
	assert.ErrorContains(t, err, "graph edge[2]-[3]: start node's output type[string] and end node's input type[int] mismatch")

	// 测试可能不匹配的透传节点
	g2 := NewGraph[any, string]()
	err = g2.AddLambdaNode("1", InvokableLambda(func(ctx context.Context, input any) (output any, err error) { return input, nil }))
	assert.NoError(t, err)
	err = g2.AddPassthroughNode("2")
	assert.NoError(t, err)
	err = g2.AddLambdaNode("3", InvokableLambda(func(ctx context.Context, input int) (output string, err error) { return strconv.Itoa(input), nil }))
	assert.NoError(t, err)
	err = g2.AddEdge(START, "1")
	assert.NoError(t, err)
	err = g2.AddEdge("1", "2")
	assert.NoError(t, err)
	err = g2.AddEdge("2", "3")
	assert.NoError(t, err)
	err = g2.AddEdge("3", END)
	assert.NoError(t, err)
	ru, err := g2.Compile(context.Background())
	assert.NoError(t, err)
	// success
	result, err := ru.Invoke(context.Background(), 1)
	assert.NoError(t, err)
	assert.Equal(t, "1", result)
	// fail
	_, err = ru.Invoke(context.Background(), "1")
	assert.ErrorContains(t, err, "failed to calculate next tasks: failed to update and get channels: get value from ready channel[3] fail: runtime type check fail, expected type: int, actual type: string")

	// 测试不匹配的图类型
	g = NewGraph[string, string]()
	err = g.AddLambdaNode("1", InvokableLambda(func(ctx context.Context, input int) (output string, err error) { return "", nil }))
	assert.NoError(t, err)
	err = g.AddLambdaNode("2", InvokableLambda(func(ctx context.Context, input string) (output int, err error) { return 0, nil }))
	assert.NoError(t, err)
	err = g.AddEdge("1", "2")
	assert.NoError(t, err)
	err = g.AddEdge(START, "1")
	assert.ErrorContains(t, err, "graph edge[start]-[1]: start node's output type[string] and end node's input type[int] mismatch")

	// 子图实现
	type A interface {
		A()
	}
	type B interface {
		B()
	}
	type AB interface{}
	lA := InvokableLambda(func(ctx context.Context, input A) (output string, err error) { return "", nil })
	lB := InvokableLambda(func(ctx context.Context, input B) (output string, err error) { return "", nil })
	lAB := InvokableLambda(func(ctx context.Context, input string) (output AB, err error) { return nil, nil })

	p := NewParallel().AddLambda("1", lA).AddLambda("2", lB).AddLambda("3", lAB)
	c := NewChain[string, map[string]any]().AppendLambda(lAB).AppendParallel(p)
	_, err = c.Compile(context.Background())
	assert.NoError(t, err)

	// 错误用法
	p = NewParallel().AddLambda("1", lA).AddLambda("2", lAB)
	c = NewChain[string, map[string]any]().AppendParallel(p)
	_, err = c.Compile(context.Background())
	assert.ErrorContains(t, err, "add parallel edge failed, from=start, to=node_0_parallel_0, err: graph edge[start]-[node_0_parallel_0]: start node's output type[string] and end node's input type[compose.A] mismatch")

	// 测试图输出类型检测
	gg := NewGraph[string, A]()
	err = gg.AddLambdaNode("nodeA", InvokableLambda(func(ctx context.Context, input A) (output A, err error) { return nil, nil }))
	assert.NoError(t, err)

	err = gg.AddLambdaNode("nodeA2", InvokableLambda(func(ctx context.Context, input A) (output A, err error) { return nil, nil }))
	assert.NoError(t, err)

	err = gg.AddLambdaNode("nodeB", InvokableLambda(func(ctx context.Context, input A) (output B, err error) { return nil, nil }))
	assert.NoError(t, err)

	err = gg.AddEdge("nodeA", END)
	assert.NoError(t, err)
	err = gg.AddEdge("nodeB", END)
	assert.ErrorContains(t, err, "graph edge[nodeB]-[end]: start node's output type[compose.B] and end node's input type[compose.A] mismatch")

	err = gg.AddEdge("nodeA2", END)
	assert.ErrorContains(t, err, "graph edge[nodeB]-[end]: start node's output type[compose.B] and end node's input type[compose.A] mismatch")

	// test any type
	anyG := NewGraph[any, string]()
	err = anyG.AddLambdaNode("node1", InvokableLambda(func(ctx context.Context, input string) (output any, err error) { return input + "node1", nil }))
	assert.NoError(t, err)

	err = anyG.AddLambdaNode("node2", InvokableLambda(func(ctx context.Context, input string) (output any, err error) { return input + "node2", nil }))
	assert.NoError(t, err)

	err = anyG.AddEdge(START, "node1")
	assert.NoError(t, err)

	err = anyG.AddEdge("node1", "node2")
	assert.NoError(t, err)

	err = anyG.AddEdge("node2", END)
	assert.NoError(t, err)

	r, err := anyG.Compile(context.Background())
	assert.NoError(t, err)
	result, err = r.Invoke(context.Background(), "start")
	assert.NoError(t, err)
	assert.Equal(t, "startnode1node2", result)

	streamResult, err := r.Stream(context.Background(), "start")
	assert.NoError(t, err)

	result = ""
	for {
		chunk, err := streamResult.Recv()
		if err != nil {
			if err == io.EOF {
				break
			}
			assert.NoError(t, err)
		}
		result += chunk
	}

	assert.Equal(t, "startnode1node2", result)

	// test any type runtime error
	anyG = NewGraph[any, string]()
	err = anyG.AddLambdaNode("node1", InvokableLambda(func(ctx context.Context, input string) (output any, err error) { return 123, nil }))
	if err != nil {
		t.Fatal(err)
	}
	err = anyG.AddLambdaNode("node2", InvokableLambda(func(ctx context.Context, input string) (output any, err error) { return input + "node2", nil }))
	if err != nil {
		t.Fatal(err)
	}
	err = anyG.AddEdge(START, "node1")
	if err != nil {
		t.Fatal(err)
	}
	err = anyG.AddEdge("node1", "node2")
	assert.NoError(t, err) // 编译期 any -> string 是合法的（宽到窄）
	err = anyG.AddEdge("node2", END)
	assert.NoError(t, err) // 编译期 any -> string合法
	r, err = anyG.Compile(context.Background())
	assert.NoError(t, err)
	_, err = r.Invoke(context.Background(), "start")
	assert.ErrorContains(t, err, "[GraphRunError] failed to calculate next tasks: failed to update and get channels: get value from ready channel[node2] fail: runtime type check fail, expected type: string, actual type: int")
	_, err = r.Stream(context.Background(), "start")
	assert.ErrorContains(t, err, "runtime type check fail")

	// test branch any type
	// success
	g = NewGraph[string, string]()
	err = g.AddLambdaNode("node1", InvokableLambda(func(ctx context.Context, input string) (output any, err error) { return input + "node1", nil }))
	if err != nil {
		t.Fatal(err)
	}
	err = g.AddLambdaNode("node2", InvokableLambda(func(ctx context.Context, input string) (output any, err error) { return input + "node2", nil }))
	if err != nil {
		t.Fatal(err)
	}
	err = g.AddLambdaNode("node3", InvokableLambda(func(ctx context.Context, input string) (output any, err error) { return input + "node3", nil }))
	if err != nil {
		t.Fatal(err)
	}
	err = g.AddBranch("node1", NewGraphBranch(func(ctx context.Context, in string) (endNode string, err error) {
		return "node2", nil
	}, map[string]bool{"node2": true, "node3": true}))
	if err != nil {
		t.Fatal(err)
	}
	err = g.AddEdge(START, "node1")
	if err != nil {
		t.Fatal(err)
	}
	err = g.AddEdge("node2", END)
	if err != nil {
		t.Fatal(err)
	}
	err = g.AddEdge("node3", END)
	if err != nil {
		t.Fatal(err)
	}
	rr, err := g.Compile(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	ret, err := rr.Invoke(context.Background(), "start")
	assert.NoError(t, err)
	assert.Equal(t, "startnode1node2", ret)
	streamResult, err = rr.Stream(context.Background(), "start")
	assert.NoError(t, err)
	ret, err = concatStreamReader(streamResult)
	assert.NoError(t, err)
	assert.Equal(t, "startnode1node2", ret)

	// fail
	g = NewGraph[string, string]()
	err = g.AddLambdaNode("node1", InvokableLambda(func(ctx context.Context, input string) (output any, err error) { return 1 /*error type*/, nil }))
	if err != nil {
		t.Fatal(err)
	}
	err = g.AddLambdaNode("node2", InvokableLambda(func(ctx context.Context, input string) (output any, err error) { return input + "node2", nil }))
	if err != nil {
		t.Fatal(err)
	}
	err = g.AddLambdaNode("node3", InvokableLambda(func(ctx context.Context, input string) (output any, err error) { return input + "node3", nil }))
	if err != nil {
		t.Fatal(err)
	}
	err = g.AddBranch("node1", NewGraphBranch(func(ctx context.Context, in string) (endNode string, err error) {
		return "node2", nil
	}, map[string]bool{"node2": true, "node3": true}))
	if err != nil {
		t.Fatal(err)
	}
	err = g.AddEdge(START, "node1")
	if err != nil {
		t.Fatal(err)
	}
	err = g.AddEdge("node2", END)
	if err != nil {
		t.Fatal(err)
	}
	err = g.AddEdge("node3", END)
	if err != nil {
		t.Fatal(err)
	}
	rr, err = g.Compile(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	_, err = rr.Invoke(context.Background(), "start")
	assert.ErrorContains(t, err, "runtime type check fail")
	_, err = rr.Stream(context.Background(), "start")
	assert.ErrorContains(t, err, "runtime type check fail")
}

// TestValidateMultiAnyValueBranch 测试多值分支(any value branch)的验证能力
// 关键验证点：
//  1. 单个节点可以有多个分支同时存在
//  2. 分支映射允许指定多个可能的结束节点
//  3. 所有映射的分支节点都会被执行（并行执行）
//  4. 分支条件函数的类型安全检查
//
// 测试场景：
//   - node1：输入string，输出any（任意类型）
//   - node2-node5：输入string，输出map[string]any（包含布尔标志）
//   - 分支1：从node1到node2/node3
//   - 分支2：从node1到node4/node5
//   - 期望：两个分支都被执行，返回多个节点的合并结果
//
// 成功案例：验证多分支并行执行
func TestValidateMultiAnyValueBranch(t *testing.T) {
	// ====== 成功案例 ======
	// 创建图：输入string，输出map[string]any
	g := NewGraph[string, map[string]any]()

	// node1: 字符串处理节点，输出any类型（支持任意类型转换）
	err := g.AddLambdaNode("node1", InvokableLambda(func(ctx context.Context, input string) (output any, err error) {
		return input + "node1", nil
	}))
	if err != nil {
		t.Fatal(err)
	}

	// node2: 返回map[string]any，键为"node2"，值为true
	err = g.AddLambdaNode("node2", InvokableLambda(func(ctx context.Context, input string) (output map[string]any, err error) {
		return map[string]any{"node2": true}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}

	// node3: 返回map[string]any，键为"node3"，值为true
	err = g.AddLambdaNode("node3", InvokableLambda(func(ctx context.Context, input string) (output map[string]any, err error) {
		return map[string]any{"node3": true}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}

	// node4: 返回map[string]any，键为"node4"，值为true
	err = g.AddLambdaNode("node4", InvokableLambda(func(ctx context.Context, input string) (output map[string]any, err error) {
		return map[string]any{"node4": true}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}

	// node5: 返回map[string]any，键为"node5"，值为true
	err = g.AddLambdaNode("node5", InvokableLambda(func(ctx context.Context, input string) (output map[string]any, err error) {
		return map[string]any{"node5": true}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}

	// ====== 添加多分支 ======
	// 分支1: 从node1开始，条件函数返回"node2"，映射到node2和node3
	// 设计意图：执行分支1时，node2和node3都会执行（并行）
	err = g.AddBranch("node1", NewGraphBranch(func(ctx context.Context, in string) (endNode string, err error) {
		return "node2", nil
	}, map[string]bool{"node2": true, "node3": true}))
	if err != nil {
		t.Fatal(err)
	}

	// 分支2: 从node1开始，条件函数返回"node4"，映射到node4和node5
	// 设计意图：执行分支2时，node4和node5都会执行（并行）
	err = g.AddBranch("node1", NewGraphBranch(func(ctx context.Context, in string) (endNode string, err error) {
		return "node4", nil
	}, map[string]bool{"node4": true, "node5": true}))
	if err != nil {
		t.Fatal(err)
	}

	// ====== 连接边 ======
	// 主边：START → node1
	err = g.AddEdge(START, "node1")
	if err != nil {
		t.Fatal(err)
	}

	// 分支1的结束边：node2 → END, node3 → END
	err = g.AddEdge("node2", END)
	if err != nil {
		t.Fatal(err)
	}

	err = g.AddEdge("node3", END)
	if err != nil {
		t.Fatal(err)
	}

	// 分支2的结束边：node4 → END, node5 → END
	err = g.AddEdge("node4", END)
	if err != nil {
		t.Fatal(err)
	}

	err = g.AddEdge("node5", END)
	if err != nil {
		t.Fatal(err)
	}

	// ====== 编译执行 ======
	// 编译图：验证分支配置正确性
	rr, err := g.Compile(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	// 测试Invoke模式：期望两个分支都被执行
	// 执行路径：START → node1 → (分支1: node2,node3) + (分支2: node4,node5) → END
	// 期望结果：{"node2": true, "node4": true}
	// 说明：分支条件返回"node2"触发分支1，返回"node4"触发分支2
	ret, err := rr.Invoke(context.Background(), "start")
	if err != nil {
		t.Fatal(err)
	}
	// 验证：同时包含node2和node4的标记
	if !ret["node2"].(bool) || !ret["node4"].(bool) {
		t.Fatal("test branch any type fail, result is unexpected")
	}

	// 测试Stream模式：验证流式执行也支持多分支
	streamResult, err := rr.Stream(context.Background(), "start")
	if err != nil {
		t.Fatal(err)
	}
	ret, err = concatStreamReader(streamResult)
	if err != nil {
		t.Fatal(err)
	}
	// 验证流式执行结果的一致性
	if !ret["node2"].(bool) || !ret["node4"].(bool) {
		t.Fatal("test branch any type fail, result is unexpected")
	}

	// ====== 失败案例 ======
	// 验证分支条件函数的类型安全：第二个分支的条件函数参数类型错误（int vs string）
	g = NewGraph[string, map[string]any]()

	// 重新添加节点（结构相同）
	err = g.AddLambdaNode("node1", InvokableLambda(func(ctx context.Context, input string) (output any, err error) {
		return input + "node1", nil
	}))
	if err != nil {
		t.Fatal(err)
	}

	err = g.AddLambdaNode("node2", InvokableLambda(func(ctx context.Context, input string) (output map[string]any, err error) {
		return map[string]any{"node2": true}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}

	err = g.AddLambdaNode("node3", InvokableLambda(func(ctx context.Context, input string) (output map[string]any, err error) {
		return map[string]any{"node3": true}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}

	err = g.AddLambdaNode("node4", InvokableLambda(func(ctx context.Context, input string) (output map[string]any, err error) {
		return map[string]any{"node4": true}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}

	err = g.AddLambdaNode("node5", InvokableLambda(func(ctx context.Context, input string) (output map[string]any, err error) {
		return map[string]any{"node5": true}, nil
	}))
	if err != nil {
		t.Fatal(err)
	}

	// 分支1：正常（输入string类型）
	err = g.AddBranch("node1", NewGraphBranch(func(ctx context.Context, in string) (endNode string, err error) {
		return "node2", nil
	}, map[string]bool{"node2": true, "node3": true}))
	if err != nil {
		t.Fatal(err)
	}

	// 分支2：错误（输入int类型，但应该与图输入类型string匹配）
	// 这个错误会在运行时被发现：条件函数参数类型与实际输入类型不匹配
	err = g.AddBranch("node1", NewGraphBranch(func(ctx context.Context, in int /*错误类型：应该是string*/) (endNode string, err error) {
		return "node4", nil
	}, map[string]bool{"node4": true, "node5": true}))
	if err != nil {
		t.Fatal(err)
	}

	// 连接边（结构相同）
	err = g.AddEdge(START, "node1")
	if err != nil {
		t.Fatal(err)
	}

	err = g.AddEdge("node2", END)
	if err != nil {
		t.Fatal(err)
	}

	err = g.AddEdge("node3", END)
	if err != nil {
		t.Fatal(err)
	}

	err = g.AddEdge("node4", END)
	if err != nil {
		t.Fatal(err)
	}

	err = g.AddEdge("node5", END)
	if err != nil {
		t.Fatal(err)
	}

	// 编译成功：类型检查在编译时相对宽松
	rr, err = g.Compile(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	// 测试Invoke模式：期望运行时错误（类型不匹配）
	_, err = rr.Invoke(context.Background(), "start")
	if err == nil || !strings.Contains(err.Error(), "runtime") {
		t.Fatal("test multi branch any type fail, haven't report runtime error")
	}

	// 测试Stream模式：期望运行时错误（类型不匹配）
	_, err = rr.Stream(context.Background(), "start")
	if err == nil || !strings.Contains(err.Error(), "runtime") {
		t.Fatal("test multi branch any type fail, haven't report runtime error")
	}
}

// TestAnyTypeWithKey 测试any类型图的键值(key)机制
// 关键验证点：
//  1. any类型图支持输入键（InputKey）机制
//  2. any类型图支持输出键（OutputKey）机制
//  3. 键值对数据在节点间的传递和累积
//  4. 流式和非流式执行都支持键值机制
//
// 测试场景：
//   - 图输入类型：any（任意类型）
//   - 图输出类型：map[string]any（键值对映射）
//   - node1: 从输入map中提取"node1"键的值，拼接"node1"，输出any
//   - node2: 接收node1的输出，拼接"node2"，输出any
//   - 期望：通过"node2"键获取最终结果："startnode1node2"
//
// 核心概念：
//   - WithInputKey: 指定从输入map中提取哪个键的值作为节点输入
//   - WithOutputKey: 指定将节点输出存储到输出map的哪个键下
func TestAnyTypeWithKey(t *testing.T) {
	// 创建any类型图：输入any类型，输出map[string]any（键值对）
	g := NewGraph[any, map[string]any]()

	// node1: 带输入键的lambda节点
	// WithInputKey("node1"): 从输入map中提取"node1"键的值("start")作为输入
	// 处理逻辑: "start" + "node1" = "startnode1"
	// 输出类型: any（任意类型，可存储到map的value中）
	err := g.AddLambdaNode("node1", InvokableLambda(func(ctx context.Context, input string) (output any, err error) {
		return input + "node1", nil
	}), WithInputKey("node1"))
	if err != nil {
		t.Fatal(err)
	}

	// node2: 带输出键的lambda节点
	// 处理逻辑: 接收node1的输出("startnode1")，拼接"node2"
	// WithOutputKey("node2"): 将输出存储到输出map的"node2"键下
	// 最终输出: "startnode1" + "node2" = "startnode1node2"
	err = g.AddLambdaNode("node2", InvokableLambda(func(ctx context.Context, input string) (output any, err error) {
		return input + "node2", nil
	}), WithOutputKey("node2"))
	if err != nil {
		t.Fatal(err)
	}

	// 构建图边：START → node1 → node2 → END
	err = g.AddEdge(START, "node1")
	if err != nil {
		t.Fatal(err)
	}

	err = g.AddEdge("node1", "node2")
	if err != nil {
		t.Fatal(err)
	}

	err = g.AddEdge("node2", END)
	if err != nil {
		t.Fatal(err)
	}

	// 编译图
	r, err := g.Compile(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	// 测试Invoke模式（非流式调用）
	// 输入：map[string]any{"node1": "start"}
	// 执行流程：
	//   1. node1从输入map提取"node1"键的值："start"
	//   2. node1处理："start" + "node1" = "startnode1"
	//   3. node2接收"startnode1"，处理："startnode1" + "node2" = "startnode1node2"
	//   4. node2的输出存储到输出map的"node2"键下
	// 期望输出：map[string]any{"node2": "startnode1node2"}
	result, err := r.Invoke(context.Background(), map[string]any{"node1": "start"})
	if err != nil {
		t.Fatal(err)
	}
	// 验证：从输出map的"node2"键获取最终结果
	if result["node2"] != "startnode1node2" {
		t.Fatal("test any type with key fail, result is unexpected")
	}

	// 测试Stream模式（流式调用）
	// 验证流式执行也支持键值机制，期望结果与Invoke模式一致
	streamResult, err := r.Stream(context.Background(), map[string]any{"node1": "start"})
	if err != nil {
		t.Fatal(err)
	}
	ret, err := concatStreamReader(streamResult)
	if err != nil {
		t.Fatal(err)
	}
	// 验证流式执行结果
	if ret["node2"] != "startnode1node2" {
		t.Fatal("test any type with key fail, result is unexpected")
	}
}

// TestInputKey 测试ChatTemplate节点的输入键机制和并行执行能力
// 关键验证点：
//  1. ChatTemplate节点支持InputKey和OutputKey机制
//  2. 多个节点可以同时从START开始执行（并行处理）
//  3. 变量替换功能正常工作（模板中的{variable}被替换）
//  4. Invoke模式和Transform模式都支持键值机制
//  5. 每个节点的输入输出相互独立，不互相干扰
//
// 测试场景：
//   - 图类型：map[string]any → map[string]any（键值对）
//   - 三个ChatTemplate节点：处理不同的变量模板
//   - 节点1：模板"{var1}"，输入键"1"，输出键"1"
//   - 节点2：模板"{var2}"，输入键"2"，输出键"2"
//   - 节点3：模板"{var3}"，输入键"3"，输出键"3"
//   - 期望：每个节点独立处理自己的变量，输出到对应的键
func TestInputKey(t *testing.T) {
	// 创建键值对图：输入map[string]any，输出map[string]any
	g := NewGraph[map[string]any, map[string]any]()

	// 节点1: ChatTemplate节点，处理变量var1
	// 模板："{var1}" - 待替换的变量占位符
	// WithInputKey("1"): 从输入map的"1"键提取变量映射
	// WithOutputKey("1"): 将输出存储到输出map的"1"键下
	// 变量替换：{var1} -> "a"
	err := g.AddChatTemplateNode("1", prompt.FromMessages(schema.FString, schema.UserMessage("{var1}")), WithOutputKey("1"), WithInputKey("1"))
	if err != nil {
		t.Fatal(err)
	}

	// 节点2: ChatTemplate节点，处理变量var2
	// 模板："{var2}"
	// WithInputKey("2"): 从输入map的"2"键提取变量映射
	// WithOutputKey("2"): 将输出存储到输出map的"2"键下
	// 变量替换：{var2} -> "b"
	err = g.AddChatTemplateNode("2", prompt.FromMessages(schema.FString, schema.UserMessage("{var2}")), WithOutputKey("2"), WithInputKey("2"))
	if err != nil {
		t.Fatal(err)
	}

	// 节点3: ChatTemplate节点，处理变量var3
	// 模板："{var3}"
	// WithInputKey("3"): 从输入map的"3"键提取变量映射
	// WithOutputKey("3"): 将输出存储到输出map的"3"键下
	// 变量替换：{var3} -> "c"
	err = g.AddChatTemplateNode("3", prompt.FromMessages(schema.FString, schema.UserMessage("{var3}")), WithOutputKey("3"), WithInputKey("3"))
	if err != nil {
		t.Fatal(err)
	}

	// ====== 构建图的边 ======
	// 所有节点都从START开始，到END结束
	// 这意味着三个节点可以并行执行（同时从START接收输入）

	// 三个节点都从START开始
	err = g.AddEdge(START, "1")
	if err != nil {
		t.Fatal(err)
	}

	err = g.AddEdge(START, "2")
	if err != nil {
		t.Fatal(err)
	}

	err = g.AddEdge(START, "3")
	if err != nil {
		t.Fatal(err)
	}

	// 三个节点都到END结束
	err = g.AddEdge("1", END)
	if err != nil {
		t.Fatal(err)
	}

	err = g.AddEdge("2", END)
	if err != nil {
		t.Fatal(err)
	}

	err = g.AddEdge("3", END)
	if err != nil {
		t.Fatal(err)
	}

	// 编译图，设置为最多100步执行
	// WithMaxRunSteps(100): 防止无限循环，这里足够三个节点完成
	r, err := g.Compile(context.Background(), WithMaxRunSteps(100))
	if err != nil {
		t.Fatal(err)
	}

	ctx := context.Background()

	// ====== 测试Invoke模式 ======
	// 输入：包含三个键值对的map
	//   - "1": {"var1": "a"}  -> 节点1处理，变量{var1}替换为"a"
	//   - "2": {"var2": "b"}  -> 节点2处理，变量{var2}替换为"b"
	//   - "3": {"var3": "c"}  -> 节点3处理，变量{var3}替换为"c"
	//
	// 期望输出：
	//   - "1": []*schema.Message{Content: "a"}
	//   - "2": []*schema.Message{Content: "b"}
	//   - "3": []*schema.Message{Content: "c"}
	result, err := r.Invoke(ctx, map[string]any{
		"1": map[string]any{"var1": "a"},
		"2": map[string]any{"var2": "b"},
		"3": map[string]any{"var3": "c"},
	})
	if err != nil {
		t.Fatal(err)
	}

	// 验证：每个节点的输出都是正确的变量替换结果
	if result["1"].([]*schema.Message)[0].Content != "a" ||
		result["2"].([]*schema.Message)[0].Content != "b" ||
		result["3"].([]*schema.Message)[0].Content != "c" {
		t.Fatal("invoke different")
	}

	// ====== 测试Transform模式 ======
	// Transform模式：接收流输入，输出流
	// 创建输入流，包含三个独立的输入批次

	// 创建管道，流容量为10
	sr, sw := schema.Pipe[map[string]any](10)

	// 发送三个输入批次到流中
	sw.Send(map[string]any{"1": map[string]any{"var1": "a"}}, nil)
	sw.Send(map[string]any{"2": map[string]any{"var2": "b"}}, nil)
	sw.Send(map[string]any{"3": map[string]any{"var3": "c"}}, nil)
	sw.Close() // 关闭写入端

	// 执行Transform操作：流式处理输入
	streamResult, err := r.Transform(ctx, sr)
	if err != nil {
		t.Fatal(err)
	}
	defer streamResult.Close()

	// 接收流输出并累积结果
	result = make(map[string]any)
	for {
		chunk, err := streamResult.Recv()
		if err == io.EOF {
			break // 流结束
		}
		if err != nil {
			t.Fatal(err)
		}
		// 累积每个chunk的结果到最终result中
		for k, v := range chunk {
			result[k] = v
		}
	}
	// 验证：流式处理结果与Invoke模式一致
	if result["1"].([]*schema.Message)[0].Content != "a" ||
		result["2"].([]*schema.Message)[0].Content != "b" ||
		result["3"].([]*schema.Message)[0].Content != "c" {
		t.Fatal("transform different")
	}
}

// TestTransferTask 测试任务转移算法（transferTask函数）的正确性
// 关键验证点：
//  1. 基于反向边(invertedEdges)重新排序任务列表
//  2. 按拓扑顺序组织节点，确保依赖关系正确
//  3. 同一层的节点保持相对顺序，但不同层按依赖关系排序
//  4. 每个节点只在其所有前驱节点之后出现
//
// 算法原理：
//   - invertedEdges: 反向边映射，key是目标节点，value是所有能到达key的源节点
//   - 例如: "1": {"3", "4"} 表示节点3和节点4都能到达节点1
//   - 目标是重新组织任务列表，使得每个节点出现在所有能到达它的节点之后
//
// 输入输出分析：
//   - 输入 in: 多层任务列表，每行是一组并发执行的任务
//   - 输出 in: 重新排序后的任务列表，符合拓扑依赖关系
//
// 图结构：
//
//	1 ← 3 ← 5 ← 7 ← 8
//	│   ↑   ↑   ↑
//	└── 4 ── 6 ──┘
//	2 ←─────┘
//
// 期望输出：
//
//	第1层: ["1"]      (没有依赖的节点)
//	第2层: ["3", "2"] (依赖1和2的节点，但2无依赖，故先)
//	第3层: ["5"]      (依赖3,5的节点)
//	第4层: ["7", "4"] (依赖5,6,4的节点，4无依赖)
//	第5层: ["8", "6"] (依赖7,8的节点，6无依赖)
func TestTransferTask(t *testing.T) {
	// ====== 输入：初始任务列表 ======
	// 每行表示一组可以并行执行的任务
	// 行号表示任务的层级（0-based）
	in := [][]string{
		// 第0行：["1", "2"] - 初始任务，节点1和节点2
		{
			"1",
			"2",
		},
		// 第1行：["3", "4", "5", "6"] - 假设的下一层任务
		{
			"3",
			"4",
			"5",
			"6",
		},
		// 第2行：["5", "6", "7"] - 假设的再下一层
		{
			"5",
			"6",
			"7",
		},
		// 第3行：["7", "8"] - 假设的再下一层
		{
			"7",
			"8",
		},
		// 第4行：["8"] - 最后一层
		{
			"8",
		},
	}

	// ====== 反向边映射 ======
	// key: 目标节点，value: 所有能到达key的源节点列表
	// 例如: "1": {"3", "4"} 表示节点3和节点4都能到达节点1
	invertedEdges := map[string][]string{
		"1": {"3", "4"}, // 节点3和4完成后才能执行节点1
		"2": {"5", "6"}, // 节点5和6完成后才能执行节点2
		"3": {"5"},      // 节点5完成后才能执行节点3
		"4": {"6"},      // 节点6完成后才能执行节点4
		"5": {"7"},      // 节点7完成后才能执行节点5
		"7": {"8"},      // 节点8完成后才能执行节点7
	}

	// 调用transferTask算法重新组织任务列表
	// 目标：按拓扑排序原则重新排列，确保依赖关系正确
	in = transferTask(in, invertedEdges)

	// ====== 验证结果 ======
	// 期望输出：重新排序后的任务列表
	expected := [][]string{
		// 第1层: 节点1（没有依赖其他节点）
		{
			"1",
		},
		// 第2层: 节点3和2
		//   - 节点3依赖节点5（在第3层），所以在第2层
		//   - 节点2没有依赖，在第2层
		{
			"3",
			"2",
		},
		// 第3层: 节点5（依赖节点7）
		{
			"5",
		},
		// 第4层: 节点7和4
		//   - 节点7依赖节点8（在第5层），所以在第4层
		//   - 节点4没有依赖（在第2层已经出现），但在第4层排序
		{
			"7",
			"4",
		},
		// 第5层: 节点8和6（没有依赖其他节点的节点）
		{
			"8",
			"6",
		},
	}

	// 使用DeepEqual比较结果，确保算法正确性
	if !reflect.DeepEqual(expected, in) {
		t.Fatal("not equal")
	}
}

// TestPregelEnd 测试Pregel执行模式中END节点的提前终止行为
// 关键验证点：
//  1. 当节点同时连接到END和其他节点时，Pregel模式会提前终止
//  2. 图执行在遇到END时停止，不会继续执行后续节点
//  3. 输出的是第一个遇到END的节点的结果，而不是最后节点的
//  4. 验证 Pregel vs DAG 执行模式的差异：DAG会继续执行，Pregel会终止
//
// 图结构：
//
//	START
//	  |
//	  v
//	node1 -----> node2
//	  |           |
//	  v           v
//	 END        END
//
// 执行流程分析：
//  1. START → node1: 执行node1，返回"node1"
//  2. node1 → END: 遇到END，停止执行（Pregel模式行为）
//  3. node1 → node2: 这条边不会被执行，因为已经在node1处终止
//  4. 最终输出: "node1"（来自node1，不是node2）
//
// 设计意义：
//   - END节点提供了一种"短路"机制，可以提前终止图执行
//   - 在需要根据中间结果决定是否继续的场景中很有用
//   - 例如：条件判断、错误处理、提前返回等情况
func TestPregelEnd(t *testing.T) {
	// 创建图：输入string，输出string
	g := NewGraph[string, string]()

	// node1: 返回常量字符串"node1"
	// 这个节点有两个出口：连接到END和连接到node2
	err := g.AddLambdaNode("node1", InvokableLambda(func(ctx context.Context, input string) (output string, err error) {
		return "node1", nil
	}))
	if err != nil {
		t.Fatal(err)
	}

	// node2: 返回常量字符串"node2"
	// 这个节点应该不会被执行（如果在node1处提前终止）
	err = g.AddLambdaNode("node2", InvokableLambda(func(ctx context.Context, input string) (output string, err error) {
		return "node2", nil
	}))
	if err != nil {
		t.Fatal(err)
	}

	// 构建图的边
	// START → node1: 从开始到node1
	err = g.AddEdge(START, "node1")
	if err != nil {
		t.Fatal(err)
	}

	// node1 → END: 从node1到结束（提前终止的关键边）
	// 当执行到node1时，这条边会导致图终止，不会继续到node2
	err = g.AddEdge("node1", END)
	if err != nil {
		t.Fatal(err)
	}

	// node1 → node2: 从node1到node2（conditional path）
	// 这条边存在，但Pregel模式会在node1 → END处终止，不会执行到这里
	err = g.AddEdge("node1", "node2")
	if err != nil {
		t.Fatal(err)
	}

	// node2 → END: 从node2到结束
	// 这条边不会被执行，因为node2不会被调用
	err = g.AddEdge("node2", END)
	if err != nil {
		t.Fatal(err)
	}

	// 编译图
	runner, err := g.Compile(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	// 执行图，期望输出为"node1"（来自node1，而不是node2）
	// 关键：Pregel模式会在遇到END时立即停止，不继续执行后续节点
	out, err := runner.Invoke(context.Background(), "")
	if err != nil {
		t.Fatal(err)
	}
	// 验证输出是"node1"，证明图在node1处提前终止，没有执行到node2
	if out != "node1" {
		t.Fatal("graph output is unexpected")
	}
}

// cb 编译回调处理器 - 收集图编译信息的测试辅助结构
// 用于捕获图编译过程中生成的GraphInfo对象，验证编译结果正确性
type cb struct {
	gInfo *GraphInfo
}

// OnFinish 实现编译回调接口，捕获编译完成的GraphInfo
func (c *cb) OnFinish(ctx context.Context, info *GraphInfo) {
	c.gInfo = info
}

// TestGraphCompileCallback 测试图编译回调机制的完整性
// 关键验证点：
//  1. 图编译时回调机制的正确触发
//  2. 编译信息（GraphInfo）的完整收集
//  3. 嵌套图（子图）编译信息的递归收集
//  4. 节点选项（NodeName、InputKey、OutputKey）的正确传递
//  5. 编译选项（MaxRunSteps、GraphName等）的正确传递
//  6. 边信息（Edges、DataEdges）的正确记录
//  7. 分支信息（Branches）的正确记录
//  8. 状态生成器函数的正确传递
//
// 测试图结构：
//   - 主图 top_level: 包含lambda节点、passthrough节点、子图节点
//   - 子图 sub_graph: 包含子子图节点
//   - 子子图 subSubGraph: 包含lambda节点
//   - 分支机制: node1 → pass1/pass2 → sub_graph → node3/node4
func TestGraphCompileCallback(t *testing.T) {
	t.Run("graph compile callback", func(t *testing.T) {
		// 定义状态类型 s，用于测试状态生成器
		type s struct{}

		// ====== 创建主图 g ======
		// WithGenLocalState: 启用状态生成器，返回*s{}实例
		g := NewGraph[map[string]any, map[string]any](WithGenLocalState(func(ctx context.Context) *s { return &s{} }))

		// node1: Lambda节点，包含节点名称和输入键选项
		lambda := InvokableLambda(func(ctx context.Context, input string) (output string, err error) {
			return "node1", nil
		})
		lambdaOpts := []GraphAddNodeOpt{WithNodeName("lambda_1"), WithInputKey("input_key")}
		err := g.AddLambdaNode("node1", lambda, lambdaOpts...)
		assert.NoError(t, err)

		// pass1/pass2: 直通节点，用于分支路径
		err = g.AddPassthroughNode("pass1")
		assert.NoError(t, err)
		err = g.AddPassthroughNode("pass2")
		assert.NoError(t, err)

		// 定义分支条件函数：从输入返回输入（透传）
		condition := func(ctx context.Context, input string) (string, error) {
			return input, nil
		}

		// 创建图分支：从node1到pass1和pass2
		branch := NewGraphBranch(condition, map[string]bool{"pass1": true, "pass2": true})
		err = g.AddBranch("node1", branch)
		assert.NoError(t, err)

		// START → node1
		err = g.AddEdge(START, "node1")
		assert.NoError(t, err)

		// ====== 创建子子图 subSubGraph ======
		// 最内层图：sub1 → END
		lambda2 := InvokableLambda(func(ctx context.Context, input string) (output string, err error) {
			return "node2", nil
		})
		lambdaOpts2 := []GraphAddNodeOpt{WithNodeName("lambda_2")}
		subSubGraph := NewGraph[string, string]()
		err = subSubGraph.AddLambdaNode("sub1", lambda2, lambdaOpts2...)
		assert.NoError(t, err)
		err = subSubGraph.AddEdge(START, "sub1")
		assert.NoError(t, err)
		err = subSubGraph.AddEdge("sub1", END)
		assert.NoError(t, err)

		// ====== 创建子图 subGraph ======
		// 中间层图：sub_sub_1 → END（sub_sub_1是subSubGraph的封装）
		subGraph := NewGraph[string, string]()
		var ssGraphCompileOpts []GraphCompileOption
		ssGraphOpts := []GraphAddNodeOpt{WithGraphCompileOptions(ssGraphCompileOpts...)}
		err = subGraph.AddGraphNode("sub_sub_1", subSubGraph, ssGraphOpts...)
		assert.NoError(t, err)
		err = subGraph.AddEdge(START, "sub_sub_1")
		assert.NoError(t, err)
		err = subGraph.AddEdge("sub_sub_1", END)
		assert.NoError(t, err)

		// ====== 将子图添加到主图 ======
		// 子图编译选项：最大运行步骤2，图名称"sub_graph"
		subGraphCompileOpts := []GraphCompileOption{WithMaxRunSteps(2), WithGraphName("sub_graph")}
		subGraphOpts := []GraphAddNodeOpt{WithGraphCompileOptions(subGraphCompileOpts...)}
		err = g.AddGraphNode("sub_graph", subGraph, subGraphOpts...)
		assert.NoError(t, err)

		// pass1/pass2 → sub_graph
		err = g.AddEdge("pass1", "sub_graph")
		assert.NoError(t, err)
		err = g.AddEdge("pass2", "sub_graph")
		assert.NoError(t, err)

		// ====== 添加后续节点 ======
		// node3: 带输出键的lambda节点
		lambda3 := InvokableLambda(func(ctx context.Context, input string) (output string, err error) {
			return "node3", nil
		})
		lambdaOpts3 := []GraphAddNodeOpt{WithNodeName("lambda_3"), WithOutputKey("lambda_3")}
		err = g.AddLambdaNode("node3", lambda3, lambdaOpts3...)
		assert.NoError(t, err)

		// node4: 带输出键的lambda节点
		lambda4 := InvokableLambda(func(ctx context.Context, input string) (output string, err error) {
			return "node4", nil
		})
		lambdaOpts4 := []GraphAddNodeOpt{WithNodeName("lambda_4"), WithOutputKey("lambda_4")}
		err = g.AddLambdaNode("node4", lambda4, lambdaOpts4...)
		assert.NoError(t, err)

		// sub_graph → node3/node4 → END
		err = g.AddEdge("sub_graph", "node3")
		assert.NoError(t, err)
		err = g.AddEdge("sub_graph", "node4")
		assert.NoError(t, err)
		err = g.AddEdge("node3", END)
		assert.NoError(t, err)
		err = g.AddEdge("node4", END)
		assert.NoError(t, err)

		// ====== 编译并验证回调 ======
		// 创建回调处理器 c，用于捕获编译信息
		c := &cb{}
		// 编译选项：图名称"top_level"，回调处理器c
		opt := []GraphCompileOption{WithGraphCompileCallbacks(c), WithGraphName("top_level")}
		_, err = g.Compile(context.Background(), opt...)
		assert.NoError(t, err)

		// ====== 期望的编译信息 ======
		// 构建完整的期望GraphInfo，与实际回调捕获的信息进行对比
		expected := &GraphInfo{
			CompileOptions: opt,
			Nodes: map[string]GraphNodeInfo{
				"node1": {
					Component:        ComponentOfLambda,
					Instance:         lambda,
					GraphAddNodeOpts: lambdaOpts,
					InputType:        reflect.TypeOf(""),
					OutputType:       reflect.TypeOf(""),
					Name:             "lambda_1",
					InputKey:         "input_key",
				},
				"pass1": {
					Component:  ComponentOfPassthrough,
					InputType:  reflect.TypeOf(""),
					OutputType: reflect.TypeOf(""),
					Name:       "",
				},
				"pass2": {
					Component:  ComponentOfPassthrough,
					InputType:  reflect.TypeOf(""),
					OutputType: reflect.TypeOf(""),
					Name:       "",
				},
				"sub_graph": {
					Component:        ComponentOfGraph,
					Instance:         subGraph,
					GraphAddNodeOpts: subGraphOpts,
					InputType:        reflect.TypeOf(""),
					OutputType:       reflect.TypeOf(""),
					Name:             "",
					// 子图的GraphInfo：递归嵌套结构
					GraphInfo: &GraphInfo{
						CompileOptions: subGraphCompileOpts,
						Nodes: map[string]GraphNodeInfo{
							"sub_sub_1": {
								Component:        ComponentOfGraph,
								Instance:         subSubGraph,
								GraphAddNodeOpts: ssGraphOpts,
								InputType:        reflect.TypeOf(""),
								OutputType:       reflect.TypeOf(""),
								Name:             "",
								// 子子图的GraphInfo：最内层嵌套
								GraphInfo: &GraphInfo{
									CompileOptions: ssGraphCompileOpts,
									Nodes: map[string]GraphNodeInfo{
										"sub1": {
											Component:        ComponentOfLambda,
											Instance:         lambda2,
											GraphAddNodeOpts: lambdaOpts2,
											InputType:        reflect.TypeOf(""),
											OutputType:       reflect.TypeOf(""),
											Name:             "lambda_2",
										},
									},
									Edges: map[string][]string{
										START:  {"sub1"},
										"sub1": {END},
									},
									DataEdges: map[string][]string{
										START:  {"sub1"},
										"sub1": {END},
									},
									Branches:   map[string][]GraphBranch{},
									InputType:  reflect.TypeOf(""),
									OutputType: reflect.TypeOf(""),
								},
							},
						},
						Edges: map[string][]string{
							START:       {"sub_sub_1"},
							"sub_sub_1": {END},
						},
						DataEdges: map[string][]string{
							START:       {"sub_sub_1"},
							"sub_sub_1": {END},
						},
						Branches:   map[string][]GraphBranch{},
						InputType:  reflect.TypeOf(""),
						OutputType: reflect.TypeOf(""),
						Name:       "sub_graph",
					},
				},
				"node3": {
					Component:        ComponentOfLambda,
					Instance:         lambda3,
					GraphAddNodeOpts: lambdaOpts3,
					InputType:        reflect.TypeOf(""),
					OutputType:       reflect.TypeOf(""),
					Name:             "lambda_3",
					OutputKey:        "lambda_3",
				},
				"node4": {
					Component:        ComponentOfLambda,
					Instance:         lambda4,
					GraphAddNodeOpts: lambdaOpts4,
					InputType:        reflect.TypeOf(""),
					OutputType:       reflect.TypeOf(""),
					Name:             "lambda_4",
					OutputKey:        "lambda_4",
				},
			},
			Edges: map[string][]string{
				START:       {"node1"},
				"pass1":     {"sub_graph"},
				"pass2":     {"sub_graph"},
				"sub_graph": {"node3", "node4"},
				"node3":     {END},
				"node4":     {END},
			},
			DataEdges: map[string][]string{
				START:       {"node1"},
				"pass1":     {"sub_graph"},
				"pass2":     {"sub_graph"},
				"sub_graph": {"node3", "node4"},
				"node3":     {END},
				"node4":     {END},
			},
			Branches: map[string][]GraphBranch{
				"node1": {*branch},
			},
			InputType:  reflect.TypeOf(map[string]any{}),
			OutputType: reflect.TypeOf(map[string]any{}),
			Name:       "top_level",
		}

		// ====== 验证状态生成器 ======
		// 验证状态生成器函数存在且返回正确的类型
		stateFn := c.gInfo.GenStateFn
		assert.NotNil(t, stateFn)
		assert.Equal(t, &s{}, stateFn(context.Background()))

		// ====== 验证编译选项 ======
		// 验证NewGraphOptions字段
		assert.Equal(t, 1, len(c.gInfo.NewGraphOptions))
		c.gInfo.NewGraphOptions = nil

		// 清除GenStateFn以便后续比较
		c.gInfo.GenStateFn = nil

		// 比较编译选项：提取回调函数并验证它们是同一个实例
		actualCompileOptions := newGraphCompileOptions(c.gInfo.CompileOptions...)
		expectedCompileOptions := newGraphCompileOptions(expected.CompileOptions...)
		assert.Equal(t, len(expectedCompileOptions.callbacks), len(actualCompileOptions.callbacks))
		assert.Same(t, expectedCompileOptions.callbacks[0], actualCompileOptions.callbacks[0])
		// 清除不可比较的字段
		actualCompileOptions.callbacks = nil
		actualCompileOptions.origOpts = nil
		expectedCompileOptions.callbacks = nil
		expectedCompileOptions.origOpts = nil
		assert.Equal(t, expectedCompileOptions, actualCompileOptions)

		// 清除CompileOptions字段以便后续比较
		c.gInfo.CompileOptions = nil
		expected.CompileOptions = nil

		// ====== 验证分支信息 ======
		// 分支的endNodes和inputType需要单独比较（类型不同）
		assert.Equal(t, expected.Branches["node1"][0].endNodes, c.gInfo.Branches["node1"][0].endNodes)
		assert.Equal(t, expected.Branches["node1"][0].inputType, c.gInfo.Branches["node1"][0].inputType)

		// 清除分支信息以便后续DeepEqual比较
		expected.Branches["node1"] = []GraphBranch{}
		c.gInfo.Branches["node1"] = []GraphBranch{}

		// ====== 最终验证 ======
		// 完整比较GraphInfo结构（清除不可比较字段后）
		assert.Equal(t, expected, c.gInfo)
	})
}

func TestCheckAddEdge(t *testing.T) {
	g := NewGraph[string, string]()
	err := g.AddPassthroughNode("1")
	if err != nil {
		t.Fatal(err)
	}
	err = g.AddPassthroughNode("2")
	if err != nil {
		t.Fatal(err)
	}
	err = g.AddEdge("1", "2")
	if err != nil {
		t.Fatal(err)
	}
	err = g.AddEdge("1", "2")
	assert.ErrorContains(t, err, "control edge[1]-[2] have been added yet")
}

func TestStartWithEnd(t *testing.T) {
	g := NewGraph[string, string]()
	err := g.AddLambdaNode("1", InvokableLambda(func(ctx context.Context, input string) (output string, err error) {
		return input, nil
	}))
	if err != nil {
		t.Fatal(err)
	}
	err = g.AddBranch(START, NewGraphBranch(func(ctx context.Context, in string) (endNode string, err error) {
		return END, nil
	}, map[string]bool{"1": true, END: true}))
	if err != nil {
		t.Fatal(err)
	}
	r, err := g.Compile(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	sr, sw := schema.Pipe[string](1)
	sw.Send("test", nil)
	sw.Close()
	result, err := r.Transform(context.Background(), sr)
	if err != nil {
		t.Fatal(err)
	}
	for {
		chunk, err := result.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if chunk != "test" {
			t.Fatal("result is out of expect")
		}
	}
}

func TestToString(t *testing.T) {
	ps := runTypePregel.String()
	assert.Equal(t, "Pregel", ps)

	ds := runTypeDAG
	assert.Equal(t, "DAG", ds.String())
}
