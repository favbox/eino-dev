package internal

import (
	"fmt"
	"reflect"

	"github.com/favbox/eino/internal/generic"
)

/*
 * merge.go - 多值合并器
 *
 * 核心功能：
 *   提供将多个相同类型的值合并为单个值的通用机制
 *   支持自定义合并函数和内置的 Map 合并逻辑
 *
 * 应用场景：
 *   - 流式数据处理：将多个流的结果合并为单值
 *   - 并行计算：聚合多个并行任务的结果
 *   - 分支合并：将多个分支的输出合并为统一输出
 *   - 聚合操作：执行 sum、avg、max 等聚合计算
 *
 * 设计特点：
 *   - 类型安全：通过反射确保类型匹配
 *   - 灵活性：支持自定义合并函数
 *   - 内置优化：对 Map 类型提供内置合并逻辑
 *   - 错误处理：详细的类型检查和错误信息
 *
 * 与其他包关系：
 *   - 被 compose/values_merge.go 调用用于字段映射合并
 *   - 与 generic 包协作提供类型信息
 *   - 支持并行执行的结果聚合
 *
 * 使用模式：
 *   1. 注册自定义合并函数（可选）
 *   2. 通过 GetMergeFunc 获取合并函数
 *   3. 调用合并函数聚合多个值
 *
 * 示例：
 *   // 注册自定义合并函数
 *   RegisterValuesMergeFunc(func([]int) (int, error) {
 *       return sum, nil
 *   })
 *
 *   // 获取合并函数并使用
 *   merge := GetMergeFunc(reflect.TypeOf(0))
 *   result, err := merge([]any{1, 2, 3})
 */

// ====== 合并函数注册表 ======

// mergeFuncs 合并函数注册表 - 全局合并函数缓存
// 映射表：类型 -> 合并函数
// 用于存储用户注册的针对特定类型的合并函数
var mergeFuncs = map[reflect.Type]any{}

// ====== 注册合并函数 ======

// RegisterValuesMergeFunc 注册指定类型的合并函数
// 用于向全局注册表添加自定义的合并逻辑
//
// 类型参数：
//   - T: 要注册合并函数的数据类型
//
// 参数：
//   - fn: 合并函数，接收 []T 类型的一组值，返回合并后的 T 类型值和可能的错误
//
// 使用示例：
//
//	RegisterValuesMergeFunc(func([]int) (int, error) {
//	    sum := 0
//	    for _, v := range vals {
//	        sum += v
//	    }
//	    return sum, nil
//	})
//
// 注意事项：
//   - 同一个类型只能注册一次，重复注册会覆盖
//   - 合并函数应该处理空切片的情况
//   - 返回的错误会被上层调用者处理
func RegisterValuesMergeFunc[T any](fn func([]T) (T, error)) {
	mergeFuncs[generic.TypeOf[T]()] = fn
}

// ====== 获取合并函数 ======

// GetMergeFunc 获取指定类型的合并函数
// 根据类型返回对应的合并函数，支持自定义函数和内置 Map 合并
//
// 参数：
//   - typ: 要获取合并函数的数据类型
//
// 返回：
//   - func([]any) (any, error): 合并函数，接收 []any 类型，返回 any 类型和错误
//
// 合并策略：
//  1. 首先检查是否有自定义注册的类型合并函数
//  2. 如果是 Map 类型，使用内置的 Map 合并逻辑
//  3. 如果都不匹配，返回 nil
//
// 错误处理：
//   - 类型不匹配错误：详细说明期望类型和实际类型
//   - Map 合并错误：重复键检测
//   - 合并函数错误：透传自定义合并函数的错误
func GetMergeFunc(typ reflect.Type) func([]any) (any, error) {
	// 检查是否有自定义注册的类型合并函数
	if fn, ok := mergeFuncs[typ]; ok {
		// 返回包装后的合并函数，处理类型转换
		return func(vs []any) (any, error) {
			// 创建指定类型的切片用于反射操作
			rvs := reflect.MakeSlice(reflect.SliceOf(typ), 0, len(vs))

			// 类型检查：确保所有值的类型匹配
			for _, v := range vs {
				if t := reflect.TypeOf(v); t != typ {
					return nil, fmt.Errorf(
						"(values merge) field type mismatch. expected: '%v', got: '%v'", typ, t)
				}
				// 使用反射将值添加到切片
				rvs = reflect.Append(rvs, reflect.ValueOf(v))
			}

			// 调用自定义合并函数（通过反射）
			rets := reflect.ValueOf(fn).Call([]reflect.Value{rvs})

			// 提取返回值和错误
			var err error
			if !rets[1].IsNil() {
				err = rets[1].Interface().(error)
			}
			return rets[0].Interface(), err
		}
	}

	// 处理 Map 类型的内置合并逻辑
	if typ.Kind() == reflect.Map {
		return func(vs []any) (any, error) {
			return mergeMap(typ, vs)
		}
	}

	// 没有找到匹配的合并函数
	return nil
}

// ====== Map 合并实现 ======

// mergeMap 合并 Map 类型的值 - 内置的 Map 合并逻辑
// 将多个相同类型的 Map 合并为一个 Map，重复键会报错
//
// 参数：
//   - typ: Map 的反射类型
//   - vs: 要合并的 Map 值切片
//
// 返回：
//   - any: 合并后的 Map 值
//   - error: 合并过程中的错误（类型不匹配、重复键）
//
// 合并规则：
//  1. 类型检查：所有 Map 必须具有相同的类型
//  2. 键冲突检测：如果发现重复键，返回错误
//  3. 顺序合并：按照输入顺序合并键值对
//
// 注意事项：
//   - Map 的值类型必须支持合并（如 slice、map 不能直接合并）
//   - 重复键会被拒绝，防止意外覆盖
//   - 返回的 Map 是新创建的，不修改原 Map
func mergeMap(typ reflect.Type, vs []any) (any, error) {
	// 创建新的 Map 实例用于存储合并结果
	merged := reflect.MakeMap(typ)

	// 遍历所有要合并的 Map
	for _, v := range vs {
		// 类型检查：确保所有 Map 类型一致
		if t := reflect.TypeOf(v); t != typ {
			return nil, fmt.Errorf(
				"(values merge map) field type mismatch. expected: '%v', got: '%v'", typ, t)
		}

		// 遍历当前 Map 的所有键值对
		iter := reflect.ValueOf(v).MapRange()
		for iter.Next() {
			key, val := iter.Key(), iter.Value()

			// 检查键是否已存在（重复键检测）
			if merged.MapIndex(key).IsValid() {
				return nil, fmt.Errorf("(values merge map) duplicated key ('%v') found", key.Interface())
			}

			// 将键值对添加到合并结果中
			merged.SetMapIndex(key, val)
		}
	}

	// 返回合并后的 Map（转换为 interface{} 类型）
	return merged.Interface(), nil
}
