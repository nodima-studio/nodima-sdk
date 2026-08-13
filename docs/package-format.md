# Nodima runner packages

Status: **experimental, v1alpha1**

A loadable runner is a directory with a strict `manifest.json` and every file
referenced by that manifest:

```text
manifest.json
runner.wasm
config.schema.json
icon.svg                  optional
README.md                 optional
```

Directory loading is the initial development and built-in distribution format.
A deterministic archive format may be added later without changing the
manifest contract.

## Manifest

The manifest uses format version `dbminer.runner.package.v1alpha1`. Unknown JSON
fields are rejected. A minimal example is:

```json
{
  "formatVersion": "dbminer.runner.package.v1alpha1",
  "id": "com.dbminer.pick-columns",
  "version": "0.1.0",
  "abi": "dbminer.runner.v1alpha1",
  "implementation": "wasm",
  "entrypoint": "runner.wasm",
  "configSchema": "config.schema.json",
  "behavior": "streaming",
  "ports": [
    {"id": "input", "direction": "input", "required": true},
    {"id": "output", "direction": "output", "required": true}
  ],
  "capabilities": [],
  "limits": {
    "memoryBytes": 268435456,
    "wallTimeMillis": 300000,
    "maxOutputBytes": 268435456,
    "maxOutputMessages": 100000,
    "stderrBytes": 65536
  },
  "files": {
    "runner.wasm": {"sha256": "<64 lowercase hex characters>"},
    "config.schema.json": {"sha256": "<64 lowercase hex characters>"}
  }
}
```

Package IDs are reverse-domain-style lowercase identifiers. Versions follow
SemVer. The ABI must exactly match the currently supported experimental ABI.
External package manifests accept `wasm` and `javascript`; native built-ins are
registered by trusted host code. JavaScript packages carry a UTF-8 `.js`
entrypoint defining `function process(row, config)`, use the standard required
`input` and `output` ports, stream record batches, and cannot declare
capabilities.

Execution behavior is `streaming`, `blocking`, or `spilling`. Capability names
are `http`, `file-read`, `file-write`, `scratch`, and `secret`. Declaration does
not grant a capability: the project and user must grant it for each execution.
Every v1alpha1 port carries bounded Apache Arrow record batches. Static
validation checks this logical data kind; concrete Arrow column schemas are
negotiated when execution links open.

## Integrity and loading

Every entrypoint, config schema, and optional icon must have a SHA-256 entry.
All other declared files are verified too. `manifest.json` cannot checksum
itself.

The loader rejects:

- unsupported format, ABI, implementation, or capability values;
- invalid package IDs, semantic versions, ports, or resource defaults;
- absolute, non-normalized, Windows-drive, backslash, or parent paths;
- symlinked roots and files;
- non-regular files;
- unknown manifest fields or trailing JSON values;
- missing, malformed, oversized, or checksum-mismatched files;
- invalid config-schema JSON, non-WebAssembly modules, and malformed JavaScript.

Reads use an operating-system rooted filesystem handle. Limits are 256 KiB for
the manifest, 64 MiB for the module, 1 MiB for the config schema, 4 MiB per
other asset, 32 declared files, and 96 MiB total loaded content.

Checksums provide integrity and stable content identity, not publisher trust.
Signatures, publisher identities, revocation, and installation confirmation
belong to the later third-party distribution milestone.

## Building a Go runner

The initial builder compiles a Go `main` package to WASI Preview 1, copies its
declared config schema and optional icon, generates every checksum, validates
the completed directory with the production loader, and then publishes it:

```sh
go run github.com/nodima-studio/nodima-sdk/cmd/nodima-package build-go \
  -manifest ./package.template.json \
  -source ./source \
  -output ./dist/package \
  -workdir .
```

The template has the same fields as a final `manifest.json` except that `files`
must be omitted; the builder owns that field. Asset paths are resolved relative
to the template. The Go source must be a `main` package.

Compilation uses `GOOS=wasip1`, `GOARCH=wasm`, `CGO_ENABLED=0`, `-trimpath`,
read-only module metadata, disabled VCS stamping, and an empty Go build ID.
Equivalent source, template, assets, and Go toolchain produce identical module
and manifest bytes.

The builder creates a temporary directory beside the destination so final
publication is an atomic rename on the same filesystem. It validates before
publication, removes failed temporary builds, and refuses to overwrite any
existing destination.

Other guest languages use the same final directory and manifest format.
Precompiled WASI modules and JavaScript entrypoints use the language-neutral
assembler:

```sh
go run github.com/nodima-studio/nodima-sdk/cmd/nodima-package assemble \
  -manifest ./package.template.json \
  -entrypoint ./runner.js \
  -output ./dist/package
```

No Go-specific information appears in the published package.
