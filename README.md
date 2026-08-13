# Nodima SDK

Public contracts and tools for building portable Nodima Studio node packages.

Licensed under the [MIT License](LICENSE).

- `runner/v1` defines the versioned runner ABI, messages, Arrow batch mapping,
  package manifests, capability requests, and remote-session wire contracts.
- `go` is the Go guest SDK for WASI Preview 1 nodes.
- `rust` is the Rust guest crate for WASI Preview 1 nodes.
- `packagekit` validates, assembles, archives, and builds deterministic packages.
- `cmd/nodima-package` exposes package building and validation workflows.

The current wire identifiers intentionally remain `dbminer.runner.v1alpha1`
and `dbminer.runner.package.v1alpha1`. They are compatibility identifiers, not
display branding, and changing them would invalidate existing projects,
packages, agents, and archives.

## Go node

```go
import (
    runnerv1 "github.com/nodima-studio/nodima-sdk/runner/v1"
    runnersdk "github.com/nodima-studio/nodima-sdk/go"
)
```

Build a Go node with:

```sh
go run github.com/nodima-studio/nodima-sdk/cmd/nodima-package build-go \
  -manifest package.template.json -source ./source -output ./dist/package
```

The Rust crate exposes `run`, `Context`, `Error`, and `Runner`, using Arrow
`RecordBatch` values at the node boundary. Other languages should implement the
same documented ABI and pass the shared wire/package fixtures.

See [the ABI](docs/runner-abi.md), [package format](docs/package-format.md), and
[language SDK requirements](docs/language-sdks.md).
