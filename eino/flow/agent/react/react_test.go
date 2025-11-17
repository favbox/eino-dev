package react

import (
	"context"
	"fmt"
	"math/rand"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.uber.org/mock/gomock"

	"github.com/favbox/eino/components/model"
	"github.com/favbox/eino/components/tool"
	"github.com/favbox/eino/compose"
	"github.com/favbox/eino/flow/agent"
	mockModel "github.com/favbox/eino/internal/mock/components/model"
	"github.com/favbox/eino/schema"
	template "github.com/favbox/eino/utils/callbacks"
)

func TestReact(t *testing.T) {
	ctx := context.Background()

	fakeTool := &fakeToolGreetForTest{
		tarCount: 3,
	}

	info, err := fakeTool.Info(ctx)
	assert.NoError(t, err)

	ctrl := gomock.NewController(t)
	cm := mockModel.NewMockToolCallingChatModel(ctrl)

	times := 0
	cm.EXPECT().Generate(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
			times++
			if times <= 2 {
				info, _ := fakeTool.Info(ctx)

				return schema.AssistantMessage("hello max",
						[]schema.ToolCall{
							{
								ID: randStr(),
								Function: schema.FunctionCall{
									Name:      info.Name,
									Arguments: fmt.Sprintf(`{"name": "%s", "hh": "123"}`, randStr()),
								},
							},
						}),
					nil
			}

			return schema.AssistantMessage("bye", nil), nil
		}).AnyTimes()
	cm.EXPECT().WithTools(gomock.Any()).Return(nil).AnyTimes()

	a, err := NewAgent(ctx, &AgentConfig{
		ToolCallingModel: cm,
		ToolsConfig: compose.ToolsNodeConfig{
			Tools: []tool.BaseTool{fakeTool},
		},
		MessageModifier: func(ctx context.Context, input []*schema.Message) []*schema.Message {
			assert.Equal(t, len(input), times*2+1)
			return input
		},
		MaxStep: 40,
	})
	assert.Nil(t, err)

	out, err := a.Generate(ctx, []*schema.Message{
		{
			Role:    schema.User,
			Content: "Use greet tool to continuously say hello until you get a bye response, greet names in the following order: max, bob, alice, john, marry, joe, ken, lily, please start directly! please start directly! please start directly!",
		},
	}, agent.WithComposeOptions(compose.WithCallbacks(callbackForTest)))
	assert.Nil(t, err)

	if out != nil {
		t.Log(out.Content)
	}

	// test return directly
	times = 0
	a, err = NewAgent(ctx, &AgentConfig{
		ToolCallingModel: cm,
		ToolsConfig: compose.ToolsNodeConfig{
			Tools: []tool.BaseTool{fakeTool},
		},
		MessageModifier: func(ctx context.Context, input []*schema.Message) []*schema.Message {
			assert.Equal(t, len(input), times*2+1)
			return input
		},
		MaxStep:            40,
		ToolReturnDirectly: map[string]struct{}{info.Name: {}},
	})
	assert.Nil(t, err)

	out, err = a.Generate(ctx, []*schema.Message{
		{
			Role:    schema.User,
			Content: "Use greet tool to continuously say hello until you get a bye response, greet names in the following order: max, bob, alice, john, marry, joe, ken, lily, please start directly! please start directly! please start directly!",
		},
	}, agent.WithComposeOptions(compose.WithCallbacks(callbackForTest)))
	assert.Nil(t, err)

	if out != nil {
		t.Log(out.Content)
	}
}

func randStr() string {
	seeds := []rune("this is a seed")
	b := make([]rune, 8)
	for i := range b {
		b[i] = seeds[rand.Intn(len(seeds))]
	}
	return string(b)
}

var callbackForTest = BuildAgentCallback(&template.ModelCallbackHandler{}, &template.ToolCallbackHandler{})
