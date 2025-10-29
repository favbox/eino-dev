package serialization

import (
	"encoding/json"
	"fmt"
	"reflect"

	"github.com/bytedance/sonic"
)

// m - 名称到类型的映射，用于反序列化时的类型查找
var m = map[string]reflect.Type{}

// rm - 类型到名称的映射，用于序列化时的类型标识
var rm = map[reflect.Type]string{}

func init() {
	// 注册 Go 语言的所有基础类型到序列化系统
	// 确保基本数据类型能够正确序列化和反序列化
	_ = GenericRegister[int]("_eino_int")
	_ = GenericRegister[int8]("_eino_int8")
	_ = GenericRegister[int16]("_eino_int16")
	_ = GenericRegister[int32]("_eino_int32")
	_ = GenericRegister[int64]("_eino_int64")
	_ = GenericRegister[uint]("_eino_uint")
	_ = GenericRegister[uint8]("_eino_uint8")
	_ = GenericRegister[uint16]("_eino_uint16")
	_ = GenericRegister[uint32]("_eino_uint32")
	_ = GenericRegister[uint64]("_eino_uint64")
	_ = GenericRegister[float32]("_eino_float32")
	_ = GenericRegister[float64]("_eino_float64")
	_ = GenericRegister[complex64]("_eino_complex64")
	_ = GenericRegister[complex128]("_eino_complex128")
	_ = GenericRegister[uintptr]("_eino_uintptr")
	_ = GenericRegister[bool]("_eino_bool")
	_ = GenericRegister[string]("_eino_string")
	_ = GenericRegister[any]("_eino_any")
}

// GenericRegister - 泛型类型注册到序列化系统。
// 建立类型名称和反射类型的双向映射，确保序列化和反序列化的类型匹配
func GenericRegister[T any](key string) error {
	// 获取类型T的反射类型信息，去除指针层
	t := reflect.TypeOf((*T)(nil)).Elem()
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	// 检查键是否已被注册
	if nt, ok := m[key]; ok {
		return fmt.Errorf("key[%s] already registered to %s", key, nt.String())
	}

	// 检查类型是否已被注册
	if nk, ok := rm[t]; ok {
		return fmt.Errorf("type[%s] already registered to %s", t.String(), nk)
	}

	// 双向注册：名称→类型，类型→名称
	m[key] = t
	rm[t] = key
	return nil
}

// InternalSerializer - Eino 框架的内部序列化器
// 提供完整的序列化和反序列化能力，支持复杂类型和嵌套结构
type InternalSerializer struct{}

// Marshal - 序列化接口值为字节切片。
// 转换为内部格式后再通过 Sonic JSON 编码，保证类型信息的完整性。
func (i *InternalSerializer) Marshal(v interface{}) ([]byte, error) {
	// 转换为内部结构化格式
	is, err := internalMarshal(v, nil)
	if err != nil {
		return nil, err
	}

	// 使用 Sonic JSON 编码内部格式
	return sonic.Marshal(is)
}

// Unmarshal - 从字节切片反序列化到接口值。
// 支持类型转换、指针处理和复杂赋值逻辑。
func (i *InternalSerializer) Unmarshal(data []byte, v any) error {
	// 从字节流恢复类型信息
	val, err := unmarshal(data, reflect.TypeOf(v))
	if err != nil {
		return fmt.Errorf("failed to unmarshal: %w", err)
	}

	// 验证目标值必须是可设置的非空指针
	rv := reflect.ValueOf(v)
	if rv.Kind() != reflect.Ptr || rv.IsNil() {
		return fmt.Errorf("failed to unmarshal: value must be a non-nil pointer")
	}

	target := rv.Elem()
	if !target.CanSet() {
		return fmt.Errorf("failed to unmarshal: output value must be settable")
	}

	// 处理 nil 值情况
	if val == nil {
		target.Set(reflect.Zero(target.Type()))
		return nil
	}

	source := reflect.ValueOf(val)

	// 赋值逻辑：处理类型兼容性、指针解引用、类型转换
	var set func(target, source reflect.Value) bool
	set = func(target, source reflect.Value) bool {
		// 无效值处理
		if !source.IsValid() {
			target.Set(reflect.Zero(target.Type()))
			return true
		}

		// 直接赋值兼容
		if source.Type().AssignableTo(target.Type()) {
			target.Set(source)
			return true
		}

		// 目标是指针：自动创建并递归设置
		if target.Kind() == reflect.Ptr {
			if target.IsNil() {
				if !target.CanSet() {
					return false
				}
				target.Set(reflect.New(target.Type().Elem()))
			}
			return set(target.Elem(), source)
		}

		// 源是指针：解引用后递归设置
		if source.Kind() == reflect.Ptr {
			if source.IsNil() {
				target.Set(reflect.Zero(target.Type()))
				return true
			}
			return set(target, source.Elem())
		}

		// 类型转换兼容
		if source.Type().ConvertibleTo(target.Type()) {
			target.Set(source.Convert(target.Type()))
			return true
		}

		return false
	}

	if set(target, source) {
		return nil
	}

	return fmt.Errorf("failed to unmarshal: cannot assign %s to %s", reflect.TypeOf(val), target.Type())
}

