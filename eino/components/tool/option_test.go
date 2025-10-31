package tool

import (
	"testing"

	"github.com/smartystreets/goconvey/convey"
)

func TestImplSpecificOpts(t *testing.T) {
	convey.Convey("TestImplSpecificOpts", t, func() {
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

		toolOption1 := WrapImplSpecificOptFn(withConf("test_conf"))
		toolOption2 := WrapImplSpecificOptFn(withIndex(1))

		implSpecificOpts := GetImplSpecificOptions(&implSpecificOptions{}, toolOption1, toolOption2)

		convey.So(implSpecificOpts, convey.ShouldResemble, &implSpecificOptions{
			conf:  "test_conf",
			index: 1,
		})
	})
}
