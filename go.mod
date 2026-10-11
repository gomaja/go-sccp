module github.com/gomaja/go-sccp

go 1.23

require github.com/google/go-cmp v0.7.0

// The module is used from its main branch; every published version is
// withdrawn. The trailing comment is what the go command shows.

retract [v1.0.0, v1.0.2] // superseded: use go get github.com/gomaja/go-sccp@main
