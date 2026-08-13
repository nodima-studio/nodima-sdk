# Nodima runner ABI

Status: **experimental, pre-v1**

This document defines the logical contract. Control messages use canonical
CBOR and typed batches use Apache Arrow IPC. Exact tagged-frame details
remain pre-v1 until allocation-limit and two-language conformance tests pass.

## Package

A distributable runner package contains:

```text
manifest.json
runner.wasm
config.schema.json
icon.svg          optional
README.md         optional
```

The manifest declares:

- package ID and semantic version;
- ABI version;
- implementation kind;
- input and output ports;
- configuration schema;
- streaming, blocking, or spilling behavior;
- required capabilities;
- default time, memory, output, log, and scratch limits;
- content checksums and, later, a signature.

The exact v1alpha1 manifest and loader rules are defined in
[Runner packages](package-format.md).

## Lifecycle

1. Host validates the package and manifest.
2. Host creates an isolated runner instance.
3. Host sends initialization and configuration.
4. Runner emits or accepts port schemas.
5. Host sends bounded input batches and per-port end markers.
6. Runner emits bounded output batches, logs, and progress.
7. Runner completes, fails, or is cancelled.
8. Host releases the instance and its execution-scoped resources.

## Logical messages

Host to runner:

- `initialize`
- `input_schema`
- `input_batch`
- `input_end`
- `capability_response`
- `cancel`

Runner to host:

- `ready`
- `output_schema`
- `output_batch`
- `log`
- `progress`
- `capability_request`
- `completed`
- `failed`

Each control message includes an ABI version, message type, execution ID, node
ID, and port ID where relevant. Transport frames identify CBOR control payloads
and Arrow IPC data payloads. Every frame is length-delimited and capped before
allocation.

## Physical transport v1alpha1

Runners read frames from stdin and write frames to stdout. Stderr is bounded
diagnostic text and never contains protocol data. Every frame starts with:

| Offset | Size | Meaning |
|---:|---:|---|
| 0 | 4 | Magic bytes `44 42 4d 01` (`DBM` plus transport version 1) |
| 4 | 1 | Payload kind: `1` for control, `2` for Arrow batch |
| 5 | 4 | Unsigned big-endian payload length |

The declared payload length excludes the nine-byte header. Receivers validate
the magic, kind, non-zero length, and configured frame limit before allocating
the payload.

A control payload is one canonical CBOR `Message`. `input_batch` and
`output_batch` are forbidden in control frames.

An Arrow batch payload contains:

| Offset | Size | Meaning |
|---:|---:|---|
| 0 | 4 | Unsigned big-endian CBOR metadata length |
| 4 | variable | Canonical CBOR `BatchMetadata`, capped at 64 KiB |
| next | variable | One complete, uncompressed Arrow IPC stream |

`BatchMetadata` contains the ABI version, `input_batch` or `output_batch`
message type, execution ID, node ID, and port ID. The IPC stream contains its
schema and exactly one record batch. A self-contained stream per transport
frame makes frames independently validatable and keeps control/data
multiplexing deterministic. Schema repetition is accepted for v1alpha1 and can
only be removed by a later versioned transport change.

The wazero host pumps these frames incrementally in both directions. A blocked
engine output link stops the host output callback, which fills the bounded pipe
and naturally applies backpressure to the guest. Input pumps stop on guest
completion or cancellation, so a guest is never required to consume a
pre-collected in-memory message slice.

## Data types

The implemented v1alpha1 portable subset is expressed with Arrow schemas and
contains:

- nullability;
- boolean;
- signed 64-bit integer;
- 64-bit float;
- UTF-8 string;
- bytes;

The v1 target additionally needs:

- UTC timestamp with unit metadata;
- list;
- object/struct.

Conversions must be explicit. Filter, sort, append, and join nodes do not infer
lossy conversions silently.

The Go SDK currently converts between the transitional DBMiner `Batch` model
and Arrow at its transport boundary. This keeps the reference runner small
while conformance is established. Engine and SDK APIs will expose Arrow batches
directly before ABI v1 so native runners can avoid this conversion.

## Capabilities

Guests have no ambient filesystem, network, environment, or secret access.
They request narrow operations from the host. Capability requests and
responses are part of the protocol so guest languages do not require raw
socket or filesystem support.

Each request has an ID, capability kind, and kind-specific payload. The
response repeats the ID and kind and contains exactly one result or structured
failure. HTTP request and response fields are defined in
[Host capabilities](capabilities.md).

Capability request and response frames are internal execution traffic. They
count toward runner byte, message, call, and wall-time limits but are not
forwarded as node output.

## Session messages

A remote agent runs behind the same protocol, with a session header in front of
it. The host sends `session_start`, the agent answers `session_ready`, and only
then can setup continue. Reading the header first lets a stale agent be rejected
before any runner starts: `session_ready` carries the agent's ABI, SHA-256, and
an ephemeral peer port when the topology has cross-host edges.

`session_start` carries the host's nodes, agent-local edges, service boundary
links, peer edges, limits, and optional ephemeral TLS material. For peer runs,
`session_connect` supplies expected peer coordinates and topology; every agent
answers `session_connected` only after its listeners, authentication, and edge
multiplexers are ready. `session_run` is the sole runner-start barrier.
`session_completed`, per-node results, and `edge_stats` close the session and
report the logical project-edge measurements.

`cancel` asks an agent to stop. It is best effort; closing the transport is the
guarantee, because SSH cannot be relied on to deliver a signal.

`BatchMetadata.edge_id` selects the multiplexed boundary or peer edge. Peer
connections apply explicit per-edge `credit`: initial credit equals the bounded
link capacity, every received batch consumes one credit, and dequeuing that
batch refunds it. Batch metadata also has an optional sequence for ordered
replica dispatch. The runner ABI remains `v1alpha1`; these session messages are
between matching service and agent builds, whose exact checksum is verified
before the run barrier.

Unknown CBOR fields are ignored by the decoder, so optional additions are
backward compatible in both directions.

## Compatibility

- ABI major versions are incompatible.
- Unknown optional message fields are ignored.
- Unknown message types fail the runner instance.
- A project references a runner package ID and compatible version range, not a
  machine-specific executable path.
