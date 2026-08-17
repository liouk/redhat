# OCPBUGS-106186: Technical Analysis

## What is happening

`oc image mirror` intermittently fails when uploading blobs to a local Docker
registry (registry:2.8) on bare-metal CI hosts. The failure sequence is:

1. The client starts uploading a blob over HTTP/2 via a single PATCH request.
2. After sending approximately 2 MiB, the client sends an HTTP/2 RST_STREAM
   CANCEL, aborting its own upload.
3. The registry logs: `"client disconnected during blob PATCH" copied=2097153
   error="stream error: stream ID NNNN; CANCEL"`.
4. On retry, the client starts a new upload session with Offset: 0, but the
   server has already written 2,097,153 bytes from the aborted upload. The
   registry rejects: `"upload resumed at wrong offset: 2097153 != 0"`.
5. Every subsequent retry hits the same offset mismatch. The failure is
   unrecoverable within a single `oc image mirror` invocation.

This blocks Keycloak image mirroring, which blocks all
`[sig-auth][Suite:openshift/auth/external-oidc]` tests. The pass rate is 93.75%
vs. the required 95%, making this a release blocker for 5.0.

## Root cause

The bug originates in the `httpBlobUpload.ReadFrom` method in the vendored
`distribution/distribution` library
(`vendor/github.com/distribution/distribution/v3/registry/client/blob_writer.go`,
line 39).

