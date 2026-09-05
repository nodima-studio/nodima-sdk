# Curated catalog integration

Follow the target catalog's `AGENTS.md`, `CONTRIBUTING.md`, and build script as authoritative. Inspect at least one current node of the same implementation kind before creating files.

A typical catalog path is:

```text
nodes/<slug>/<semver>/
```

It includes the standard package sources plus `repository.json`, whose concise summary describes the node in catalog listings. The folder slug is a stable human-readable name; the manifest package ID is the runtime identity and normally uses the catalog's existing reverse-domain namespace.

Do not edit prior version directories. When releasing a change, copy only the source and metadata that remain true, bump the semantic version, and update the new template's `version`. Do not carry stale generated checksums because `package.template.json` omits `files`.

Run the catalog's aggregate build from its repository root. It may intentionally disable a parent Go workspace to use the SDK pinned in its own `go.mod`. Verify that source tests run, every package builds, the catalog is reproducible, and ignored `dist/` content is not staged. Only commit a regenerated catalog when the repository workflow requires it.

Do not create release tags, publish artifacts, or alter existing releases unless the user explicitly asks for that release operation.
