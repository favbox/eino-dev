package internal

import (
	"fmt"
	"reflect"
	"strings"
	"time"

	"eino/internal/generic"
)

var (
	concatFuncs = map[reflect.Type]any{
		generic.TypeOf[string]():        concatStrings,
		generic.TypeOf[int8]():          useLast[int8],
		generic.TypeOf[int16]():         useLast[int16],
		generic.TypeOf[int32]():         useLast[int32],
		generic.TypeOf[int64]():         useLast[int64],
		generic.TypeOf[int]():           useLast[int],
		generic.TypeOf[uint8]():         useLast[uint8],
		generic.TypeOf[uint16]():        useLast[uint16],
		generic.TypeOf[uint32]():        useLast[uint32],
		generic.TypeOf[uint64]():        useLast[uint64],
		generic.TypeOf[uint]():          useLast[uint],
		generic.TypeOf[bool]():          useLast[bool],
		generic.TypeOf[float32]():       useLast[float32],
		generic.TypeOf[float64]():       useLast[float64],
		generic.TypeOf[time.Time]():     useLast[time.Time],
		generic.TypeOf[time.Duration](): useLast[time.Duration],
	}
)

// 高效拼接字符串切片。
func concatStrings(ss []string) (string, error) {
	var n int
	for _, s := range ss {
		n += len(s)
	}

	var b strings.Builder
	b.Grow(n) // 预分配内存容量，避免多次扩容。
	for _, s := range ss {
		_, err := b.WriteString(s)
		if err != nil {
			return "", err
		}
	}

	return b.String(), nil
}

// useLast 返回切片的最后一个元素。
func useLast[T any](s []T) (T, error) {
	return s[len(s)-1], nil
}

// RegisterStreamChunkConcatFunc 注册类型 T 的流块合并函数。
func RegisterStreamChunkConcatFunc[T any](fn func([]T) (T, error)) {
	concatFuncs[generic.TypeOf[T]()] = fn
}

// GetConcatFunc 获取指定类型的流块合并函数。
// 返回通用的 reflect.Value 处理函数。
func GetConcatFunc(typ reflect.Type) func(reflect.Value) (reflect.Value, error) {
	if fn, ok := concatFuncs[typ]; ok {
		return func(a reflect.Value) (reflect.Value, error) {
			rvs := reflect.ValueOf(fn).Call([]reflect.Value{a})
			var err error
			if !rvs[1].IsNil() {
				err = rvs[1].Interface().(error)
			}
			return rvs[0], err
		}
	}

	return nil
}

// ConcatItems 合并多个元素为单个元素。
//
// 支持 map 和 slice 类型：
//   - map: 相同 key 的值收集到 slice，单值时直接存储
//   - slice: 优先使用注册函数，否则只允许一个非空元素。
//
// 调用方需确保 len(items) > 1。
func ConcatItems[T any](items []T) (T, error) {
	typ := generic.TypeOf[T]()
	v := reflect.ValueOf(items)

	var cv reflect.Value
	var err error

	// 根据类型选择合并策略：进入不同类型的合并流程
	if typ.Kind() == reflect.Map {
		cv, err = concatMaps(v) // map 类型合并
	} else {
		cv, err = concatSliceValue(v) // slice 类型合并
	}

	if err != nil {
		var t T
		return t, err
	}

	return cv.Interface().(T), nil
}

// concatMaps 合并多个 map，将相同 key 的值收集到 slice 中。
// 对于单个值的 key，直接存储原值而非 slice。
func concatMaps(ms reflect.Value) (reflect.Value, error) {
	typ := ms.Type().Elem()

	rms := reflect.MakeMap(reflect.MapOf(typ.Key(), generic.TypeOf[[]any]()))
	ret := reflect.MakeMap(typ)

	// 阶段 1：收集 - 将所有 map 中相同 key 的值收集到 []any 中
	n := ms.Len()
	for i := 0; i < n; i++ {
		m := ms.Index(i)

		for _, key := range m.MapKeys() {
			vals := rms.MapIndex(key)
			if !vals.IsValid() {
				var s []any
				vals = reflect.ValueOf(s)
			}

			val := m.MapIndex(key)
			vals = reflect.Append(vals, val)
			rms.SetMapIndex(key, vals)
		}
	}

	// 阶段 2-4：转换+合并+最终合并 - 收集处理到的值
	for _, key := range rms.MapKeys() {
		vals := rms.MapIndex(key)

		anyVals := vals.Interface().([]any)
		if len(anyVals) == 1 {
			ele := anyVals[0]
			if ele == nil { // we cannot SetMapIndex with nil because it will delete the key
				ret.SetMapIndex(key, reflect.Zero(typ.Elem()))
				continue
			}

			ret.SetMapIndex(key, reflect.ValueOf(ele))
			continue
		}

		// 阶段 2：转换 - 将 []any 转换为具体类型的切片
		v, err := toSliceValue(anyVals)
		if err != nil {
			return reflect.Value{}, err
		}

		var cv reflect.Value

		// 阶段 3：合并 - 根据元素类型选择合并策略
		if v.Type().Elem().Kind() == reflect.Map {
			cv, err = concatMaps(v) // 递归合并嵌套 map
		} else {
			cv, err = concatSliceValue(v) // 合并 slice 元素
		}

		if err != nil {
			return reflect.Value{}, err
		}

		// 阶段 4：最终合并 - 将合并结果设置到返回 map 中
		ret.SetMapIndex(key, cv)
	}

	return ret, nil
}

// concatSliceValue 合并切片元素为单个值。
// 优先使用注册的合并函数，否则处理空值和单值情况。
func concatSliceValue(val reflect.Value) (reflect.Value, error) {
	elmType := val.Type().Elem()

	if val.Len() == 1 {
		return val.Index(0), nil
	}

	// 尝试使用注册的合并函数进行合并
	f := GetConcatFunc(elmType)
	if f != nil {
		return f(val)
	}

	// 没有注册合并函数时的处理： 只允许一个非空元素
	var filtered reflect.Value
	for i := 0; i < val.Len(); i++ {
		oneVal := val.Index(i)
		if !oneVal.IsZero() {
			if filtered.IsValid() {
				return reflect.Value{}, fmt.Errorf("cannot concat multiple non-zero value of type %s", elmType)
			}

			filtered = oneVal
		}
	}
	if !filtered.IsValid() {
		filtered = reflect.New(elmType).Elem()
	}

	return filtered, nil
}

// toSliceValue 将 any 切片转换为指定类型的切片。
// 确保所有元素类型一致，否则返回错误。
func toSliceValue(vs []any) (reflect.Value, error) {
	typ := reflect.TypeOf(vs[0])

	ret := reflect.MakeSlice(reflect.SliceOf(typ), len(vs), len(vs))
	ret.Index(0).Set(reflect.ValueOf(vs[0]))

	for i := 1; i < len(vs); i++ {
		v := vs[i]
		vt := reflect.TypeOf(v)
		if typ != vt {
			return reflect.Value{}, fmt.Errorf("unexpected slice element type. Got %v, expected %v", typ, vt)
		}

		ret.Index(i).Set(reflect.ValueOf(v))
	}

	return ret, nil
}
