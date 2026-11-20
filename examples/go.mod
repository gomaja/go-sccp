module github.com/gomaja/go-sccp/examples

go 1.26

require (
	github.com/gomaja/go-m3ua v0.0.0-20261120080009-f9898712ea7e
	github.com/gomaja/go-pointcode v0.0.0-20261015102047-53f963a604bd
	github.com/gomaja/go-sccp v0.0.0
	github.com/gomaja/go-sctp v0.0.0-20261112061005-2d7d4d9cb936
)

require github.com/gomaja/go-ctxsync v0.0.0-20261015170731-23c9cb67b19b // indirect

// The examples build against the library in the same commit.
replace github.com/gomaja/go-sccp => ../
