// Command hne-collector scans the subnets visible from this machine and uploads the
// observations to the Home Net Explorer server. See contracts/collector-cli.md.
package main

// version is set at build time with -ldflags "-X main.version=...".
var version = "dev"

func main() {
	_ = version
}
