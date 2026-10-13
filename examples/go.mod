module github.com/gomaja/go-sccp/examples

go 1.26

require (
	github.com/gomaja/go-m3ua v0.0.0-20261013190641-9c90e4c48c7a
	github.com/gomaja/go-sccp v0.0.0
	github.com/gomaja/go-sctp v0.0.0-20261013061924-51d016bab120
)

// The examples build against the library in the same commit.
replace github.com/gomaja/go-sccp => ../
