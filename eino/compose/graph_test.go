package compose

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

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
		for _, msg := range c.msgs {
			sw.Send(msg, nil)
		}
		sw.Close()
	}()
	return sr, nil
}

func TestSingleGraph(t *testing.T) {
	const (
		nodeOfModel  = "model"
		nodeOfPrompt = "prompt"
	)

	ctx := context.Background()
	g := NewGraph[map[string]any, *schema.Message]()

	pt := prompt.FromMessages(schema.FString,
		schema.UserMessage("{location}的天气怎么样?"),
	)

	err := g.AddChatTemplateNode("prompt", pt)
	assert.NoError(t, err)

	cm := &chatModel{msgs: []*schema.Message{
		schema.AssistantMessage("天气很好", nil),
	}}

	err = g.AddChatModelNode(nodeOfModel, cm, WithNodeName("MockChatModel"))
	assert.NoError(t, err)

	err = g.AddEdge(START, nodeOfPrompt)
	assert.NoError(t, err)
	err = g.AddEdge(nodeOfPrompt, nodeOfModel)
	assert.NoError(t, err)

	err = g.AddEdge(nodeOfModel, END)
	assert.NoError(t, err)

	start := time.Now()
	r, err := g.Compile(context.Background(), WithMaxRunSteps(10))
	assert.NoError(t, err)
	fmt.Println("图编译", time.Since(start))
	start = time.Now()

	in := map[string]any{"location": "suzhou"}
	out, err := r.Invoke(ctx, in)
	fmt.Println("图调用", time.Since(start))
	assert.NoError(t, err)
	fmt.Println(out)
}