func unmarshal(data []byte, t reflect.Type) (any, error) {
	is := &internalStruct{}
	err := sonic.Unmarshal(data, is)
	if err != nil {
		return nil, err
	}
	return internalUnmarshal(is, t)
}

// internalStruct - 序列化的中间数据结构
// 用于在序列化过程中保持类型信息和数据内容的完整性
type internalStruct struct {
	Type      *valueType      `json:",omitempty"` // 类型信息标识
	JSONValue json.RawMessage `json:",omitempty"` // JSON 原始数据（简单类型）

	// 复合类型存储
	MapValues   map[string]*internalStruct `json:",omitempty"` // map 或 struct 的字段值
	SliceValues []*internalStruct          `json:",omitempty"` // slice 或 array 的元素值
}

// valueType - 类型的元信息描述
// 记录指针层级和具体类型信息，支持精确的类型恢复
type valueType struct {
	PointerNum uint32 `json:",omitempty"` // 指针层级数量
	SimpleType string `json:",omitempty"` // 简单类型注册名称
	StructType string `json:",omitempty"` // 结构体类型注册名称

	// 复合类型参数
	MapKeyType     *valueType `json:",omitempty"` // map 的键类型
	MapValueType   *valueType `json:",omitempty"` // map 的值类型
	SliceValueType *valueType `json:",omitempty"` // slice 的元素类型
}

// extractType - 从反射类型提取类型元信息。
// 递归处理复合类型，构建完整的类型描述树。
func extractType(t reflect.Type) (*valueType, error) {
	ret := &valueType{}
	for t.Kind() == reflect.Ptr {
		ret.PointerNum += 1
		t = t.Elem()
	}
	var err error
	if t.Kind() == reflect.Map {
		ret.MapKeyType, err = extractType(t.Key())
		if err != nil {
			return nil, err
		}
		ret.MapValueType, err = extractType(t.Elem())
		if err != nil {
			return nil, err
		}
	} else if t.Kind() == reflect.Slice || t.Kind() == reflect.Array {
		ret.SliceValueType, err = extractType(t.Elem())
		if err != nil {
			return nil, err
		}
	} else {
		key, ok := rm[t]
		if !ok {
			return ret, fmt.Errorf("unknown type: %s", t.String())
		}
		ret.SimpleType = key
	}
	return ret, nil
}

// restoreType - 从类型元信息恢复反射类型。
// 根据 valueType 重构原始的反射类型。
func restoreType(vt *valueType) (reflect.Type, error) {
	if vt.SimpleType != "" {
		rt, ok := m[vt.SimpleType]
		if !ok {
			return nil, fmt.Errorf("unknown type: %s", vt.SimpleType)
		}
		return resolvePointerNum(vt.PointerNum, rt), nil
	}
	if vt.StructType != "" {
		rt, ok := m[vt.StructType]
		if !ok {
			return nil, fmt.Errorf("unknown type: %s", vt.StructType)
		}
		return resolvePointerNum(vt.PointerNum, rt), nil
	}
	if vt.MapKeyType != nil {
		rkt, err := restoreType(vt.MapKeyType)
		if err != nil {
			return nil, err
		}
		rvt, err := restoreType(vt.MapValueType)
		if err != nil {
			return nil, err
		}
		return resolvePointerNum(vt.PointerNum, reflect.MapOf(rkt, rvt)), nil
	}
	if vt.SliceValueType != nil {
		rt, err := restoreType(vt.SliceValueType)
		if err != nil {
			return nil, err
		}
		return resolvePointerNum(vt.PointerNum, reflect.SliceOf(rt)), nil
	}
	return nil, fmt.Errorf("empty value")
}