`ReadFrom` sends a PATCH request with **no Content-Length, Content-Range, or
Content-Type headers** -- violating the [OCI Distribution
Spec](https://github.com/opencontainers/distribution-spec/blob/main/spec.md#pushing-a-blob-in-chunks),
which defines the chunked blob upload PATCH request as requiring all three.

Compare the two upload methods on the same type:

| Header           | `Write` (line 72) | `ReadFrom` (line 39) |
|------------------|--------------------|----------------------|
| `Content-Type`   | `application/octet-stream` | not set      |
| `Content-Range`  | `{offset}-{offset+len-1}`  | not set      |
| `Content-Length`  | `len(p)`                   | not set      |

The missing Content-Length is what triggers the HTTP/2 failure, as explained
below.

### HTTP/1.1 vs HTTP/2

Over HTTP/1.1, the missing Content-Length is harmless: Go's HTTP client falls
back to **chunked transfer encoding**, where each piece of data is prefixed with
its size. The server reads chunks until a zero-length terminator arrives. There
is no protocol-level flow control beyond TCP backpressure.

Over HTTP/2, chunked transfer encoding does not exist. Instead, HTTP/2 uses
**stream-level flow control**: the server grants the client a byte budget (the
flow control window) via SETTINGS_INITIAL_WINDOW_SIZE. The client can send DATA
frames up to that budget, then must pause and wait for the server to send a
WINDOW_UPDATE frame granting more.

### The race condition

When uploading a blob over HTTP/2 without a Content-Length header, the client
exhausts the server's flow control window and must pause; if the server's
response headers arrive before the flow control grant that would let the client
resume sending, Go's HTTP/2 transport interprets this as the server being done
and aborts the upload.

Step by step:

1. `ReadFrom` passes the reader as `req.Body` via `io.NopCloser(r)`. Since the
   body length is unknown, Go's HTTP/2 transport treats `ContentLength` as -1.
2. The HTTP/2 transport sends DATA frames, consuming the server's flow control
   window. Once the window is exhausted, the client blocks in
   `awaitFlowControl`, waiting for a WINDOW_UPDATE.
3. Meanwhile, the server has received the data and is processing it. It may send
   **response headers** (e.g. 202 Accepted) on the same stream.
4. Two things are now racing:
   - The server sending a **WINDOW_UPDATE** (which would let the client continue)
   - The server sending **response headers** (which Go interprets as "done")
5. If response headers arrive first, Go's HTTP/2 transport sees a response while
   the body write is still incomplete. Because Content-Length was never set, Go
   cannot tell whether all data has been sent. It defaults to aborting: it stops
   the body write and sends RST_STREAM CANCEL.
6. With Content-Length set, this race does not matter -- Go knows exactly how
   many bytes to send and coordinates the END_STREAM flag properly.

### The 2 MiB boundary

The bug report consistently shows the client disconnecting after 2,097,153 bytes
(2 MiB + 1). The exact source of this boundary is uncertain. Go's default HTTP/2
server settings are:

| Setting                          | Default value |
|----------------------------------|---------------|
| `initialWindowSize` (HTTP/2 spec)| 65,535 bytes  |
| `MaxUploadBufferPerStream`       | 1 MiB         |
| `MaxUploadBufferPerConnection`   | 1 MiB         |

The defaults point to 1 MiB, not 2 MiB. The 2 MiB figure could result from:
- The stream window + an additional WINDOW_UPDATE sent during processing
- The registry overriding defaults in its HTTP/2 server config
- An internal buffer size in the registry's blob upload handler

Confirming the exact mechanism requires a packet capture
(`GODEBUG=http2debug=2` on both sides, or tcpdump with HTTP/2 frame decoding).

### Why retry fails

The retry logic in `copyBlob` (`mirror.go:773-779`) has two problems:

1. The retry predicate only matches `REFUSED_STREAM`, not the RST_STREAM CANCEL
   or offset mismatch errors that actually occur.
2. Even if the predicate were widened, each retry calls `c.to.Create(ctx)` which
   starts a new upload session with Offset: 0. But the server-side upload from
   the aborted attempt still has 2,097,153 bytes. The registry rejects the
   mismatched offset. The previous `BlobWriter` (and its upload UUID) is gone by
   the time the next attempt starts, so there is no way to cancel the stale
   server-side session.

## Why did this start happening now

Nothing changed in the HTTP/2 transport or in `oc image mirror`. The Go version
went from 1.24 to 1.26, but the HTTP/2 flow control and body write logic in
`x/net` is identical across all three versions. HTTP/2 has always been enabled
for registry communication (via ALPN over TLS).

The `ExternalOIDCWithUpstreamParity` bare-metal test is **new to 5.0**. The base
release (4.22) shows 0 successes and 0 failures -- the test did not exist. This
is not a code regression; it is a pre-existing latent bug exposed by a new test
that mirrors a Keycloak image to a local registry. The bare-metal CI setup is
unusual: client and registry run on the same host via localhost, making the
timing window for the race much tighter than with a remote registry.

## Upstream status

The bug has **not been fixed upstream** in `distribution/distribution`. The
current `ReadFrom` on the main branch still does not set Content-Length.

Related upstream activity:

- [#3965](https://github.com/distribution/distribution/issues/3965) (closed) /
  [PR #3981](https://github.com/distribution/distribution/pull/3981) (merged Aug
  2023): Fixed the missing `Content-Type` header in `ReadFrom`. The issue
  explicitly acknowledged that Content-Length was also missing but deferred it,
  calling it "challenging to implement without introducing buffering."
- [#2593](https://github.com/distribution/distribution/issues/2593) (open, P1,
  from 2018): Reports the same server-side symptom ("upload resumed at wrong
  offset"). No resolution, no connection to the HTTP/2 client-side cause.

No open issue or PR exists for the specific problem: `ReadFrom` not setting
Content-Length causing HTTP/2 flow control race leading to RST_STREAM CANCEL and
unrecoverable offset mismatch on retry.

## Options

### 1. Disable HTTP/2 for baremetal CI jobs (immediate workaround)

Set `DISABLE_HTTP2=1` in the environment for the affected CI jobs. This variable
is already recognized by `SetTransportDefaults` in
`vendor/k8s.io/apimachinery/pkg/util/net/http.go:134`. Over HTTP/1.1, the
missing Content-Length is handled by chunked transfer encoding and the race does
not occur.

Pros: No code changes, immediate fix.
Cons: Workaround, not a fix. Disables HTTP/2 for all registry operations in
those jobs.

### 2. Buffered upload via `Write` instead of `ReadFrom` (oc-only fix)

Replace the `w.ReadFrom(r)` call in `copyBlob` (`mirror.go:758`) with
`io.ReadAll(r)` followed by `w.Write(data)`. The `Write` method correctly sets
Content-Length, Content-Range, and Content-Type.

Pros: No vendor changes. Single PATCH request with correct headers.
Cons: Buffers the entire blob in memory. With 6 concurrent uploads
(`MaxPerRegistry` default), worst case is 6x blob size in memory (typical layers
are 10-200 MB). `ReadFrom` streams with ~16 KB in memory at any time.

This could be made opt-in via a CLI flag (e.g. `--force-buffered-upload`) so
the default behavior is unchanged and CI jobs opt in.

### 3. Vendor patch to `ReadFrom` (carry patch)

Patch the vendored `blob_writer.go` to set Content-Length in `ReadFrom`. The
blob size is not directly available inside `ReadFrom` (it receives a bare
`io.Reader`), so the caller must communicate it. Options:

- Add a `ContentLength() int64` interface check on the reader
- Add a new method like `ReadFromWithLength(r io.Reader, length int64)`
- Store expected size on the `httpBlobUpload` struct

Pros: Preserves streaming, fixes root cause, one PATCH request.
Cons: Requires carrying a vendor patch until upstream merges a fix.

### 4. Upstream fix in `distribution/distribution`

File an upstream issue linking #2593 and #3965 with the full HTTP/2 root cause
analysis. Propose a fix (e.g. a new `ReadFromWithLength` method or a
`ContentLength` interface on the reader).

Pros: Proper long-term fix.
Cons: Depends on upstream acceptance timeline.

These options are not mutually exclusive. Option 1 provides immediate CI relief.
Option 2 or 3 provides an oc-level fix. Option 4 is the long-term solution.

## Key files

| File | Lines | What |
|------|-------|------|
| `pkg/cli/image/mirror/mirror.go` | 667-780 | `copyBlob` -- blob upload and retry logic |
| `vendor/.../distribution/v3/registry/client/blob_writer.go` | 39-70 | `ReadFrom` -- PATCH with no headers (the bug) |
| `vendor/.../distribution/v3/registry/client/blob_writer.go` | 72-105 | `Write` -- PATCH with correct headers |
| `vendor/.../distribution/v3/registry/client/repository.go` | 820-827 | `httpBlobUpload` construction |
| `pkg/cli/image/manifest/manifest.go` | 120-155 | Transport creation (HTTP/2 enabled) |
| `vendor/k8s.io/apimachinery/pkg/util/net/http.go` | 131-143 | `SetTransportDefaults` -- DISABLE_HTTP2 env var |

## CI integration point

The `oc image mirror` invocations for the affected jobs are in the shared
baremetal e2e test step in the release repo:

`ci-operator/step-registry/baremetalds/e2e/test/baremetalds-e2e-test-commands.sh`
(lines 78-82, function `run-oc-image-mirror`).
