package parser

import (
	"testing"

	"github.com/smartystreets/goconvey/convey"
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

	convey.Convey("TestImplSpecificOpts", t, func() {
		parserOption1 := WrapImplSpecificOptFn(withConf("test_conf"))
		parserOption2 := WrapImplSpecificOptFn(withIndex(1))

		implSpecificOpts := GetImplSpecificOptions(&implSpecificOptions{}, parserOption1, parserOption2)

		convey.So(implSpecificOpts, convey.ShouldResemble, &implSpecificOptions{
			conf:  "test_conf",
			index: 1,
		})
	})
}
