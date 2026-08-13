# Language SDKs

The wire protocol is language-neutral. A language SDK must provide canonical
CBOR control messages, Arrow IPC batch framing, lifecycle helpers, capability
request correlation, WASI Preview 1 stdio transport, and conformance against
the fixtures and validation rules in `runner/v1`.

Go and Rust implementations are provided. Recommended next targets are
Zig/TinyGo and C-compatible bindings. JavaScript node packages use
the host's sandboxed `process(row, config)` runtime and do not require a WASI
guest SDK.

New language SDKs should live in top-level language directories and release
independently while declaring the exact runner ABI they support.
