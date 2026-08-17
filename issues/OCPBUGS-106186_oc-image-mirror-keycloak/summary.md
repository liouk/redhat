## What is happening

`oc image mirror` intermittently fails when uploading blobs to a local Docker registry (`registry:2.8`) on bare-metal CI hosts. The client sends exactly 2 MiB + 1 byte over HTTP/2, then cancels its own upload with an HTTP/2 `RST_STREAM CANCEL`. On retry, the server has 2 MiB of partial data but the client restarts from offset 0 — the registry rejects with `"upload resumed at wrong offset"` and the failure is unrecoverable. This blocks Keycloak image mirroring, which blocks all external-OIDC tests.

## Why does this happen

`oc image mirror` uses a 3rd party library [distribution/distribution](github.com/distribution/distribution) to mirror images to the internal registry; in particular, it calls [`httpBlobUpload.ReadFrom`](https://github.com/openshift/oc/blob/main/pkg/cli/image/mirror/mirror.go#L758) to stream image blob data from the external to the internal registry.

The `httpBlobUpload.ReadFrom` method in the `distribution/distribution` library sends a `PATCH` request with no `Content-Length`, `Content-Range`, or `Content-Type` headers — violating the [OCI Distribution Spec](https://github.com/opencontainers/distribution-spec/blob/main/spec.md#pushing-a-blob-in-chunks). Over HTTP/1.1 this works because Go uses chunked transfer encoding (i.e. each chunk sent by HTTP/1.1 is prefixed with its size). Over HTTP/2 (which `oc image mirror` enables by default), the unknown body length causes a race: the client fills the server's flow control window and must pause (the bug manifests at ~2 MiB, though the exact window size depends on the registry's HTTP/2 configuration); if the server sends response headers before granting more flow control, Go's HTTP/2 transport aborts the stream. Go does this because it doesn't know whether it has more data to send, since `Content-Length` is missing. Retrying does not help because the server retains the partial upload state from the aborted attempt, while the client starts a new session from offset 0 — the offset mismatch is unrecoverable.

## Why did it start happening now

This behavior is a combination of:

- `httpBlobUpload.ReadFrom` doesn't set `Content-Length`
- The registry sends response headers before granting more flow control (valid per HTTP/2 spec)
- Go's HTTP/2 transport aborts unknown-length body writes on early server response (i.e. before `WINDOW_UPDATE`)

This started happening now because the tests that trigger this problem were recently enabled upon promotion of the `ExternalOIDCWithUpstreamParity` feature gate. These tests mirror a keycloak image to the internal registry, and for baremetal hosts, client and registry live on the same host via localhost, so the timing window is much tighter, which allows the race to occur.

## How can we fix this

1. **Disable HTTP/2 for the baremetal jobs.** This is essentially a stop-the-bleeding solution; as explained above HTTP/1.1 won't encounter this issue, so since these jobs are not testing HTTP/2 specifically, this is a quick workaround. A `DISABLE_HTTP2=1` environment variable is already recognized by `SetTransportDefaults` in the apimachinery library, so no code changes are needed — just set the env var in the CI job.

2. **Change how the mirroring is done.** The current implementation streams image data through a single `PATCH` request with `httpBlobUpload.ReadFrom`; we can replace this with [`httpBlobUpload.Write`](https://github.com/distribution/distribution/blob/main/internal/client/blob_writer.go#L77-L83), which actually sets the necessary Content headers. However this will change how the mirroring is actually done: `Write` creates a new request for each chunk it'll send; in other words, it doesn't stream the data. This means that we will either need a number of requests that depends on the chunk and blob size, or we will need to load the whole blob into memory in order to send with a single `PATCH` request; the former solution will have a clear impact on network performance, while the latter on resource (memory) utilization. If neither solution is preferable for the default operation, this could be done in a configurable way (e.g. with a CLI flag which will enforce buffered mirroring).

3. **Implement a fix for the upstream library.** Since the implementation of the `httpBlobUpload.ReadFrom` method doesn't conform to the [OCI Distribution Spec](https://github.com/opencontainers/distribution-spec/blob/main/spec.md#pushing-a-blob-in-chunks), this could be fixed upstream, and vendored into `oc` once the fix becomes available upstream.
