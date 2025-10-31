package parser

type Options struct {
	URI string

	ExtraMeta map[string]any
}
type Option struct {
	apply func(opts *Options)

	implSpecificOptFn any
}
