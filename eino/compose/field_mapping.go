package compose

import (
	"errors"
	"fmt"
	"reflect"
	"runtime/debug"
	"strings"

	"github.com/favbox/eino/internal/generic"
	"github.com/favbox/eino/internal/safe"
	"github.com/favbox/eino/schema"
)

// ====== 字段映射定义 ======

// FieldMapping 字段映射定义，描述源字段到目标字段的映射关系。
type FieldMapping struct {
	fromNodeKey     string
	from            string
	to              string
	customExtractor func(input any) (any, error)
}

// ====== 字段映射方法 ======

// String 返回字段映射的字符串表示
func (m *FieldMapping) String() string {
	var sb strings.Builder
	sb.WriteString("[from ")

	if m.from != "" {
		sb.WriteString(m.from)
		sb.WriteString("(field) of ")
	}

	sb.WriteString(m.fromNodeKey)

	if m.to != "" {
		sb.WriteString(" to ")
		sb.WriteString(m.to)
		sb.WriteString("(field)")
	}

	sb.WriteString("]")
	return sb.String()
}

// FromField 创建字段映射，将单个前驱字段映射到整个后继输入
// 独占映射：设置后无法再添加其他字段映射（后继输入已被完整映射）
func FromField(from string) *FieldMapping {
	return &FieldMapping{
		from: from,
	}
}

// ToField 创建字段映射，将整个前驱输出映射到单个后继字段
func ToField(to string, opts ...FieldMappingOption) *FieldMapping {
	fm := &FieldMapping{
		to: to,
	}
	for _, opt := range opts {
		opt(fm)
	}
	return fm
}

// MapFields 创建字段映射，将单个前驱字段映射到单个后继字段
func MapFields(from, to string) *FieldMapping {
	return &FieldMapping{
		from: from,
		to:   to,
	}
}

// FromNodeKey 获取源节点键
func (m *FieldMapping) FromNodeKey() string {
	return m.fromNodeKey
}

// FromPath 获取源字段路径
func (m *FieldMapping) FromPath() FieldPath {
	return splitFieldPath(m.from)
}

// ToPath 获取目标字段路径
func (m *FieldMapping) ToPath() FieldPath {
	return splitFieldPath(m.to)
}

// Equals 判断两个字段映射是否相等
func (m *FieldMapping) Equals(o *FieldMapping) bool {
	if m == nil {
		return o == nil
	}

	if o == nil || m.customExtractor != nil || o.customExtractor != nil {
		return false
	}

	return m.from == o.from && m.to == o.to && m.fromNodeKey == o.fromNodeKey
}

// ====== 字段路径抽象 ======

// FieldPath 嵌套字段访问路径抽象，支持结构体字段和 Map 键的嵌套访问
// 路径元素可以是结构体字段名或 Map 键
// 示例：
//   - []string{"user"}            // 顶层字段
//   - []string{"user", "name"}    // 嵌套结构体字段
//   - []string{"users", "admin"}  // Map 键访问
type FieldPath []string

// join 将路径元素连接为字符串
func (fp *FieldPath) join() string {
	return strings.Join(*fp, pathSeparator)
}

// splitFieldPath 拆分字段路径字符串
func splitFieldPath(path string) FieldPath {
	p := strings.Split(path, pathSeparator)
	if len(p) == 1 && p[0] == "" {
		return FieldPath{}
	}

	return p
}

// pathSeparator 路径分隔符，使用 Unit Separator（\x1F）
// 选择此字符是因为它极不可能出现在用户定义的字段名或 Map 键中
const pathSeparator = "\x1F"

// FromFieldPath 创建字段路径映射，将单个前驱字段路径映射到整个后继输入
// 独占映射：设置后无法再添加其他字段映射（后继输入已被完整映射）
// 示例：FromFieldPath(FieldPath{"user", "profile", "name"})
// 注意：字段路径元素不能包含内部路径分隔符（'\x1F'）
func FromFieldPath(fromFieldPath FieldPath) *FieldMapping {
	return &FieldMapping{
		from: fromFieldPath.join(),
	}
}

