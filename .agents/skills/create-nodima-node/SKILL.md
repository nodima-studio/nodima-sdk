---
name: create-nodima-node
description: Create, extend, and verify portable Nodima node packages using the repository's current runner ABI and packaging tools. Use when implementing a new Go/WASI or sandboxed JavaScript node, adding a node version to a Nodima catalog repository, or repairing a node package so it builds and validates.
---

# Create Nodima Node

Build a complete source package that follows the checked-out SDK rather than relying on remembered protocol details.

## Establish the target

Translate the request into a display name, package ID, semantic version, inputs and outputs, configuration, execution behavior, capabilities, and null/error behavior. Inspect nearby packages and repository instructions before choosing paths or conventions.

Do not put a production node in `nodima-sdk`: this repository owns contracts and tooling, not the public node catalog. When invoked here without a destination, ask where the node should be created. A catalog repository normally has `nodes/`, `catalog.json`, and `CONTRIBUTING.md`; a standalone node may use its own directory.

Never modify an already published version. Add a new semantic-version directory when changing a catalog node.

## Choose an implementation

- Prefer JavaScript for a stateless, one-input/one-output row transform needing no host capabilities. Read [references/javascript.md](references/javascript.md).
- Use Go targeting WASI Preview 1 for batch-aware behavior, multiple ports, capabilities, progress/log messages, or tighter control of types and streaming. Read [references/go.md](references/go.md).
- In a curated catalog repository, also read [references/catalog.md](references/catalog.md).

Do not invent a raw filesystem, network, environment, or secret channel. Go nodes request only narrowly declared host capabilities. JavaScript nodes cannot declare capabilities.

## Create the package sources

A normal source package contains:

```text
package.template.json
config.schema.json
ui.json
README.md
source/
```

An optional `icon.svg` may be included. A catalog may additionally require `repository.json`.

Use the exact constants and accepted values in [`runner/v1/manifest.go`](../../../runner/v1/manifest.go) and [`runner/v1/ui.go`](../../../runner/v1/ui.go). Preserve the compatibility identifiers `dbminer.runner.package.v1alpha1` and `dbminer.runner.v1alpha1` unless those files say otherwise. The template must omit `files`; the package builder generates integrity entries.

Configuration reaches runners as `map[string]string`. Model choices as string enums and list-like values as a documented serialized string. Keep `config.schema.json`, `ui.json`, runner defaults, and README semantics aligned. The JSON schema is authoritative for validation; UI metadata is presentation only. Use `visibleWhen` for conditional fields instead of teaching runner IDs to a host UI.

Choose `streaming`, `blocking`, or `spilling` based on whether the node can emit bounded results while reading input. Do not call a collecting implementation streaming. Declare only capabilities the implementation actually requests. Treat package limits as enforced budgets, not hints.

## Test behavior

Add focused tests beside source when the language supports it. Cover the transformation or generation logic independently from stdio, including:

- every supported input type and configuration mode;
- null preservation and empty batches;
- missing columns, wrong types, malformed configuration, and capability failures;
- output schema/column order and bounded output behavior;
- lifecycle details that are non-trivial for the node.

Errors should name the node and explain the offending field, column, row, or operation. Do not expose secret values in errors or logs.

## Build and verify

Use the checked-out SDK tooling or the SDK version pinned by the target repository. Follow target-repository commands when present. For a standalone Go source package:

```sh
go test ./...
go run github.com/nodima-studio/nodima-sdk/cmd/nodima-package build-go \
  -manifest ./package.template.json \
  -source ./source \
  -output ./dist/package \
  -workdir .
```

For JavaScript:

```sh
go run github.com/nodima-studio/nodima-sdk/cmd/nodima-package assemble \
  -manifest ./package.template.json \
  -entrypoint ./source/runner.js \
  -output ./dist/package
```

The destination must not already exist. Remove or choose a fresh ignored build destination before rerunning; never overwrite published source. If the repository has an aggregate build script, run it because it may also reproduce the catalog and archive.

Inspect the generated `manifest.json` and report the exact verification commands. Do not commit `dist/`, generated packages, archives, or unrelated catalog changes unless repository instructions explicitly require them.

For package rules and the logical lifecycle, consult [`docs/package-format.md`](../../../docs/package-format.md) and [`docs/runner-abi.md`](../../../docs/runner-abi.md) when an implementation decision is not covered here.
