package adk

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math/rand"
	"testing"

	"github.com/bytedance/sonic"
	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"

	"github.com/favbox/eino/components/model"
	"github.com/favbox/eino/components/tool"
	"github.com/favbox/eino/compose"
	mockModel "github.com/favbox/eino/internal/mock/components/model"
	"github.com/favbox/eino/schema"
)

func TestReact(t *testing.T) {
	// newReact 函数的基础测试
	t.Run("基础调用", func(t *testing.T) {
		ctx := context.Background()

		// 创建一个假工具用于测试
		fakeTool := &fakeToolForTest{
			tarCount: 3,
		}

		info, err := fakeTool.Info(ctx)
		assert.NoError(t, err)

		// 创建一个模拟聊天模型
		ctrl := gomock.NewController(t)
		cm := mockModel.NewMockToolCallingChatModel(ctrl)

		// 设置模拟模型的期望值
		times := 0
		cm.EXPECT().Generate(gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, input []Message, opts ...model.Option) (Message, error) {
				times++
				if times <= 2 {
					return schema.AssistantMessage("hello test",
							[]schema.ToolCall{
								{
									ID: randStrForTest(),
									Function: schema.FunctionCall{
										Name:      info.Name,
										Arguments: fmt.Sprintf(`{"name": "%s", "hh": "123"}`, randStrForTest()),
									},
								},
							}),
						nil
				}

				return schema.AssistantMessage("bye", nil), nil
			}).AnyTimes()
		cm.EXPECT().WithTools(gomock.Any()).Return(cm, nil).AnyTimes()

		// 创建一个 reactConfig
		config := &reactConfig{
			model: cm,
			toolsConfig: &compose.ToolsNodeConfig{
				Tools: []tool.BaseTool{fakeTool},
			},
			toolsReturnDirectly: map[string]bool{},
		}

		graph, err := newReact(ctx, config)
		assert.NoError(t, err)
		assert.NotNil(t, graph)

		compiled, err := graph.Compile(ctx)
		assert.NoError(t, err)
		assert.NotNil(t, compiled)

		// 测试用户信息
		result, err := compiled.Invoke(ctx, []Message{
			schema.UserMessage("使用测试工具说你好"),
		})
		assert.NoError(t, err)
		assert.NotNil(t, result)
	})

	// 测试 toolsReturnDirectly 调用 1 次就直接返回
	t.Run("ToolsReturnDirectly", func(t *testing.T) {
		ctx := context.Background()

		// Create a fake tool for testing
		fakeTool := &fakeToolForTest{
			tarCount: 3,
		}

		info, err := fakeTool.Info(ctx)
		assert.NoError(t, err)

		// Create a mock chat model
		ctrl := gomock.NewController(t)
		cm := mockModel.NewMockToolCallingChatModel(ctrl)

		// Set up expectations for the mock model
		times := 0
		cm.EXPECT().Generate(gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, input []Message, opts ...model.Option) (Message, error) {
				times++
				if times <= 2 {
					return schema.AssistantMessage("hello test",
							[]schema.ToolCall{
								{
									ID: randStrForTest(),
									Function: schema.FunctionCall{
										Name:      info.Name,
										Arguments: fmt.Sprintf(`{"name": "%s", "hh": "123"}`, randStrForTest()),
									},
								},
							}),
						nil
				}

				return schema.AssistantMessage("bye", nil), nil
			}).AnyTimes()
		cm.EXPECT().WithTools(gomock.Any()).Return(cm, nil).AnyTimes()

		// Create a reactConfig with toolsReturnDirectly
		config := &reactConfig{
			model: cm,
			toolsConfig: &compose.ToolsNodeConfig{
				Tools: []tool.BaseTool{fakeTool},
			},
			toolsReturnDirectly: map[string]bool{info.Name: true},
		}

		graph, err := newReact(ctx, config)
		assert.NoError(t, err)
		assert.NotNil(t, graph)

		compiled, err := graph.Compile(ctx)
		assert.NoError(t, err)
		assert.NotNil(t, compiled)

		// Test with a user message when tool returns directly
		result, err := compiled.Invoke(ctx, []Message{
			{
				Role:    schema.User,
				Content: "Use the test tool to say hello",
			},
		})
		assert.NoError(t, err)
		assert.NotNil(t, result)

		assert.Equal(t, result.Role, schema.Tool)
	})

	// 测试流式输出功能，返回最后一次结果
	t.Run("Stream", func(t *testing.T) {
		ctx := context.Background()

		// Create a fake tool for testing
		fakeTool := &fakeToolForTest{
			tarCount: 3,
		}

		fakeStreamTool := &fakeStreamToolForTest{
			tarCount: 3,
		}

		// Create a mock chat model
		ctrl := gomock.NewController(t)
		cm := mockModel.NewMockToolCallingChatModel(ctrl)

		// Set up expectations for the mock model
		times := 0
		cm.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, input []Message, opts ...model.Option) (
				MessageStream, error) {
				sr, sw := schema.Pipe[Message](1)
				defer sw.Close()

				info, _ := fakeTool.Info(ctx)
				streamInfo, _ := fakeStreamTool.Info(ctx)

				times++
				if times <= 1 {
					sw.Send(schema.AssistantMessage("hello test",
						[]schema.ToolCall{
							{
								ID: randStrForTest(),
								Function: schema.FunctionCall{
									Name:      info.Name,
									Arguments: fmt.Sprintf(`{"name": "%s", "hh": "tool"}`, randStrForTest()),
								},
							},
						}),
						nil)
					return sr, nil
				} else if times == 2 {
					sw.Send(schema.AssistantMessage("hello stream",
						[]schema.ToolCall{
							{
								ID: randStrForTest(),
								Function: schema.FunctionCall{
									Name:      streamInfo.Name,
									Arguments: fmt.Sprintf(`{"name": "%s", "hh": "stream tool"}`, randStrForTest()),
								},
							},
						}),
						nil)
					return sr, nil
				}

				sw.Send(schema.AssistantMessage("bye", nil), nil)
				return sr, nil
			}).AnyTimes()
		cm.EXPECT().WithTools(gomock.Any()).Return(cm, nil).AnyTimes()

		// Create a reactConfig
		config := &reactConfig{
			model: cm,
			toolsConfig: &compose.ToolsNodeConfig{
				Tools: []tool.BaseTool{fakeTool, fakeStreamTool},
			},
			toolsReturnDirectly: map[string]bool{},
		}

		graph, err := newReact(ctx, config)
		assert.NoError(t, err)
		assert.NotNil(t, graph)

		compiled, err := graph.Compile(ctx)
		assert.NoError(t, err)
		assert.NotNil(t, compiled)

		// Test streaming with a user message
		outStream, err := compiled.Stream(ctx, []Message{
			{
				Role:    schema.User,
				Content: "Use the test tool to say hello",
			},
		})
		assert.NoError(t, err)
		assert.NotNil(t, outStream)

		defer outStream.Close()

		msgs := make([]Message, 0)
		for {
			msg, err_ := outStream.Recv()
			if err_ != nil {
				if errors.Is(err_, io.EOF) {
					break
				}
				t.Fatal(err_)
			}

			msgs = append(msgs, msg)
		}

		assert.NotEmpty(t, msgs)
	})

	// 测试流式+配置直接返回的工具，返回第 2 次流式结果
	t.Run("StreamWithToolsReturnDirectly", func(t *testing.T) {
		ctx := context.Background()

		// Create a fake tool for testing
		fakeTool := &fakeToolForTest{
			tarCount: 3,
		}

		fakeStreamTool := &fakeStreamToolForTest{
			tarCount: 3,
		}

		// Create a mock chat model
		ctrl := gomock.NewController(t)
		cm := mockModel.NewMockToolCallingChatModel(ctrl)

		// Set up expectations for the mock model
		times := 0
		cm.EXPECT().Stream(gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, input []Message, opts ...model.Option) (
				MessageStream, error) {
				sr, sw := schema.Pipe[Message](1)
				defer sw.Close()

				info, _ := fakeTool.Info(ctx)
				streamInfo, _ := fakeStreamTool.Info(ctx)

				times++
				if times <= 1 {
					sw.Send(schema.AssistantMessage("hello test",
						[]schema.ToolCall{
							{
								ID: randStrForTest(),
								Function: schema.FunctionCall{
									Name:      info.Name,
									Arguments: fmt.Sprintf(`{"name": "%s", "hh": "tool"}`, randStrForTest()),
								},
							},
						}),
						nil)
					return sr, nil
				} else if times == 2 {
					sw.Send(schema.AssistantMessage("hello stream",
						[]schema.ToolCall{
							{
								ID: randStrForTest(),
								Function: schema.FunctionCall{
									Name:      streamInfo.Name,
									Arguments: fmt.Sprintf(`{"name": "%s", "hh": "stream tool"}`, randStrForTest()),
								},
							},
						}),
						nil)
					return sr, nil
				}

				sw.Send(schema.AssistantMessage("bye", nil), nil)
				return sr, nil
			}).AnyTimes()
		cm.EXPECT().WithTools(gomock.Any()).Return(cm, nil).AnyTimes()

		streamInfo, err := fakeStreamTool.Info(ctx)
		assert.NoError(t, err)

		// Create a reactConfig with toolsReturnDirectly
		config := &reactConfig{
			model: cm,
			toolsConfig: &compose.ToolsNodeConfig{
				Tools: []tool.BaseTool{fakeTool, fakeStreamTool},
			},
			toolsReturnDirectly: map[string]bool{streamInfo.Name: true},
		}

		graph, err := newReact(ctx, config)
		assert.NoError(t, err)
		assert.NotNil(t, graph)

		compiled, err := graph.Compile(ctx)
		assert.NoError(t, err)
		assert.NotNil(t, compiled)

		// Reset times counter
		times = 0

		// Test streaming with a user message when tool returns directly
		outStream, err := compiled.Stream(ctx, []Message{
			{
				Role:    schema.User,
				Content: "Use the test tool to say hello",
			},
		})
		assert.NoError(t, err)
		assert.NotNil(t, outStream)

		msgs := make([]Message, 0)
		for {
			msg, err_ := outStream.Recv()
			if err_ != nil {
				if errors.Is(err_, io.EOF) {
					break
				}
				t.Fatal(err)
			}

			assert.Equal(t, msg.Role, schema.Tool)

			msgs = append(msgs, msg)
		}

		outStream.Close()

		assert.NotEmpty(t, msgs)
	})

	t.Run("MaxIterations", func(t *testing.T) {
		ctx := context.Background()

		// Create a fake tool for testing
		fakeTool := &fakeToolForTest{
			tarCount: 3,
		}

		info, err := fakeTool.Info(ctx)
		assert.NoError(t, err)

		// Create a mock chat model
		ctrl := gomock.NewController(t)
		cm := mockModel.NewMockToolCallingChatModel(ctrl)

		// Set up expectations for the mock model
		times := 0
		cm.EXPECT().Generate(gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(ctx context.Context, input []Message, opts ...model.Option) (Message, error) {
				times++
				if times <= 5 {
					return schema.AssistantMessage("hello test",
							[]schema.ToolCall{
								{
									ID: randStrForTest(),
									Function: schema.FunctionCall{
										Name:      info.Name,
										Arguments: fmt.Sprintf(`{"name": "%s", "hh": "123"}`, randStrForTest()),
									},
								},
							}),
						nil
				}

				return schema.AssistantMessage("bye", nil), nil
			}).AnyTimes()
		cm.EXPECT().WithTools(gomock.Any()).Return(cm, nil).AnyTimes()

		// don't exceed max iterations
		config := &reactConfig{
			model: cm,
			toolsConfig: &compose.ToolsNodeConfig{
				Tools: []tool.BaseTool{fakeTool},
			},
			toolsReturnDirectly: map[string]bool{},
			maxIterations:       6,
		}

		graph, err := newReact(ctx, config)
		assert.NoError(t, err)
		assert.NotNil(t, graph)

		compiled, err := graph.Compile(ctx)
		assert.NoError(t, err)
		assert.NotNil(t, compiled)

		// Test with a user message
		result, err := compiled.Invoke(ctx, []Message{
			{
				Role:    schema.User,
				Content: "Use the test tool to say hello",
			},
		})
		assert.NoError(t, err)
		assert.Equal(t, result.Content, "bye")

		// reset chat model times counter
		times = 0
		// exceed max iterations
		config = &reactConfig{
			model: cm,
			toolsConfig: &compose.ToolsNodeConfig{
				Tools: []tool.BaseTool{fakeTool},
			},
			toolsReturnDirectly: map[string]bool{},
			maxIterations:       5,
		}

		graph, err = newReact(ctx, config)
		assert.NoError(t, err)
		assert.NotNil(t, graph)

		compiled, err = graph.Compile(ctx)
		assert.NoError(t, err)
		assert.NotNil(t, compiled)

		// Test with a user message
		result, err = compiled.Invoke(ctx, []Message{
			{
				Role:    schema.User,
				Content: "Use the test tool to say hello",
			},
		})
		assert.Error(t, err)
		t.Logf("actual error: %v", err.Error())
		assert.ErrorIs(t, err, ErrExceedMaxIterations)

		assert.Contains(t, err.Error(), ErrExceedMaxIterations.Error())
	})
}