// ToFieldPath 创建字段路径映射，将整个前驱输出映射到单个后继字段路径
// 示例：ToFieldPath(FieldPath{"response", "data", "userName"})
// 注意：字段路径元素不能包含内部路径分隔符（'\x1F'）
func ToFieldPath(toFieldPath FieldPath, opts ...FieldMappingOption) *FieldMapping {
	fm := &FieldMapping{
		to: toFieldPath.join(),
	}
	for _, opt := range opts {
		opt(fm)
	}
	return fm
}

// MapFieldPaths 创建字段路径映射，将单个前驱字段路径映射到单个后继字段路径
// 示例：MapFieldPaths(FieldPath{"user", "profile", "name"}, FieldPath{"response", "userName"})
// 注意：字段路径元素不能包含内部路径分隔符（'\x1F'）
func MapFieldPaths(fromFieldPath, toFieldPath FieldPath) *FieldMapping {
	return &FieldMapping{
		from: fromFieldPath.join(),
		to:   toFieldPath.join(),
	}
}

// ====== 字段映射选项 ======

// FieldMappingOption 字段映射配置选项函数类型
type FieldMappingOption func(*FieldMapping)

// WithCustomExtractor 设置自定义提取器
// 提取器函数用于从字段映射的源中提取值
// 注意：此方式下，Eino 只能在请求时检查字段映射的有效性
func WithCustomExtractor(extractor func(input any) (any, error)) FieldMappingOption {
	return func(m *FieldMapping) {
		m.customExtractor = extractor
	}
}

// targetPath 获取目标路径
func (m *FieldMapping) targetPath() FieldPath {
	return splitFieldPath(m.to)
}

// ====== 转换器构建器 ======

// buildFieldMappingConverter 构建非流式字段映射转换器
func buildFieldMappingConverter[I any]() func(input any) (any, error) {
	return func(input any) (any, error) {
		in, ok := input.(map[string]any)
		if !ok {
			panic(newUnexpectedInputTypeErr(reflect.TypeOf(map[string]any{}), reflect.TypeOf(input)))
		}

		return convertTo(in, generic.TypeOf[I]()), nil
	}
}

// buildStreamFieldMappingConverter 构建流式字段映射转换器
func buildStreamFieldMappingConverter[I any]() func(input streamReader) streamReader {
	return func(input streamReader) streamReader {
		s, ok := unpackStreamReader[map[string]any](input)
		if !ok {
			panic("mappingStreamAssign incoming streamReader chunk type not map[string]any")
		}

		return packStreamReader(schema.StreamReaderWithConvert(s, func(v map[string]any) (I, error) {
			t := convertTo(v, generic.TypeOf[I]())
			return t.(I), nil
		}))
	}
}

// convertTo 将映射数据转换为目标类型实例
func convertTo(mappings map[string]any, typ reflect.Type) any {
	tValue := newInstanceByType(typ)
	if !tValue.CanAddr() {
		tValue = newInstanceByType(reflect.PointerTo(typ)).Elem()
	}

	for mapping, taken := range mappings {
		tValue = assignOne(tValue, taken, mapping)
	}

	return tValue.Interface()
}

// ====== 反射赋值引擎 ======

