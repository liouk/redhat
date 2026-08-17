## Migrating oc from distribution/distribution v3 (2023-05-19) to v3.1.1

### 1. Import path changes

| Old package | Status in v3.1.1 | Files affected |
|---|---|---|
| `reference` | Extracted to `github.com/distribution/reference` (already in go.mod) | 7 files: `append/{append,scratch}.go`, `imagesource/{dryrun,s3,file}.go`, `manifest/manifest.go`, `mirror/mirror.go` |
| `registry/client` | Moved to `internal/client` | 3 files: `append/append.go`, `imagesource/dryrun.go`, `mirror/mirror.go` |
| `registry/client/auth` | Moved to `internal/client/auth` | 3 files: `imagesource/s3.go`, `manifest/dockercredentials/credential_store_factory.go`, `mirror/mappings.go` |
| `registry/client/transport` | Moved to `internal/client/transport` | 1 file: `registry/info/info.go` |
| `manifest/schema1` | Removed entirely | 3 files: `mirror/mirror.go`, `manifest/manifest.go`, `helpers/image/test/util.go` |

### 2. The `internal/` blocker — client is deliberately deprecated

The core client packages (`registry/client`, `registry/client/auth`, `registry/client/auth/challenge`, `registry/client/transport`) were moved to `internal/` in v3.1.1. This was intentional:

- [Issue #4110](https://github.com/distribution/distribution/issues/4110) — the tracking issue for v3.0.0 API cleanup, where maintainers discussed switching consumers to containerd's client
- [PR #4126](https://github.com/distribution/distribution/pull/4126) — the internalization PR, stating: *"Our registry client is not currently in a good place to be used as the reference OCI Distribution client implementation."*
- [v3.0.0 release notes](https://github.com/distribution/distribution/releases/tag/v3.0.0) — *"`client` is no longer supported as a standalone package."*

distribution/distribution v3 is now a **server-only project**; no migration guide was published.

Go enforces the `internal` visibility rule even in `vendor/` — external modules cannot import these packages. This affects 7 files across oc. Workarounds:

- **Replace the registry client** with a different library (largest effort, cleanest result):
  - [`containerd/containerd/remotes/docker`](https://github.com/containerd/containerd) — the containerd registry client, uses a `Resolver` → `Fetcher`/`Pusher` abstraction (not a drop-in replacement for distribution's `Repository` → `BlobStore` → `BlobWriter` pattern). Docker/Moby is also [migrating to this](https://github.com/moby/moby/issues?q=is%3Aopen+is%3Aissue+label%3Acontainerd-integration).
  - [`google/go-containerregistry`](https://github.com/google/go-containerregistry) — standalone OCI registry client, no daemon dependency
  - [`oras-project/oras-go`](https://github.com/oras-project/oras-go) — OCI Distribution Spec client
- **Copy the client code into oc** — inline the needed client packages directly. This is what [Harbor did](https://github.com/goharbor/harbor/pull/21896) (still a draft PR; Harbor maintainers are "very reluctant to bump distribution v3").
- **Fork v3.1.1** and re-export `internal/client` as `registry/client` — moderate effort, creates a permanent fork to maintain.
- **Vendor with `internal/` renamed** — patch the vendor tree to move `internal/client` back to `registry/client` — smallest effort, but a large vendor patch.

### 3. library-go dependency

`openshift/library-go` imports `registry/client/auth/challenge` (in `pkg/image/registryclient/client.go`). That library must be migrated first or in parallel, since oc vendors it.

### 4. schema1 removal

`manifest/schema1` is actively used in oc — not dead code. `manifest.go` has schema1<->schema2 conversion logic (`convertToSchema2`, `convertToSchema1`), `mirror.go` handles `*schema1.SignedManifest` in a type switch, and `test/util.go` references `schema1.MediaTypeManifest`. Removing schema1 means dropping Docker Image Manifest V2 Schema 1 support and deleting the conversion paths. This may break compatibility with very old registries.

### 5. Effort estimate

**XL.** This is not a version bump — it's a registry client replacement. The migration requires choosing and integrating a new client library (containerd, go-containerregistry, or oras-go), rewriting ~15 files in oc, coordinating a library-go update, and removing schema1 support with backwards-compatibility implications. Estimated at 2-4 weeks of focused work including testing, not counting upstream coordination.

### 6. Prior art

A previous attempt to bump `distribution/v3` (commit `70b058825`, OCPBUGS-7465) was **reverted** (`808d66f99`), confirming this has been tried before and proved problematic.
