package schema

import (
	"testing"

	"github.com/eino-contrib/jsonschema"
	"github.com/smartystreets/goconvey/convey"
)

// 验证ParamsOneOf多种参数描述格式到JSON Schema的转换机制
func TestParamsOneOf_ToJSONSchema(t *testing.T) {
	convey.Convey("测试ParamsOneOf到JSON Schema的转换功能", t, func() {
		var (
			oneOf     ParamsOneOf // 被测试的参数描述对象
			converted any         // 转换后的结果
			err       error       // 转换过程中的错误
		)

		// 场景1：用户直接提供JSON Schema - 应直接返回原schema
		convey.Convey("用户直接提供JSON Schema时，直接使用用户提供的schema", func() {
			oneOf.jsonschema = &jsonschema.Schema{
				Type:        "string",
				Description: "this is the only argument",
			}
			converted, err = oneOf.ToJSONSchema()
			convey.So(err, convey.ShouldBeNil)
			convey.So(converted, convey.ShouldResemble, oneOf.jsonschema)
		})

		// 场景2：用户提供ParameterInfo映射 - 应转换为JSON Schema
		convey.Convey("用户提供ParameterInfo映射时，正确转换为JSON Schema", func() {
			oneOf.params = map[string]*ParameterInfo{
				"arg1": {
					Type:     String,
					Desc:     "this is the first argument",
					Required: true,
					Enum:     []string{"1", "2"},
				},
				"arg2": {
					Type: Object,
					Desc: "this is the second argument",
					SubParams: map[string]*ParameterInfo{
						"sub_arg1": {
							Type:     String,
							Desc:     "this is the sub argument",
							Required: true,
							Enum:     []string{"1", "2"},
						},
						"sub_arg2": {
							Type: String,
							Desc: "this is the sub argument 2",
						},
					},
					Required: true,
				},
				"arg3": {
					Type: Array,
					Desc: "this is the third argument",
					ElemInfo: &ParameterInfo{
						Type:     String,
						Desc:     "this is the element of the third argument",
						Required: true,
						Enum:     []string{"1", "2"},
					},
					Required: true,
				},
			}
			converted, err = oneOf.ToJSONSchema()
			convey.So(err, convey.ShouldBeNil)
		})
	})
}