// assignOne 将值赋值到目标路径，支持嵌套结构体和 Map
func assignOne(destValue reflect.Value, taken any, to string) reflect.Value {
	if len(to) == 0 { // 直接赋值到输出
		destValue.Set(reflect.ValueOf(taken))
		return destValue
	}

	var (
		toPaths           = splitFieldPath(to)
		originalDestValue = destValue
		parentMap         reflect.Value
		parentKey         string
	)

	for {
		path := toPaths[0]
		toPaths = toPaths[1:]
		if len(toPaths) == 0 {
			toSet := reflect.ValueOf(taken)

			if destValue.Type() == reflect.TypeOf((*any)(nil)).Elem() {
				existingMap, ok := destValue.Interface().(map[string]any)
				if ok {
					destValue = reflect.ValueOf(existingMap)
				} else {
					mapValue := reflect.MakeMap(reflect.TypeOf(map[string]any{}))
					destValue.Set(mapValue)
					destValue = mapValue
				}
			}

			if destValue.Kind() == reflect.Map {
				key := reflect.ValueOf(path)
				keyType := destValue.Type().Key()
				if keyType != strType {
					key = key.Convert(keyType)
				}

				if !toSet.IsValid() {
					toSet = reflect.Zero(destValue.Type().Elem())
				}
				destValue.SetMapIndex(key, toSet)

				if parentMap.IsValid() {
					parentMap.SetMapIndex(reflect.ValueOf(parentKey), destValue)
				}

				return originalDestValue
			}

			ptrValue := destValue
			for destValue.Kind() == reflect.Ptr {
				destValue = destValue.Elem()
			}

			if !toSet.IsValid() {
				// just skip it, because this 'nil' is the zero value of the corresponding struct field
			} else {
				field := destValue.FieldByName(path)
				field.Set(toSet)
			}

			if parentMap.IsValid() {
				parentMap.SetMapIndex(reflect.ValueOf(parentKey), ptrValue)
			}

			return originalDestValue
		}

		if destValue.Type() == reflect.TypeOf((*any)(nil)).Elem() {
			existingMap, ok := destValue.Interface().(map[string]any)
			if ok {
				destValue = reflect.ValueOf(existingMap)
			} else {
				mapValue := reflect.MakeMap(reflect.TypeOf(map[string]any{}))
				destValue.Set(mapValue)
				destValue = mapValue
			}
		}

		if destValue.Kind() == reflect.Map {
			keyValue := reflect.ValueOf(path)
			valueValue := destValue.MapIndex(keyValue)
			if !valueValue.IsValid() {
				valueValue = newInstanceByType(destValue.Type().Elem())
				destValue.SetMapIndex(keyValue, valueValue)
			}

			if parentMap.IsValid() {
				parentMap.SetMapIndex(reflect.ValueOf(parentKey), destValue)
			}

			parentMap = destValue
			parentKey = path
			destValue = valueValue

			continue
		}

		ptrValue := destValue
		for destValue.Kind() == reflect.Ptr {
			destValue = destValue.Elem()
		}

		field := destValue.FieldByName(path)
		instantiateIfNeeded(field)

		if parentMap.IsValid() {
			parentMap.SetMapIndex(reflect.ValueOf(parentKey), ptrValue)
			parentMap = reflect.Value{}
			parentKey = ""
		}

		destValue = field
	}
}

// instantiateIfNeeded 按需实例化指针或 Map 字段
func instantiateIfNeeded(field reflect.Value) {
	if field.Kind() == reflect.Ptr {
		if field.IsNil() {
			field.Set(reflect.New(field.Type().Elem()))
		}
	} else if field.Kind() == reflect.Map {
		if field.IsNil() {
			field.Set(reflect.MakeMap(field.Type()))
		}
	}
}

// newInstanceByType 根据类型创建新实例
func newInstanceByType(typ reflect.Type) reflect.Value {
	switch typ.Kind() {
	case reflect.Map:
		return reflect.MakeMap(typ)
	case reflect.Slice, reflect.Array:
		slice := reflect.New(typ).Elem()
		slice.Set(reflect.MakeSlice(typ, 0, 0))
		return slice
	case reflect.Ptr:
		typ = typ.Elem()
		origin := reflect.New(typ)
		nested := newInstanceByType(typ)
		origin.Elem().Set(nested)

		return origin
	default:
		return reflect.New(typ).Elem()
	}
}

// checkAndExtractFromField 从结构体字段提取值
func checkAndExtractFromField(fromField string, input reflect.Value) (reflect.Value, error) {
	f := input.FieldByName(fromField)
	if !f.IsValid() {
		return reflect.Value{}, fmt.Errorf("field mapping from a struct field, but field not found. field=%v, inputType=%v", fromField, input.Type())
	}

	if !f.CanInterface() {
		return reflect.Value{}, fmt.Errorf("field mapping from a struct field, but field not exported. field= %v, inputType=%v", fromField, input.Type())
	}

	return f, nil
}

// ====== 辅助函数和验证机制 ======

type errMapKeyNotFound struct {
	mapKey string
}

// Error 返回错误信息
func (e *errMapKeyNotFound) Error() string {
	return fmt.Sprintf("key=%s", e.mapKey)
}

type errInterfaceNotValidForFieldMapping struct {
	interfaceType reflect.Type
	actualType    reflect.Type
}

