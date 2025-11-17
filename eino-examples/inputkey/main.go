package main

import (
	"context"
	"io"

	"github.com/favbox/eino/components/prompt"
	"github.com/favbox/eino/compose"
	"github.com/favbox/eino/schema"
)

func main() {
	// 创建键值对图：输入map[string]any，输出map[string]any
	g := compose.NewGraph[map[string]any, map[string]any]()

	// 节点1: ChatTemplate节点，处理变量var1
	// 模板："{var1}" - 待替换的变量占位符
	// WithInputKey("1"): 从输入map的"1"键提取变量映射
	// WithOutputKey("1"): 将输出存储到输出map的"1"键下
	// 变量替换：{var1} -> "a"
	err := g.AddChatTemplateNode("1", prompt.FromMessages(schema.FString, schema.UserMessage("{var1}")), compose.WithOutputKey("1"), compose.WithInputKey("1"))
	if err != nil {
		panic(err)
	}

	// 节点2: ChatTemplate节点，处理变量var2
	// 模板："{var2}"
	// WithInputKey("2"): 从输入map的"2"键提取变量映射
	// WithOutputKey("2"): 将输出存储到输出map的"2"键下
	// 变量替换：{var2} -> "b"
	err = g.AddChatTemplateNode("2", prompt.FromMessages(schema.FString, schema.UserMessage("{var2}")), compose.WithOutputKey("2"), compose.WithInputKey("2"))
	if err != nil {
		panic(err)
	}

	// 节点3: ChatTemplate节点，处理变量var3
	// 模板："{var3}"
	// WithInputKey("3"): 从输入map的"3"键提取变量映射
	// WithOutputKey("3"): 将输出存储到输出map的"3"键下
	// 变量替换：{var3} -> "c"
	err = g.AddChatTemplateNode("3", prompt.FromMessages(schema.FString, schema.UserMessage("{var3}")), compose.WithOutputKey("3"), compose.WithInputKey("3"))
	if err != nil {
		panic(err)
	}

	// ====== 构建图的边 ======
	// 所有节点都从START开始，到END结束
	// 这意味着三个节点可以并行执行（同时从START接收输入）

	// 三个节点都从START开始
	err = g.AddEdge(compose.START, "1")
	if err != nil {
		panic(err)
	}

	err = g.AddEdge(compose.START, "2")
	if err != nil {
		panic(err)
	}

	err = g.AddEdge(compose.START, "3")
	if err != nil {
		panic(err)
	}

	// 三个节点都到END结束
	err = g.AddEdge("1", compose.END)
	if err != nil {
		panic(err)
	}

	err = g.AddEdge("2", compose.END)
	if err != nil {
		panic(err)
	}

	err = g.AddEdge("3", compose.END)
	if err != nil {
		panic(err)
	}

	// 编译图，设置为最多100步执行
	// WithMaxRunSteps(100): 防止无限循环，这里足够三个节点完成
	r, err := g.Compile(context.Background(), compose.WithMaxRunSteps(100))
	if err != nil {
		panic(err)
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
		panic(err)
	}
	// 验证：每个节点的输出都是正确的变量替换结果
	if result["1"].([]*schema.Message)[0].Content != "a" ||
		result["2"].([]*schema.Message)[0].Content != "b" ||
		result["3"].([]*schema.Message)[0].Content != "c" {
		panic("invoke different")
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
		panic(err)
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
			panic(err)
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
		panic("transform different")
	}
}
