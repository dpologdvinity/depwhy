// Fixture for --go-graph. Every dependency is replaced by a local directory so
// `go mod graph` runs offline. b requires c v1.0.0 while the main module
// requires c v1.1.0, so MVS selects c v1.1.0; c v1.0.0 and the d that only it
// requires are not part of the selected graph. The go and toolchain lines make
// `go mod graph` print go@ and toolchain@ pseudo-entries.
module example.com/demo

go 1.22

toolchain go1.22.0

require (
	example.com/b v1.0.0
	example.com/c v1.1.0 // indirect
)

replace example.com/b v1.0.0 => ./b

replace example.com/c v1.0.0 => ./c0

replace example.com/c v1.1.0 => ./c1

replace example.com/d => ./d
