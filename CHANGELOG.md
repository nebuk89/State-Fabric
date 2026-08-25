# Changelog

## Unreleased

- Added `git-remote-fabric`, allowing unmodified SHA-1 `git clone`, `git fetch`,
  and `git push` against local State Fabric nodes and peer-backed local caches.
- Added `git fabric checkpoint` and `git fabric status` for workspace durability
  semantics with no faithful Git equivalent.
- Added bounded Git history bundles so clones preserve complete reachable commit
  history without forking or vendoring Git. Stable serialized bundle refs keep
  repeated captures of unchanged history content-address identical.
- Added session-stable fetch advertisements and compare-and-set push expectations,
  including stale forced-push protection.
- Added incremental closure-first peer refresh: validate the signed journal
  extension, fetch only its new transition suffix, then publish authority refs.
- Limited v0.1 Git publication to commit-backed branch refs; tags are rejected
  explicitly rather than being silently rewritten.

## v0.1.0 - Public beta

- Added content-defined workspace chunks and layered delta manifests.
- Added constant-time metadata workspace forks and safe materialization.
- Added independent edge durability using namespace-scoped `receipt.issue`
  capabilities.
- Added process-isolated actor, edge, and authority integration coverage.
- Added optional TLS 1.3 serving and explicit client CA trust.
- Added authenticated operational stats, full node audit, and offline
  reachability garbage collection.
- Added persisted journal-head rollback detection, exact journal-to-receipt
  audit binding, and transport-safe graph-object limits.
- Added authority-journal reconciliation for independent accepting edges.
- Added macOS/Linux CI, race testing, release archives, checksums, security
  guidance, and beta operations documentation.

## v0.0.0 - Protocol proof

- Initial three-graph model, signed Authority Journal, durable self-acceptance,
  conflict preservation, Git projection, private trust domains, replication, and
  three-node convergence demo.