// internalMarshal - 核心序列化逻辑。
// 将任意值转换为 internalStruct 格式，保持类型信息。
func internalMarshal(v any, fieldType reflect.Type) (*internalStruct, error) {
	if v == nil ||
		(reflect.ValueOf(v).IsZero() && fieldType != nil && fieldType.Kind() != reflect.Interface) {
		return nil, nil
	}

	ret := &internalStruct{}
	rv := reflect.ValueOf(v)
	rt := rv.Type()
	typeUnspecific := fieldType == nil || fieldType.Kind() == reflect.Interface

	var pointerNum uint32
	for rt.Kind() == reflect.Ptr {
		pointerNum++
		if !rv.IsNil() {
			rv = rv.Elem()
			rt = rt.Elem()
			continue
		}
		for rt.Kind() == reflect.Ptr {
			rt = rt.Elem()
		}
		if typeUnspecific {
			// need type registered
			key, ok := rm[rt]
			if !ok {
				return nil, fmt.Errorf("unknown type: %v", rt)
			}
			ret.Type = &valueType{
				PointerNum: pointerNum,
				SimpleType: key,
			}
		}
		ret.JSONValue = json.RawMessage("null")
		return ret, nil
	}

	switch rt.Kind() {
	case reflect.Struct:
		if typeUnspecific {
			// need type registered
			key, ok := rm[rt]
			if !ok {
				return nil, fmt.Errorf("unknown type: %v", rt)
			}

			if checkMarshaler(rt) {
				ret.Type = &valueType{
					PointerNum: pointerNum,
					SimpleType: key,
				}
			} else {
				ret.Type = &valueType{
					PointerNum: pointerNum,
					StructType: key,
				}
			}
		}

		if checkMarshaler(rt) {
			jsonBytes, err := json.Marshal(rv.Interface())
			if err != nil {
				return nil, err
			}
			ret.JSONValue = jsonBytes
			return ret, nil
		}

		ret.MapValues = make(map[string]*internalStruct)

		for i := 0; i < rt.NumField(); i++ {
			field := rt.Field(i)
			// only handle exported fields
			if field.PkgPath == "" {
				k := field.Name
				v := rv.Field(i)

				internalValue, err := internalMarshal(v.Interface(), field.Type)
				if err != nil {
					return nil, err
				}

				ret.MapValues[k] = internalValue
			}
		}

		return ret, nil
	case reflect.Map:
		if typeUnspecific {
			var err error
			ret.Type = &valueType{
				PointerNum: pointerNum,
			}
			// map key type
			ret.Type.MapKeyType, err = extractType(rt.Key())
			if err != nil {
				return nil, err
			}

			// map value type
			ret.Type.MapValueType, err = extractType(rt.Elem())
			if err != nil {
				return nil, err
			}
		}

		ret.MapValues = make(map[string]*internalStruct)

		iter := rv.MapRange()
		for iter.Next() {
			k := iter.Key()
			v := iter.Value()

			internalValue, err := internalMarshal(v.Interface(), rt.Elem())
			if err != nil {
				return nil, err
			}

			keyStr, err := sonic.MarshalString(k.Interface())
			if err != nil {
				return nil, fmt.Errorf("marshaling map key[%v] fail: %v", k.Interface(), err)
			}
			ret.MapValues[keyStr] = internalValue
		}

		return ret, nil
	case reflect.Slice, reflect.Array:
		if typeUnspecific {
			var err error
			ret.Type = &valueType{PointerNum: pointerNum}
			ret.Type.SliceValueType, err = extractType(rt.Elem())
			if err != nil {
				return nil, err
			}
		}

		length := rv.Len()
		ret.SliceValues = make([]*internalStruct, length)

		for i := 0; i < length; i++ {
			internalValue, err := internalMarshal(rv.Index(i).Interface(), rt.Elem())
			if err != nil {
				return nil, err
			}
			ret.SliceValues[i] = internalValue
		}

		return ret, nil

	default:
		if typeUnspecific {
			key, ok := rm[rv.Type()]
			if !ok {
				return nil, fmt.Errorf("unknown type: %v", rt)
			}
			ret.Type = &valueType{
				PointerNum: pointerNum,
				SimpleType: key,
			}
		}

		jsonBytes, err := json.Marshal(rv.Interface())
		if err != nil {
			return nil, err
		}
		ret.JSONValue = jsonBytes
		return ret, nil
	}
}

