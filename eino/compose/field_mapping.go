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

/*
 * field_mapping.go - 字段映射系统实现
 *
 * 核心组件：
 *   - FieldMapping: 字段映射定义结构体
 *   - FieldPath: 嵌套字段路径抽象
 *   - 工厂函数族: FromField/ToField/MapFields 等
 *   - 转换器构建器: buildFieldMappingConverter 等
 *   - 反射赋值引擎: convertTo/assignOne 等
 *   - 验证机制: validateFieldMapping 等
 *
 * 设计特点：
 *   - 精细字段级映射：支持结构体字段和 Map 键的精确映射
 *   - 嵌套路径支持：a.b.c 形式的多层嵌套访问
 *   - 反射动态赋值：运行时动态构造目标对象
 *   - 编译时 + 运行时双重验证：确保类型安全和映射正确性
 *   - 流式和非流式双模式：完整支持流式数据处理
 *
 * 与其他文件关系：
 *   - 为 Workflow 模式提供字段级数据映射能力
 *   - 补充 Chain/Graph 的粗粒度数据传递
 *   - 与 graph.go 的边处理机制协同工作
 *
 * 使用场景：
 *   - 结构体字段到字段的映射
 *   - Map 键值对的映射
 *   - 嵌套对象的字段映射
 *   - 异构数据结构的转换
 */

// ====== 字段映射定义 ======

// FieldMapping 字段映射定义 - 描述源字段到目标字段的映射关系
type FieldMapping struct {
	fromNodeKey string
	from        string
	to          string

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

// targetPath 获取目标路径
func (m *FieldMapping) targetPath() FieldPath {
	return splitFieldPath(m.to)
}

// ====== 字段路径抽象 ======

// FieldPath 字段路径 - 嵌套字段访问路径抽象
// 支持结构体字段和 Map 键的嵌套访问
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

// pathSeparator 路径分隔符 - 内部使用的特殊字符
// 使用 Unit Separator（\x1F），选择此字符是因为它极不可能出现在用户定义的字段名或 Map 键中
const pathSeparator = "\x1F"

// ====== 字段路径工厂函数 ======

// FromFieldPath 创建字段路径映射 - 将单个前驱字段路径映射到整个后继输入
// 独占映射：设置后无法再添加其他字段映射（后继输入已被完整映射）
//
// 示例：
//
//	// 将嵌套 'user.profile' 中的 'name' 字段映射到整个后继输入
//	FromFieldPath(FieldPath{"user", "profile", "name"})
//
// 注意：字段路径元素不能包含内部路径分隔符（'\x1F'）
func FromFieldPath(fromFieldPath FieldPath) *FieldMapping {
	return &FieldMapping{
		from: fromFieldPath.join(),
	}
}

// ToFieldPath 创建字段路径映射 - 将整个前驱输出映射到单个后继字段路径
//
// 示例：
//
//	// 将整个前驱输出映射到 response.data.userName
//	ToFieldPath(FieldPath{"response", "data", "userName"})
//
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

// MapFieldPaths 创建字段路径映射 - 将单个前驱字段路径映射到单个后继字段路径
//
// 示例：
//
//	// 将 user.profile.name 映射到 response.userName
//	MapFieldPaths(
//	    FieldPath{"user", "profile", "name"},
//	    FieldPath{"response", "userName"},
//	)
//
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

// ====== 转换器构建器 ======

func buildFieldMappingConverter[I any]() func(input any) (any, error) {
	return func(input any) (any, error) {
		in, ok := input.(map[string]any)
		if !ok {
			panic(newUnexpectedInputTypeErr(reflect.TypeOf(map[string]any{}), reflect.TypeOf(input)))
		}

		return convertTo(in, generic.TypeOf[I]()), nil
	}
}

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

func assignOne(destValue reflect.Value, taken any, to string) reflect.Value {
	if len(to) == 0 { // assign to output directly
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

type errMapKeyNotFound struct {
	mapKey string
}

func (e *errMapKeyNotFound) Error() string {
	return fmt.Sprintf("key=%s", e.mapKey)
}

type errInterfaceNotValidForFieldMapping struct {
	interfaceType reflect.Type
	actualType    reflect.Type
}

func (e *errInterfaceNotValidForFieldMapping) Error() string {
	return fmt.Sprintf("field mapping from an interface type, but actual type is not struct, struct ptr or map. InterfaceType= %v, ActualType= %v", e.interfaceType, e.actualType)
}

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

func streamFieldMap(mappings []*FieldMapping, uncheckedSourcePaths map[string]FieldPath) func(streamReader) streamReader {
	return func(input streamReader) streamReader {
		return packStreamReader(schema.StreamReaderWithConvert(input.toAnyStreamReader(), fieldMap(mappings, true, uncheckedSourcePaths)))
	}
}

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

func isFromAll(mappings []*FieldMapping) bool {
	for _, mapping := range mappings {
		if len(mapping.from) == 0 && mapping.customExtractor == nil {
			return true
		}
	}
	return false
}

func fromFields(mappings []*FieldMapping) bool {
	for _, mapping := range mappings {
		if len(mapping.from) == 0 || mapping.customExtractor != nil {
			return false
		}
	}

	return true
}

func isToAll(mappings []*FieldMapping) bool {
	for _, mapping := range mappings {
		if len(mapping.to) == 0 {
			return true
		}
	}
	return false
}

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

func validateFieldMapping(predecessorType reflect.Type, successorType reflect.Type, mappings []*FieldMapping) (
	// type checkers that are deferred to request-time
	typeHandler *handlerPair,
	// the remaining predecessor field paths that are not checked at compile time because of interface type found
	uncheckedSourcePath map[string]FieldPath,
	err error) {
	// check if mapping is legal
	if isFromAll(mappings) && isToAll(mappings) {
		// unreachable
		panic(fmt.Errorf("invalid field mappings: from all fields to all, use common edge instead"))
	} else if !isToAll(mappings) && (!validateStructOrMap(successorType) && successorType != reflect.TypeOf((*any)(nil)).Elem()) {
		// if user has not provided a specific struct type, graph cannot construct any struct in the runtime
		return nil, nil, fmt.Errorf("static check fail: successor input type should be struct or map, actual: %v", successorType)
	} else if fromFields(mappings) && !validateStructOrMap(predecessorType) {
		return nil, nil, fmt.Errorf("static check fail: predecessor output type should be struct or map, actual: %v", predecessorType)
	}

	var fieldCheckers map[string]handlerPair

	for i := range mappings {
		mapping := mappings[i]

		successorFieldType, successorRemaining, err := checkAndExtractFieldType(splitFieldPath(mapping.to), successorType)
		if err != nil {
			return nil, nil, fmt.Errorf("static check failed for mapping %s: %w", mapping, err)
		}

		if len(successorRemaining) > 0 {
			if successorFieldType == reflect.TypeOf((*any)(nil)).Elem() {
				continue // at request time expand this 'any' to 'map[string]any'
			}
			return nil, nil, fmt.Errorf("static check failed for mapping %s, the successor has intermediate interface type %v", mapping, successorFieldType)
		}

		if mapping.customExtractor != nil { // custom extractor applies to request-time data, so skip compile-time check
			continue
		}

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
			// can't check if types match at compile time, because there is interface type at some point along the source path. Defer to request time
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
				// can't decide if types match, because the successorFieldType implements predecessorFieldType, which is an interface type
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
