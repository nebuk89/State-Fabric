# State Fabric v0.1 Protocol

## Canonical model

State Fabric has three immutable graphs and one signed journal:

```mermaid
flowchart LR
    A["Coding agent"] --> G["Normal Git CLI<br/>clone · fetch · push"]
    G --> H["git-remote-fabric"]
    H --> N["Local State Fabric<br/>node / cache"]
    N --> S["Source Graph"]
    N --> W["Workspace Graph"]
    N --> P["Provenance Graph"]
    S --> E["Accepting edge receipt"]
    W --> E
    P --> E
    E --> J["Authority Journal"]
```

Git is projected into the Source Graph. Complete filesystem state is projected
into the Workspace Graph. Agent instructions and evidence live in the
Provenance Graph.

## Git extension boundary

`fabric://` is designed as a Git remote transported by `git-remote-fabric`.
When Git encounters that URL, its supported remote-helper mechanism delegates
clone, fetch, and push without changing the Git binary. State Fabric will neither
vendor nor fork Git.

The helper is responsible for mapping Git refs and objects to the Source Graph
and for coordinating the related State Fabric transition. The local node/cache,
accepting edge receipt, and Authority Journal remain the correctness path. Git
hooks are not part of that path; future hooks may only provide optional
automation.

State Fabric semantics with no faithful Git representation are exposed by the
`git-fabric` executable as `git fabric checkpoint` and
`git fabric status`. This extension is intentionally narrow: normal source
control continues through normal Git commands.

The current beta process-tests remote-helper clone, fetch, and push plus
checkpoint and status against both a local authority node and a local cache
hydrated from an HTTP authority peer. The helper currently requires an absolute
local node/cache path and a SHA-1 repository. Direct host-addressed
`fabric://` discovery and SHA-256 helper flows are not yet implemented.

Remote-helper advertisements are stable for the lifetime of a helper session.
Fetches use the advertised Source Graph root even if the authority ref advances,
and pushes use the advertised transition as their compare-and-set expectation.
This preserves Git's force-with-lease safety across a concurrent authority
update. v0.1 only publishes commit-backed destinations under `refs/heads/`;
deletions and tag destinations are rejected explicitly.

## Canonical bytes

v0.1 uses `fabric-json-v0`:

- UTF-8 JSON;
- fixed struct field order;
- no maps, floats, unknown fields, or insignificant whitespace;
- HTML escaping disabled;
- byte slices use standard JSON base64 encoding;
- sorted and unique link, parent, operation, and ref lists.

SHA-256 identities and Ed25519 signatures cover these exact bytes. Golden-vector
tests protect encoding stability.

## Objects

Public:

```text
obj:sha256:<digest of canonical GraphObject>
```

Private:

```text
priv:<trust-domain>:1:<HMAC(domain identity key, canonical digest)>
```

Objects are written using temp-file, file fsync, atomic rename, and directory
fsync before they may contribute to a durability receipt.

## Transition

A signed transition binds:

- namespace and trust domain;
- causal parent transitions;
- expected prior ref;
- Source, Workspace, and Provenance roots;
- required-object manifest;
- one v0.1 ref intent;
- actor public key;
- signed capability;
- optional policy context.

The edge verifies the transition ID, actor signature, capability, manifest, and
complete recursive object closure before persisting the transition.

## Edge receipt

The signed receipt binds:

- transition ID;
- accepting node;
- acceptance time;
- durability class;
- object manifest.
- the latest Authority Journal checkpoint observed by the edge.

v0.1 advertises `host-disk`. It means all required objects, the transition, the
receipt, and the accepted journal event were fsynced before the receipt was
returned.

The actor may self-accept locally. A distinct edge may instead accept on the
actor's behalf when the authority has issued that edge a namespace-scoped
`receipt.issue` capability. The receipt binds the acceptance-capability ID,
accepting node identity, durability class, object manifest, and latest observed
Authority Journal checkpoint.

## Authority journal

The journal is append-only canonical JSONL. Every record includes:

- sequence;
- previous record ID;
- event type;
- namespace/ref/transition;
- result;
- authority signature.

Ref state is rebuilt only from journal replay. Snapshots are not authoritative.
Each journal also persists its expected head in a separately fsynced sidecar.
Reloads must extend that checkpoint, so replacing the JSONL with an older valid
signed prefix is rejected. Hostile rollback of both files requires an external
witness or hardware-backed monotonic checkpoint and remains deferred.

## Conflict behavior

Finalization performs a compare-and-set against the transition's expected ref.
The per-node finalization lock serializes the check and journal append.

If the expected value is stale:

- the transition is not overwritten or deleted;
- a signed `divergent` record is appended;
- a stable `refs/divergent/.../<transition-prefix>` name is created;
- a later merge transition may reference both parents.

Wall-clock timestamps never select a winner.

## Replication

`fabric sync` recursively sends:

1. causal parent transitions and their receipts;
2. each complete required-object closure;
3. the requested transition and its durability receipt.

The receiving node recalculates every object and transition identity, verifies
signatures and capability, and only then persists the state.

Edges mirror the authority journal as a verified prefix. A history fork is
rejected rather than silently replaced. Peer refresh validates the candidate
journal extension first, reuses transitions covered by the verified local
prefix, and fetches every transition and object closure in the new suffix before
publishing that journal locally. A partial pull therefore cannot expose refs
whose content is absent.

## Explicit v0.1 limits

- One authority per trust domain.
- One `set` ref intent per transition.
- Cross-process journal locking is implemented on macOS and Linux; Windows is
  deferred.
- Peer transport uses a shared domain credential. TLS 1.3 with an explicit CA is
  supported, but per-node mTLS identity is deferred.
- The current Git adapter snapshots and exports SHA-1 or SHA-256 repositories,
  while `git-remote-fabric` clone/fetch/push currently supports SHA-1 and requires
  an absolute local node/cache path. That cache can hydrate from an authenticated
  HTTP or TLS authority peer. A forked Git binary and a separate full Git
  smart-protocol server are not required by the architecture.
  Tracked blobs larger than 32 MiB are rejected before snapshot publication
  because v0.1 does not yet stream source objects.
- Git history bundles larger than 32 MiB are rejected before publication.
  Bundle capture is streamed and aborted once that limit is crossed.
- `git-remote-fabric` v0.1 publishes commit-backed branch destinations only.
  Tag publication and ref deletion are deferred.
- Every canonical graph-object envelope is capped at 48 MiB so locally accepted
  objects always fit within the authenticated v0.1 JSON replication transport.
- Git and workspace paths must be valid UTF-8 in v0; unsupported paths are
  rejected rather than lossy-normalized.
- Workspace snapshots use content-defined chunks and layered persisted-filesystem
  roots; live demand-paged mounts are not yet implemented.
- Offline reachability GC retains every object referenced by a persisted
  transition and only selects unreachable objects older than an explicit grace
  period. Run destructive GC with the node daemon stopped.
- No live demand-paged mount, placement prediction, transparency witnesses, or
  semantic merge engine.
