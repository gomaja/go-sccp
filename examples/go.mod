module github.com/gomaja/go-sccp/examples

go 1.26

require (
	github.com/gomaja/go-m3ua v0.0.0-20261015055950-600260766a36
	github.com/gomaja/go-sccp v0.0.0
	github.com/gomaja/go-sctp v0.0.0-20261015040308-001e3ddea46c
)

require github.com/gomaja/go-ctxsync v0.0.0-20261014212015-de26fb453109 // indirect

// The examples build against the library in the same commit.
replace github.com/gomaja/go-sccp => ../
