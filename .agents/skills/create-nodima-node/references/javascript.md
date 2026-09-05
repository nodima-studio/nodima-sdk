# JavaScript node guidance

Use JavaScript only for a sandboxed row transform. The entrypoint is UTF-8 source that defines:

```js
function process(row, config) {
  // Mutate and return row, or return a replacement row.
  return row;
}
```

The manifest requirements are fixed:

- `implementation` is `javascript`;
- `behavior` is `streaming`;
- the only ports are required `input` and required `output`;
- `capabilities` is empty;
- distribution is omitted or singleton.

The runtime supplies one row and a string-valued configuration object. Preserve properties the node does not own. Handle `null` and `undefined` explicitly, reject unsupported input types with a contextual error, and avoid undeclared globals or environment assumptions.

JavaScript packages have no filesystem, network, scratch, secret, package import, or host API access. If the requested behavior needs one, implement a Go/WASI node instead.

Keep transformation logic deterministic. Do not buffer rows across calls or depend on call ordering. Make schema-changing behavior explicit in the README and ensure the returned values use the portable Nodima types.

Use the package assembler to validate syntax, metadata, assets, checksums, and the final package. If the target repository provides a build script, use it instead of bypassing its catalog checks.
