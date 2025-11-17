package adk

import (
	"context"
	"fmt"

	"github.com/bytedance/sonic"

	"github.com/favbox/eino/components/tool"
	"github.com/favbox/eino/schema"
)

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
