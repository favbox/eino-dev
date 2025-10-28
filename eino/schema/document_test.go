package schema

import (
	"testing"

	"github.com/smartystreets/goconvey/convey"
)

// TestDocument - 验证文档扩展方法的链式调用和元数据管理功能
func TestDocument(t *testing.T) {
	// 使用 convey 框架组织测试结构，提供清晰的测试描述
	convey.Convey("验证文档元数据管理的完整功能", t, func() {
		// 准备测试数据：各种类型的元数据，覆盖所有扩展方法支持的类型
		var (
			subIndexes = []string{"hello", "bye"}      // 子索引列表，用于文档分片
			score      = 1.1                           // 文档相关性评分，用于搜索排序
			extraInfo  = "asd"                         // 额外信息字符串，用于自定义元数据
			dslInfo    = map[string]any{"hello": true} // DSL查询映射，结构化查询条件
			vector     = []float64{1.1, 2.2}           // 密集向量，用于语义相似度计算
		)

		// 创建基础文档对象，MetaData 初始化为 nil 测试零值安全性
		d := &Document{
			ID:       "asd",
			Content:  "qwe",
			MetaData: nil,
		}

		// 链式调用设置各种元数据，验证 Fluent Interface 的流畅性
		d.WithSubIndexes(subIndexes).
			WithDenseVector(vector).
			WithScore(score).
			WithExtraInfo(extraInfo).
			WithDSLInfo(dslInfo)

		// 验证所有元数据设置的正确性，确保链式调用成功
		convey.So(d.SubIndexes(), convey.ShouldEqual, subIndexes)
		convey.So(d.Score(), convey.ShouldEqual, score)
		convey.So(d.ExtraInfo(), convey.ShouldEqual, extraInfo)
		convey.So(d.DSLInfo(), convey.ShouldEqual, dslInfo)
		convey.So(d.DenseVector(), convey.ShouldEqual, vector)
	})
}
