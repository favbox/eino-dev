package compose

import (
	"fmt"
	"reflect"

	"github.com/favbox/eino/internal"
)

/*
 * values_merge.go - 值合并系统实现
 *
 * 核心组件：
 *   - RegisterValuesMergeFunc: 合并函数注册机制，支持泛型类型
 *   - mergeValues: 核心值合并函数，支持多种数据类型
 *   - mergeOptions: 合并选项配置，控制合并行为
 *
 * 设计特点：
 *   - 自定义合并：允许为特定类型注册自定义合并逻辑
 *   - 类型安全：编译期和运行期的双重类型检查
 *   - 流式支持：专门优化流式数据的合并处理
 *   - 扩展能力：通过注册表模式支持无限扩展
 *
 * 与其他文件关系：
 *   - 与 internal 包协作管理全局合并函数注册表
 *   - 为图执行提供扇入模式下的数据合并能力
 *   - 与 streamReader 协作处理流式数据合并
 *   - 被 DAG 调度器调用进行多输入数据聚合
 *
 * 使用场景：
 *   - 扇入合并：多个节点输出汇聚为一个节点输入
 *   - 流式合并：多个流读取器合并为单个流
 *   - 自定义聚合：为特定业务类型定义合并规则
 *   - 类型安全检查：确保合并前后类型一致性
 */

// ====== 合并函数注册机制 ======

// RegisterValuesMergeFunc 注册合并函数 - 为扇入模式定义类型特定的合并逻辑
// 当多个节点输出汇聚到一个节点时，使用此函数定义如何合并该类型的数据
// 示例：为自定义结构体注册合并函数
//
//	RegisterValuesMergeFunc[*MyType](func(slice []T) (T, error) {
//	    // 自定义合并逻辑
//	    return mergedValue, nil
//	})
//
// 注意：对于已有默认合并函数的类型（如 map），无需重复注册，除非需要自定义逻辑
func RegisterValuesMergeFunc[T any](fn func([]T) (T, error)) {
	internal.RegisterValuesMergeFunc(fn)
}

// ====== 合并选项配置 ======

// mergeOptions 合并选项 - 控制合并过程的行为和特性
type mergeOptions struct {
	// streamMergeWithSourceEOF 带源名的流合并 - 是否在合并流时保留源信息
	streamMergeWithSourceEOF bool
	// names 流源名称列表 - 用于标记每个流的来源节点
	names []string
}

// ====== 核心合并逻辑 ======

// mergeValues 合并多个值 - 扇入模式下的数据聚合核心函数
// 合并策略：
//  1. 优先查找已注册的自定义合并函数
//  2. 对于流式数据，调用流的合并方法
//  3. 完整的类型检查确保安全性
//
// 要求：调用方确保输入数组长度大于 1
func mergeValues(vs []any, opts *mergeOptions) (any, error) {
	// 提取第一个值的类型信息作为合并基准
	v0 := reflect.ValueOf(vs[0])
	t0 := v0.Type()

	// 策略1：查找已注册的自定义合并函数
	if fn := internal.GetMergeFunc(t0); fn != nil {
		return fn(vs)
	}

	// 策略2：处理流式数据合并
	if s, ok := vs[0].(streamReader); ok {
		// 获取流的数据块类型
		t := s.getChunkType()

		// 验证数据块类型是否支持合并
		if internal.GetMergeFunc(t) == nil {
			return nil, fmt.Errorf("(mergeValues | stream type)"+
				" unsupported chunk type: %v", t)
		}

		// 验证所有流的类型一致性
		ss := make([]streamReader, len(vs)-1)
		for i := 0; i < len(ss); i++ {
			s_, ok_ := vs[i+1].(streamReader)
			if !ok_ {
				return nil, fmt.Errorf("(mergeStream) unexpected type. "+
					"expect: %v, got: %v", t0, reflect.TypeOf(vs[i]))
			}

			// 检查数据块类型匹配
			if st := s_.getChunkType(); st != t {
				return nil, fmt.Errorf("(mergeStream) chunk type mismatch. "+
					"expect: %v, got: %v", t, st)
			}

			ss[i] = s_
		}

		// 根据选项选择合并策略
		if opts != nil && opts.streamMergeWithSourceEOF {
			// 带源名的合并：保留每个流的来源信息
			ms := s.mergeWithNames(ss, opts.names)
			return ms, nil
		}

		// 标准合并：直接合并流数据
		ms := s.merge(ss)

		return ms, nil
	}

	// 未找到支持的合并策略
	return nil, fmt.Errorf("(mergeValues) unsupported type: %v", t0)
}
