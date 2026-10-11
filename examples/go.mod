module github.com/gomaja/go-sccp/examples

go 1.26

require (
	github.com/gomaja/go-m3ua v1.3.1-0.20261010214351-e05bb5c83207
	github.com/gomaja/go-sccp v0.0.0
	github.com/gomaja/go-sctp v1.1.1-0.20261010090839-614ae6465de4
)

// The examples build against the library in the same commit.
replace github.com/gomaja/go-sccp => ../