// Error 返回错误信息
func (e *errInterfaceNotValidForFieldMapping) Error() string {
	return fmt.Sprintf("field mapping from an interface type, but actual type is not struct, struct ptr or map. InterfaceType= %v, ActualType= %v", e.interfaceType, e.actualType)
}

// checkAndExtractFromMapKey 从 Map 键提取值
func checkAndExtractFromMapKey(fromMapKey string, input reflect.Value) (reflect.Value, error) {
	key := reflect.ValueOf(fromMapKey)
	if input.Type().Key() != strType {
		key = key.Convert(input.Type().Key())
	}

	v := input.MapIndex(key)
	if !v.IsValid() {
		return reflect.Value{}, fmt.Errorf("field mapping from a map key, but key not found in input. %w", &errMapKeyNotFound{mapKey: fromMapKey})
	}

	return v, nil
}

// checkAndExtractFieldType 检查并提取字段类型，返回剩余路径
func checkAndExtractFieldType(paths []string, typ reflect.Type) (extracted reflect.Type, remainingPaths FieldPath, err error) {
	extracted = typ
	for i, field := range paths {
		for extracted.Kind() == reflect.Ptr {
			extracted = extracted.Elem()
		}

		if extracted.Kind() == reflect.Map {
			if !strType.ConvertibleTo(extracted.Key()) {
				return nil, nil, fmt.Errorf("type[%v] is not a map with string or string alias key", extracted)
			}

			extracted = extracted.Elem()
			continue
		}

		if extracted.Kind() == reflect.Struct {
			f, ok := extracted.FieldByName(field)
			if !ok {
				return nil, nil, fmt.Errorf("type[%v] has no field[%s]", extracted, field)
			}

			if !f.IsExported() {
				return nil, nil, fmt.Errorf("type[%v] has an unexported field[%s]", extracted.String(), field)
			}

			extracted = f.Type
			continue
		}

		if extracted.Kind() == reflect.Interface {
			return extracted, paths[i:], nil
		}

		return nil, nil, fmt.Errorf("intermediate type[%v] is not valid", extracted)
	}

	return extracted, nil, nil
}

var strType = reflect.TypeOf("")

// fieldMap 创建字段映射函数，将输入转换为映射数据
func fieldMap(mappings []*FieldMapping, allowMapKeyNotFound bool, uncheckedSourcePaths map[string]FieldPath) func(any) (map[string]any, error) {
	return func(input any) (result map[string]any, err error) {
		result = make(map[string]any, len(mappings))
		var inputValue reflect.Value
	loop:
		for _, mapping := range mappings {
			if mapping.customExtractor != nil {
				result[mapping.to], err = mapping.customExtractor(input)
				if err != nil {
					return nil, err
				}
				continue
			}

			if len(mapping.from) == 0 {
				result[mapping.to] = input
				continue
			}

			fromPath := splitFieldPath(mapping.from)

			if !inputValue.IsValid() {
				inputValue = reflect.ValueOf(input)
			}

			var (
				pathInputValue = inputValue
				pathInputType  = inputValue.Type()
				taken          = input
			)

			for i, path := range fromPath {
				for pathInputValue.Kind() == reflect.Ptr {
					pathInputValue = pathInputValue.Elem()
				}

				if !pathInputValue.IsValid() {
					return nil, fmt.Errorf("intermediate source value on path=%v is nil for type [%v]", fromPath[:i+1], pathInputType)
				}

				if pathInputValue.Kind() == reflect.Map && pathInputValue.IsNil() {
					return nil, fmt.Errorf("intermediate source value on path=%v is nil for map type [%v]", fromPath[:i+1], pathInputType)
				}

				taken, pathInputType, err = takeOne(pathInputValue, pathInputType, path)
				if err != nil {
					// we deferred check from Compile time to request time for interface types, so we won't panic here
					var interfaceNotValidErr *errInterfaceNotValidForFieldMapping
					if errors.As(err, &interfaceNotValidErr) {
						return nil, err
					}

					// map key not found can only be a request time error, so we won't panic here
					var mapKeyNotFoundErr *errMapKeyNotFound
					if errors.As(err, &mapKeyNotFoundErr) {
						if allowMapKeyNotFound {
							continue loop
						}
						return nil, err
					}

					if uncheckedSourcePaths != nil {
						uncheckedPath, ok := uncheckedSourcePaths[mapping.from]
						if ok && len(uncheckedPath) >= len(fromPath)-i {
							// the err happens on the mapping source path which is unchecked at request time, so we won't panic here
							return nil, err
						}
					}

					panic(safe.NewPanicErr(err, debug.Stack()))
				}

				if i < len(fromPath)-1 {
					pathInputValue = reflect.ValueOf(taken)
				}
			}

			result[mapping.to] = taken
		}

		return result, nil
	}
}

