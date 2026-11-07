module github.com/gomaja/go-sccp/examples

go 1.26

require (
	github.com/gomaja/go-m3ua v0.0.0-20261105084705-26cf9e1fd4af
	github.com/gomaja/go-pointcode v0.0.0-20261015102047-53f963a604bd
	github.com/gomaja/go-sccp v0.0.0
	github.com/gomaja/go-sctp v0.0.0-20261104191826-88a7cc2a62fb
)

require github.com/gomaja/go-ctxsync v0.0.0-20261015170731-23c9cb67b19b // indirect

// The examples build against the library in the same commit.
replace github.com/gomaja/go-sccp => ../