// internalUnmarshal - 核心反序列化逻辑。
// 从 internalStruct 格式恢复原始数据和类型。
func internalUnmarshal(v *internalStruct, typ reflect.Type) (any, error) {
	if v == nil {
		return nil, nil
	}

	if v.Type == nil {
		// specific type
		if checkMarshaler(typ) {
			pv := reflect.New(typ)
			err := json.Unmarshal(v.JSONValue, pv.Interface())
			if err != nil {
				return nil, err
			}
			return pv.Elem().Interface(), nil
		}
		return internalSpecificTypeUnmarshal(v, typ)
	}

	if len(v.Type.SimpleType) != 0 {
		// based type
		t, ok := m[v.Type.SimpleType]
		if !ok {
			return nil, fmt.Errorf("unknown type key: %v", v.Type)
		}
		pResult := reflect.New(resolvePointerNum(v.Type.PointerNum, t))
		err := sonic.Unmarshal(v.JSONValue, pResult.Interface())
		if err != nil {
			return nil, fmt.Errorf("unmarshal type[%s] fail: %v, data: %s", t.String(), err, string(v.JSONValue))
		}
		return pResult.Elem().Interface(), nil
	}

	if len(v.Type.StructType) > 0 {
		// struct
		rt, ok := m[v.Type.StructType]
		if !ok {
			return nil, fmt.Errorf("unknown type key: %v", v.Type.StructType)
		}
		result, dResult := createValueFromType(resolvePointerNum(v.Type.PointerNum, rt))

		err := setStructFields(dResult, v.MapValues)
		if err != nil {
			return nil, err
		}

		return result.Interface(), nil
	}

	if v.Type.MapKeyType != nil {
		// map
		rkt, err := restoreType(v.Type.MapKeyType)
		if err != nil {
			return nil, err
		}
		rvt, err := restoreType(v.Type.MapValueType)
		if err != nil {
			return nil, err
		}

		result, dResult := createValueFromType(reflect.MapOf(rkt, rvt))
		err = setMapKVs(dResult, v.MapValues)
		if err != nil {
			return nil, err
		}
		return result.Interface(), nil
	}

	// slice
	rvt, err := restoreType(v.Type.SliceValueType)
	if err != nil {
		return nil, err
	}

	result, dResult := createValueFromType(reflect.SliceOf(rvt))
	err = setSliceElems(dResult, v.SliceValues)
	if err != nil {
		return nil, err
	}
	return result.Interface(), nil
}

func internalSpecificTypeUnmarshal(is *internalStruct, typ reflect.Type) (any, error) {
	_, dtyp := derefPointerNum(typ)
	result, dResult := createValueFromType(typ)

	if dtyp.Kind() == reflect.Struct {
		err := setStructFields(dResult, is.MapValues)
		if err != nil {
			return nil, err
		}
		return result.Interface(), nil
	} else if dtyp.Kind() == reflect.Map {
		err := setMapKVs(dResult, is.MapValues)
		if err != nil {
			return nil, err
		}
		return result.Interface(), nil
	} else if dtyp.Kind() == reflect.Array || dtyp.Kind() == reflect.Slice {
		err := setSliceElems(dResult, is.SliceValues)
		if err != nil {
			return nil, err
		}
		return result.Interface(), nil
	}
	// simple type
	v := reflect.New(typ)
	err := sonic.Unmarshal(is.JSONValue, v.Interface())
	if err != nil {
		return nil, fmt.Errorf("unmarshal type[%s] fail: %v", typ.String(), err)
	}
	return v.Elem().Interface(), nil
}