// streamFieldMap 创建流式字段映射函数
func streamFieldMap(mappings []*FieldMapping, uncheckedSourcePaths map[string]FieldPath) func(streamReader) streamReader {
	return func(input streamReader) streamReader {
		return packStreamReader(schema.StreamReaderWithConvert(input.toAnyStreamReader(), fieldMap(mappings, true, uncheckedSourcePaths)))
	}
}

// takeOne 从输入中提取单个值
func takeOne(inputValue reflect.Value, inputType reflect.Type, from string) (taken any, takenType reflect.Type, err error) {
	var f reflect.Value
	switch k := inputValue.Kind(); k {
	case reflect.Map:
		f, err = checkAndExtractFromMapKey(from, inputValue)
		if err != nil {
			return nil, nil, err
		}

		return f.Interface(), f.Type(), nil
	case reflect.Struct:
		f, err = checkAndExtractFromField(from, inputValue)
		if err != nil {
			return nil, nil, err
		}

		return f.Interface(), f.Type(), nil
	default:
		if inputType.Kind() == reflect.Interface {
			return nil, nil, &errInterfaceNotValidForFieldMapping{
				interfaceType: inputType,
				actualType:    inputValue.Type(),
			}
		}

		panic("when take one value from source, value not map or struct, and type not interface")
	}
}

// isFromAll 检查是否存在从全部输入映射
func isFromAll(mappings []*FieldMapping) bool {
	for _, mapping := range mappings {
		if len(mapping.from) == 0 && mapping.customExtractor == nil {
			return true
		}
	}
	return false
}

// fromFields 检查是否所有映射都从字段提取
func fromFields(mappings []*FieldMapping) bool {
	for _, mapping := range mappings {
		if len(mapping.from) == 0 || mapping.customExtractor != nil {
			return false
		}
	}

	return true
}

// isToAll 检查是否存在映射到全部输出
func isToAll(mappings []*FieldMapping) bool {
	for _, mapping := range mappings {
		if len(mapping.to) == 0 {
			return true
		}
	}
	return false
}

// validateStructOrMap 验证类型是否为结构体或 Map
func validateStructOrMap(t reflect.Type) bool {
	switch t.Kind() {
	case reflect.Map:
		return true
	case reflect.Ptr:
		t = t.Elem()
		fallthrough
	case reflect.Struct:
		return true
	default:
		return false
	}
}

