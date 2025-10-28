package generic

import "reflect"

// NewInstance 返回类型 T 的可用零值实例。
// 对 map、slice、pointer 等引用类型返回非 nil 实例。
// 对基础类型值类型返回零值。
//
// 示例：
//
//	NewInstance[int)()			// 0
//	NewInstance[*int]()			// &0 (非 nil)
//	NewInstance[[]int]()		// []int{} (非 nil)
//	NewInstance[map[int][int]]	// map[int]int{} (非 nil)
func NewInstance[T any]() T {
	typ := TypeOf[T]()

	switch typ.Kind() {
	case reflect.Map:
		return reflect.MakeMap(typ).Interface().(T)
	case reflect.Slice, reflect.Array:
		return reflect.MakeSlice(typ, 0, 0).Interface().(T)
	case reflect.Ptr:
		typ = typ.Elem()
		origin := reflect.New(typ)
		inst := origin

		for typ.Kind() == reflect.Ptr {
			typ = typ.Elem()
			inst = inst.Elem()
			inst.Set(reflect.New(typ))
		}

		return origin.Interface().(T)

	default:
		var t T
		return t
	}
}

// TypeOf 返回 T 的 reflect.Type。
//
// 示例:
//
//	TypeOf[int]()     // reflect.TypeOf(int)
//	TypeOf[*int]()    // reflect.TypeOf(*int)
func TypeOf[T any]() reflect.Type {
	return reflect.TypeOf((*T)(nil)).Elem()
}

// PtrOf 返回传入值 v 的指针。
// 用于需要获取值指针的场景，如配置结构体字段初始化。
//
// 典型场景:
//
//	config := Config{
//	    MaxConn: PtrOf(100),    // 设置配置项指针
//	    Timeout: PtrOf(30),     // 避免直接使用 &字面量
//	}
func PtrOf[T any](v T) *T {
	return &v
}

// Pair 表示一个包含两个值的键值对。
// F 表示第一个值的类型，S 表示第二个值的类型。
//
// 典型场景:
//
//	result := Pair[string, int]{
//	    First:  "success",
//	    Second: 200,
//	}
type Pair[F, S any] struct {
	First  F
	Second S
}

// Reverse 返回元素顺序反转的新切片。
func Reverse[S ~[]E, E any](s S) S {
	d := make(S, len(s))
	for i := 0; i < len(s); i++ {
		d[i] = s[len(s)-i-1]
	}

	return d
}

// CopyMap 创建 map 的完整副本。
// 新 map 与原 map 完全独立，修改不会相互影响。
func CopyMap[K comparable, V any](src map[K]V) map[K]V {
	dst := make(map[K]V, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}
