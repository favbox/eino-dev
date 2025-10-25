package generic

import (
	"reflect"
	"regexp"
	"runtime"
	"strings"
)

var (
	regOfAnonymousFunc = regexp.MustCompile(`^func[0-9]+`)
	regOfNumber        = regexp.MustCompile(`^\d+$`)
)

// ParseTypeName 返回值的类型名称。
// 自动解引用指针类型，获取实际类型名称。
// 支持函数类型，区分命名函数、方法和匿名函数。
//
// 示例:
//
//	ParseTypeName(reflect.ValueOf(&User{}))          // "User"
//	ParseTypeName(reflect.ValueOf(ParseTypeName))   // "utils.ParseTypeName"
//	ParseTypeName(reflect.ValueOf(user.GetUser))    // "(*User).GetUser"
//	ParseTypeName(reflect.ValueOf(anonymousFunc))   // "TestParseTypeName.func6.1"
func ParseTypeName(val reflect.Value) string {
	typ := val.Type()

	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}

	if typ.Kind() == reflect.Func {
		funcName := runtime.FuncForPC(val.Pointer()).Name()
		idx := strings.LastIndex(funcName, ".")
		if idx < 0 {
			if funcName != "" {
				return funcName
			}
			return ""
		}

		name := funcName[idx+1:]

		if regOfAnonymousFunc.MatchString(name) {
			return ""
		}

		if regOfNumber.MatchString(name) {
			return ""
		}

		return name
	}

	return typ.Name()
}
