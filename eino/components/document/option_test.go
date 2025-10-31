package document

import (
	"testing"

	"github.com/smartystreets/goconvey/convey"

	"github.com/favbox/eino/components/document/parser"
)

func TestImplSpecificOpts(t *testing.T) {
	type implSpecificOptions struct {
		conf  string
		index int
	}

	withConf := func(conf string) func(o *implSpecificOptions) {
		return func(o *implSpecificOptions) {
			o.conf = conf
		}
	}

	withIndex := func(index int) func(o *implSpecificOptions) {
		return func(o *implSpecificOptions) {
			o.index = index
		}
	}

	convey.Convey("TestLoaderImplSpecificOpts", t, func() {
		documentOption1 := WrapLoaderImplSpecificOptFn(withConf("test_conf"))
		documentOption2 := WrapLoaderImplSpecificOptFn(withIndex(1))

		implSpecificOpts := GetLoaderImplSpecificOptions(&implSpecificOptions{}, documentOption1, documentOption2)

		convey.So(implSpecificOpts, convey.ShouldResemble, &implSpecificOptions{
			conf:  "test_conf",
			index: 1,
		})
	})
	convey.Convey("TestTransformerImplSpecificOpts", t, func() {
		documentOption1 := WrapTransformerImplSpecificOptFn(withConf("test_conf"))
		documentOption2 := WrapTransformerImplSpecificOptFn(withIndex(1))

		implSpecificOpts := GetTransformerImplSpecificOptions(&implSpecificOptions{}, documentOption1, documentOption2)

		convey.So(implSpecificOpts, convey.ShouldResemble, &implSpecificOptions{
			conf:  "test_conf",
			index: 1,
		})
	})
}

func TestCommonOptions(t *testing.T) {
	convey.Convey("TestCommonOptions", t, func() {
		o := &LoaderOptions{ParserOptions: []parser.Option{{}}}
		o1 := GetLoaderCommonOptions(o)
		convey.So(len(o1.ParserOptions), convey.ShouldEqual, 1)

		o2 := GetLoaderCommonOptions(o, WithParserOptions(parser.Option{}, parser.Option{}))
		convey.So(len(o2.ParserOptions), convey.ShouldEqual, 2)
	})
}
