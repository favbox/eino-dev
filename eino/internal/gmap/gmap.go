package gmap

/*
 * gmap.go - Map 工具函数包
 *
 * 包概述：
 *   提供一组实用的 Map 操作函数，基于 Go 1.18+ 泛型特性实现
 *   所有函数都是泛型的，支持任意可比较的键类型和任意值类型
 *
 * 核心功能：
 *   1. Concat：合并多个 Map，取并集
 *   2. Map：对 Map 的键值对进行转换
 *   3. Values：提取 Map 的所有值
 *   4. Clone：浅拷贝 Map
 *   5. cloneWithoutNilCheck：内部辅助函数
 *
 * 设计特点：
 *   - 泛型设计：使用 Go 1.18+ 泛型，支持类型安全
 *   - 性能优化：针对空 Map 和单 Map 场景进行快速路径优化
 *   - 语义清晰：明确的函数行为和错误处理
 *   - 实用示例：每个函数都提供实际使用示例
 *
 * 与其他包关系：
 *   - 被 compose/values_merge.go 等包调用
 *   - 提供底层 Map 操作支持
 *
 * 使用场景：
 *   - 数据转换：对 Map 进行键值转换
 *   - 数据聚合：合并多个 Map 数据源
 *   - 数据提取：从 Map 中提取特定字段
 *   - 数据复制：安全地复制 Map 数据
 *
 * 注意事项：
 *   - 所有函数返回新 Map，不修改原 Map（浅拷贝）
 *   - Values 函数返回的值顺序是不确定的
 *   - Concat 函数在键冲突时，后面的值会覆盖前面的值
 */

// Concat 合并多个 Map 为一个新 Map - 取所有 Map 的并集
//
// 功能说明：
//
//	将多个相同类型的 Map 合并为一个新 Map，所有 Map 的键值对都会被包含
//	返回一个新 Map，原 Map 不会被修改
//
// 键冲突处理：
//   - 当多个 Map 中存在相同键时，后面的值会覆盖前面的值（DiscardOld 策略）
//   - 总是返回空 Map 而非 nil，即使结果是空集合
//
// 示例：
//
//	m := map[int]int{1: 1, 2: 2}
//	Concat(m, nil)             ⏩ map[int]int{1: 1, 2: 2}
//	Concat(m, map[int]{3: 3})  ⏩ map[int]int{1: 1, 2: 2, 3: 3}
//	Concat(m, map[int]{2: -1}) ⏩ map[int]int{1: 1, 2: -1} // "2:2" 被新的 "2:-1" 覆盖
//
// 💡 别名：Merge, Union, Combine
func Concat[K comparable, V any](ms ...map[K]V) map[K]V {
	// 快速路径1：没有传入任何 Map，返回空 Map
	if len(ms) == 0 {
		return make(map[K]V)
	}
	// 快速路径2：只有一个 Map，直接克隆返回
	if len(ms) == 1 {
		return cloneWithoutNilCheck(ms[0])
	}

	// 计算结果 Map 的容量：取所有输入 Map 长度的最大值
	var maxLen int
	for _, m := range ms {
		if len(m) > maxLen {
			maxLen = len(m)
		}
	}
	// 预分配 Map 容量以提高性能
	ret := make(map[K]V, maxLen)

	// 快速路径3：所有 Map 都为空，直接返回空 Map
	if maxLen == 0 {
		return ret
	}

	// 合并所有 Map：将每个 Map 的键值对复制到结果中
	for _, m := range ms {
		for k, v := range m {
			// 如果键已存在，后面的值会覆盖前面的值
			ret[k] = v
		}
	}
	return ret
}

// Map 对 Map 的每个键值对应用转换函数，返回转换后的新 Map
//
// 示例：
//
//	f := func(k, v int) (string, string) { return strconv.Itoa(k), strconv.Itoa(v) }
//	Map(map[int]int{1: 1}, f) ⏩ map[string]string{"1": "1"}
//	Map(map[int]int{}, f)     ⏩ map[string]string{}
func Map[K1, K2 comparable, V1, V2 any](m map[K1]V1, f func(K1, V1) (K2, V2)) map[K2]V2 {
	r := make(map[K2]V2, len(m))
	for k, v := range m {
		k2, v2 := f(k, v)
		r[k2] = v2
	}
	return r
}

// Values 提取 Map 的所有值 - 返回值切片
//
// 示例：
//
//	m := map[int]string{1: "1", 2: "2", 3: "3", 4: "4"}
//	Values(m) ⏩ []string{"1", "4", "2", "3"} //⚠️顺序不确定⚠️
func Values[K comparable, V any](m map[K]V) []V {
	r := make([]V, 0, len(m))
	for _, v := range m {
		r = append(r, v)
	}
	return r
}

// Clone 浅拷贝 Map - 返回 Map 的浅拷贝
//
// 示例：
//
//	Clone(map[int]int{1: 1, 2: 2}) ⏩ map[int]int{1: 1, 2: 2}
//	Clone(map[int]int{})           ⏩ map[int]int{}
//	Clone[int, int](nil)           ⏩ nil
//
// 💡 提示：键和值通过赋值复制，属于浅拷贝
// 💡 别名：Copy
func Clone[K comparable, V any, M ~map[K]V](m M) M {
	if m == nil {
		return nil
	}
	return cloneWithoutNilCheck(m)
}

func cloneWithoutNilCheck[K comparable, V any, M ~map[K]V](m M) M {
	r := make(M, len(m))
	for k, v := range m {
		r[k] = v
	}
	return r
}