func randStrForTest() string {
	seeds := []rune("test seed")
	b := make([]rune, 8)
	for i := range b {
		b[i] = seeds[rand.Intn(len(seeds))]
	}
	return string(b)
}

type fakeToolForTest struct {
	tarCount int
	curCount int
}

func (t *fakeToolForTest) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "test_tool",
		Desc: "用于单元测试的假工具",
		ParamsOneOf: schema.NewParamsOneOfByParams(
			map[string]*schema.ParameterInfo{
				"name": {
					Desc:     "用于测试的用户名",
					Required: true,
					Type:     schema.String,
				},
			}),
	}, nil
}

func (t *fakeToolForTest) InvokableRun(_ context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	p := &fakeToolInputForTest{}
	err := sonic.UnmarshalString(argumentsInJSON, p)
	if err != nil {
		return "", err
	}

	if t.curCount >= t.tarCount {
		return `{"say": "bye"}`, nil
	}

	t.curCount++
	return fmt.Sprintf(`{"say": "hello %v"}`, p.Name), nil
}

type fakeToolInputForTest struct {
	Name string `json:"name"`
}

type fakeStreamToolForTest struct {
	tarCount int
	curCount int
}

func (t *fakeStreamToolForTest) Info(_ context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "test_stream_tool",
		Desc: "test stream tool for unit testing",
		ParamsOneOf: schema.NewParamsOneOfByParams(
			map[string]*schema.ParameterInfo{
				"name": {
					Desc:     "user name for testing",
					Required: true,
					Type:     schema.String,
				},
			}),
	}, nil
}

func (t *fakeStreamToolForTest) StreamableRun(_ context.Context, argumentsInJSON string, _ ...tool.Option) (
	*schema.StreamReader[string], error) {
	p := &fakeToolInputForTest{}
	err := sonic.UnmarshalString(argumentsInJSON, p)
	if err != nil {
		return nil, err
	}

	if t.curCount >= t.tarCount {
		s := schema.StreamReaderFromArray([]string{`{"say": "bye"}`})
		return s, nil
	}
	t.curCount++
	s := schema.StreamReaderFromArray([]string{fmt.Sprintf(`{"say": "hello %v"}`, p.Name)})
	return s, nil
}
