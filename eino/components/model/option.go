package model

type Options struct {
}

type Option struct {
	apply func(opts *Options)

	implSpecificOptFn any
}
