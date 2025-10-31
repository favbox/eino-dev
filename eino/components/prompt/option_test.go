package prompt

import (
	"testing"

	"github.com/smartystreets/goconvey/convey"
)

type implOption struct {
	userID int64
	name   string
}

func WithUserID(uid int64) Option {
	return WrapImplSpecificOptFn[implOption](func(i *implOption) {
		i.userID = uid
	})
}

func WithName(n string) Option {
	return WrapImplSpecificOptFn[implOption](func(i *implOption) {
		i.name = n
	})
}

func TestImplSpecificOption(t *testing.T) {
	convey.Convey("impl_specific_option", t, func() {
		opt := GetImplSpecificOptions(&implOption{}, WithUserID(101), WithName("Wang"))

		convey.So(opt, convey.ShouldEqual, &implOption{
			userID: 101,
			name:   "Wang",
		})
	})
}