func setSliceElems(dResult reflect.Value, values []*internalStruct) error {
	t := dResult.Type()
	for _, internalValue := range values {
		value, err := internalUnmarshal(internalValue, t.Elem())
		if err != nil {
			return fmt.Errorf("unmarshal slice[%s] fail: %v", t.Elem(), err)
		}
		if value == nil {
			// empty value
			dResult.Set(reflect.Append(dResult, reflect.New(t.Elem()).Elem()))
		} else {
			dResult.Set(reflect.Append(dResult, reflect.ValueOf(value)))
		}
	}
	return nil
}

func setMapKVs(dResult reflect.Value, values map[string]*internalStruct) error {
	t := dResult.Type()
	for marshaledMapKey, internalValue := range values {
		prkv := reflect.New(t.Key())
		err := sonic.UnmarshalString(marshaledMapKey, prkv.Interface())
		if err != nil {
			return fmt.Errorf("unmarshal map key[%v] to type[%s] fail: %v", marshaledMapKey, t.Key(), err)
		}

		value, err := internalUnmarshal(internalValue, t.Elem())
		if err != nil {
			return fmt.Errorf("unmarshal map value fail: %v", err)
		}
		if value == nil {
			dResult.SetMapIndex(prkv.Elem(), reflect.New(t.Elem()).Elem())
		} else {
			dResult.SetMapIndex(prkv.Elem(), reflect.ValueOf(value))
		}
	}
	return nil
}

func setStructFields(dResult reflect.Value, values map[string]*internalStruct) error {
	t := dResult.Type()
	for k, internalValue := range values {
		sf, ok := t.FieldByName(k)
		if !ok {
			continue
		}
		value, err := internalUnmarshal(internalValue, sf.Type)
		if err != nil {
			return fmt.Errorf("unmarshal map field[%v] fail: %v", k, err)
		}
		err = setStructField(t, dResult, k, value)
		if err != nil {
			return err
		}
	}
	return nil
}

func setStructField(t reflect.Type, s reflect.Value, fieldName string, val any) error {
	field := s.FieldByName(fieldName)
	if !field.CanSet() {
		return fmt.Errorf("unmarshal map fail, can not set field %v", fieldName)
	}
	if val == nil {
		rft, ok := t.FieldByName(fieldName)
		if !ok {
			return fmt.Errorf("unmarshal map fail, cannot find field: %v", fieldName)
		}
		field.Set(reflect.New(rft.Type).Elem())
	} else {
		field.Set(reflect.ValueOf(val))
	}
	return nil
}

func resolvePointerNum(pointerNum uint32, t reflect.Type) reflect.Type {
	for i := uint32(0); i < pointerNum; i++ {
		t = reflect.PointerTo(t)
	}
	return t
}

func derefPointerNum(t reflect.Type) (uint32, reflect.Type) {
	var ptrCount uint32 = 0

	for t != nil && t.Kind() == reflect.Ptr {
		t = t.Elem()
		ptrCount++
	}

	return ptrCount, t
}

func createValueFromType(t reflect.Type) (value reflect.Value, derefValue reflect.Value) {
	value = reflect.New(t).Elem()

	derefValue = value
	for derefValue.Kind() == reflect.Ptr {
		if derefValue.IsNil() {
			derefValue.Set(reflect.New(derefValue.Type().Elem()))
		}
		derefValue = derefValue.Elem()
	}

	if derefValue.Kind() == reflect.Map && derefValue.IsNil() {
		derefValue.Set(reflect.MakeMap(derefValue.Type()))
	}

	if (derefValue.Kind() == reflect.Slice || derefValue.Kind() == reflect.Array) && derefValue.IsNil() {
		derefValue.Set(reflect.MakeSlice(derefValue.Type(), 0, 0))
	}

	return value, derefValue
}

var marshalerType = reflect.TypeOf((*json.Marshaler)(nil)).Elem()
var unmarshalerType = reflect.TypeOf((*json.Unmarshaler)(nil)).Elem()

func checkMarshaler(t reflect.Type) bool {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}

	if (t.Implements(marshalerType) || reflect.PointerTo(t).Implements(marshalerType)) &&
		(t.Implements(unmarshalerType) || reflect.PointerTo(t).Implements(unmarshalerType)) {
		return true
	}
	return false
}
