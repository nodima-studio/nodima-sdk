# Go/WASI node guidance

Use the public imports:

```go
import (
	runnersdk "github.com/nodima-studio/nodima-sdk/go"
	runnerv1 "github.com/nodima-studio/nodima-sdk/runner/v1"
)
```

Implement `runnersdk.Runner` for nodes without capabilities or `runnersdk.RunnerWithCapabilities` when host-brokered operations are required. End `main` with `runnersdk.Main(runner{})`.

## Lifecycle

The implementation must:

1. accept exactly one `initialize`, parse and validate resolved string configuration, then emit `ready`;
2. process `input_batch` messages without collecting an unbounded stream;
3. react to `input_end` by flushing bounded pending output and emitting `completed`;
4. return contextual errors for invalid ordering or unsupported messages so `runnersdk.Main` emits `failed`;
5. pass the supplied context into input, output, and capability operations.

Copy the initialize message's execution and node IDs onto lifecycle output. Set the declared output port on every output batch. If sequence-bearing batch-parallel operation is selected, preserve each input sequence on all corresponding output and emit `batch_complete` for that sequence; do not enable batch parallelism for stateful ordering, capabilities, multiple required inputs, or cross-batch aggregation.

An input batch may contain zero rows and still carry the schema downstream. Emit an output batch for every input batch when the node represents a streaming transform, even if the result has zero rows. Nodes that expand input must flush bounded chunks instead of constructing one arbitrarily large batch.

## Batch correctness

The current portable types are `boolean`, `int64`, `float64`, `string`, and `bytes`. Set `RowCount` consistently with every column's value slice. Preserve column order and unmodified columns unless the node contract says otherwise.

`Column.Valid` is either empty (all values valid) or exactly `RowCount` entries. Preserve it when transforming values; do not turn nulls into zero values or empty strings. Deep-copy any slice that will be mutated. Validate selected column types explicitly and do not perform undocumented lossy conversions.

## Capabilities

Declare the exact manifest capability and use the matching SDK interface:

- `http` via `Capabilities.HTTP`;
- `file-read` and `file-write` via a `runnersdk.FileCapabilities` assertion;
- `scratch` via a `runnersdk.ScratchCapabilities` assertion;
- `secret` via a `runnersdk.SecretCapabilities` assertion.

Treat failed interface assertions and structured `CapabilityError` values as normal runner errors. A secret is configuration-independent sensitive data: request it by name, never accept its clear text in ordinary node configuration, logging, or output unless the node's explicit contract requires outputting it.

Keep file scopes and paths narrow. Scratch is execution-scoped temporary storage, not ambient disk access.

## Tests

Extract configuration parsing and batch transformations into ordinary functions and unit-test them. For lifecycle-sensitive nodes, use in-memory `runnersdk.Input` and `runnersdk.Output` fakes to assert message order, IDs, ports, empty-batch output, completion, and errors. Test capability calls with a narrow fake rather than real network or filesystem access.