// validateFieldMapping 验证字段映射的合法性和类型兼容性
// 返回：请求时类型检查器、未检查源路径映射、错误
func validateFieldMapping(predecessorType reflect.Type, successorType reflect.Type, mappings []*FieldMapping) (
	typeHandler *handlerPair,
	uncheckedSourcePath map[string]FieldPath,
	err error) {
	// 检查映射合法性
	if isFromAll(mappings) && isToAll(mappings) {
		panic(fmt.Errorf("invalid field mappings: from all fields to all, use common edge instead"))
	} else if !isToAll(mappings) && (!validateStructOrMap(successorType) && successorType != reflect.TypeOf((*any)(nil)).Elem()) {
		// 用户未提供具体结构体类型，运行时无法构造
		return nil, nil, fmt.Errorf("static check fail: successor input type should be struct or map, actual: %v", successorType)
	} else if fromFields(mappings) && !validateStructOrMap(predecessorType) {
		return nil, nil, fmt.Errorf("static check fail: predecessor output type should be struct or map, actual: %v", predecessorType)
	}

	var fieldCheckers map[string]handlerPair

	for i := range mappings {
		mapping := mappings[i]

		// 检查后继字段类型
		successorFieldType, successorRemaining, err := checkAndExtractFieldType(splitFieldPath(mapping.to), successorType)
		if err != nil {
			return nil, nil, fmt.Errorf("static check failed for mapping %s: %w", mapping, err)
		}

		if len(successorRemaining) > 0 {
			if successorFieldType == reflect.TypeOf((*any)(nil)).Elem() {
				continue // 运行时展开 'any' 为 'map[string]any'
			}
			return nil, nil, fmt.Errorf("static check failed for mapping %s, the successor has intermediate interface type %v", mapping, successorFieldType)
		}

		if mapping.customExtractor != nil {
			// 自定义提取器在请求时处理，跳过编译时检查
			continue
		}

		// 检查前驱字段类型
		predecessorFieldType, predecessorRemaining, err := checkAndExtractFieldType(splitFieldPath(mapping.from), predecessorType)
		if err != nil {
			return nil, nil, fmt.Errorf("static check failed for mapping %s: %w", mapping, err)
		}

		if len(predecessorRemaining) > 0 {
			if uncheckedSourcePath == nil {
				uncheckedSourcePath = make(map[string]FieldPath)
			}
			uncheckedSourcePath[mapping.from] = predecessorRemaining
		}

		// 创建运行时类型检查器
		checker := func(a any) (any, error) {
			trueInType := reflect.TypeOf(a)
			if trueInType == nil {
				switch successorFieldType.Kind() {
				case reflect.Map, reflect.Slice, reflect.Ptr, reflect.Interface:
				default:
					return nil, fmt.Errorf("runtime check failed for mapping %s, field[%v]-[%v] is absolutely not assignable", mapping, trueInType, successorFieldType)
				}
			} else {
				if !trueInType.AssignableTo(successorFieldType) {
					return nil, fmt.Errorf("runtime check failed for mapping %s, field[%v]-[%v] is absolutely not assignable", mapping, trueInType, successorFieldType)
				}
			}

			return a, nil
		}

		if len(predecessorRemaining) > 0 {
			// 源路径中存在接口类型，无法在编译时检查类型匹配，推迟到请求时
			if fieldCheckers == nil {
				fieldCheckers = make(map[string]handlerPair)
			}
			fieldCheckers[mapping.to] = handlerPair{
				invoke: checker,
				transform: func(input streamReader) streamReader {
					return packStreamReader(schema.StreamReaderWithConvert(input.toAnyStreamReader(), checker))
				},
			}
		} else {
			at := checkAssignable(predecessorFieldType, successorFieldType)
			if at == assignableTypeMustNot {
				return nil, nil, fmt.Errorf("static check failed for mapping %s, field[%v]-[%v] is absolutely not assignable", mapping, predecessorFieldType, successorFieldType)
			} else if at == assignableTypeMay {
				// 无法确定类型是否匹配，因为后继类型实现了前驱接口类型
				if fieldCheckers == nil {
					fieldCheckers = make(map[string]handlerPair)
				}
				fieldCheckers[mapping.to] = handlerPair{
					invoke: checker,
					transform: func(input streamReader) streamReader {
						return packStreamReader(schema.StreamReaderWithConvert(input.toAnyStreamReader(), checker))
					},
				}
			}
		}

	}

	if len(fieldCheckers) == 0 {
		return nil, uncheckedSourcePath, nil
	}

	// 创建聚合检查器
	checker := func(value map[string]any) (map[string]any, error) {
		var err error
		for k, v := range fieldCheckers {
			for mapping := range value {
				if mapping == k {
					value[mapping], err = v.invoke(value[mapping])
					if err != nil {
						return nil, err
					}
				}
			}
		}
		return value, nil
	}
	return &handlerPair{
		invoke: func(value any) (any, error) {
			return checker(value.(map[string]any))
		},
		transform: func(input streamReader) streamReader {
			s, ok := unpackStreamReader[map[string]any](input)
			if !ok {
				// impossible
				panic("field mapping edge stream value isn't map[string]any")
			}
			return packStreamReader(schema.StreamReaderWithConvert(s, checker))
		},
	}, uncheckedSourcePath, nil
}
