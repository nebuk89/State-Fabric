# State Fabric

**Warm, forkable workspace state for coding-agent fleets.**

State Fabric v0.1 is an OSS public beta of a protocol that makes three
content-addressed
graphs canonical:

- source state;
- complete workspace state;
- agent provenance.

A signed Authority Journal finalizes transitions between those roots. Git remains
the agent-facing source-control interface; State Fabric is the storage and
durability layer behind it.

The product bet is that a fleet should be able to fork a complete warm workspace
near compute, run without reconstructing it from an origin, and preserve every
divergent result. v0.1 implements content-defined workspace chunks, delta layers,
constant-time metadata forks, safe materialization, and independent edge
durability. Live demand-paged mounts are the next workspace gate.

## Git-native direction

Agents should keep using ordinary Git:

```bash
git clone 'fabric:///absolute/path/to/node?namespace=team-repository'
git fetch origin
git push origin HEAD
```

The current beta includes a first `git-remote-fabric` executable using Git's
supported [remote-helper protocol](https://git-scm.com/docs/gitremote-helpers) to
translate those commands into State Fabric operations. We will not vendor or fork
Git. Correctness will not depend on Git hooks; hooks may later automate optional
local conveniences.

A separate `git-fabric` executable provides
`git fabric checkpoint` and `git fabric status` for Workspace Graph, Provenance
Graph, receipt, and journal semantics that Git cannot represent. Local and
peer-backed SHA-1 clone, fetch, push, checkpoint, and status flows are
process-tested. Direct host-addressed `fabric://` discovery and SHA-256
remote-helper support remain roadmap work.

## How it differs

[Cursor's Git at any scale](https://cursor.com/blog/git-at-any-scale) describes
its published architecture; this table does not imply benchmark parity.

| Dimension | Traditional Git / GitHub | Cursor's published Git-at-scale approach | State Fabric direction |
|---|---|---|---|
| System model | Distributed Git repositories plus a mature centralized collaboration and governance platform | Centralized Git serving: normal repositories on warm NVMe caches, with an S3-compatible write-ahead log as source of truth | Distributed State Fabric nodes behind a standard Git remote helper, with three graphs and a signed journal |
| Agent-facing commands | Normal Git, plus GitHub UI, API, and CLI workflows | Normal Git protocol and clients | Normal `git clone`, `fetch`, and `push`; narrow `git fabric` commands only for non-Git semantics |
| Canonical state | Git commits, trees, blobs, and refs; GitHub holds collaboration metadata separately | Git objects and linearizable refs recorded through the WAL/index | Source, Workspace, and Provenance roots finalized by the Authority Journal |
| Workspace / uncommitted state | Working trees and stashes remain local unless separately uploaded | The article focuses on repository serving, not complete agent working state | Workspace Graph is intended to capture complete, layered filesystem state, including uncommitted state |
| Locality / cache | Every clone is local; GitHub provides mature remote hosting | Warm server-side NVMe repositories are disposable caches materialized and caught up from the object-store WAL | A local node/cache is intended to keep chunked workspace state near agent compute; live demand hydration is unproven |
| Offline durability acknowledgement | Local commits work offline; a push is acknowledged by the configured remote, without a portable signed edge receipt | Pushes are acknowledged after centralized WAL persistence; reads remain consistent with that source of truth | An authorized edge can sign a host-disk receipt while the authority is unavailable, then replicate for finalization |
| Conflict behavior | Non-fast-forward updates are normally rejected; users merge/rebase, with GitHub review and protection policy | Linearizable ref updates use object-store compare-and-swap and retry | Compare-and-set finalization preserves stale concurrent work as signed divergent state for explicit reconciliation |
| Provenance | Author/committer data, signatures, reviews, and CI provide a broad ecosystem | Standard Git metadata; the article does not define a separate agent-provenance graph | Provenance Graph binds agent instructions and evidence to each transition |
| Authority / trust | Git is decentralized; GitHub adds centralized identity, permissions, branch protection, and audit controls | The WAL/index is authoritative and controlled by the hosted service | Signed actors, capabilities, edge receipts, and a single-domain Authority Journal in v0.1 |
| Maturity / ideal use | Industry standard for interoperable source control and broad human/automation collaboration | A published production design optimized for centralized Git-serving and cache scale | Experimental OSS beta for agent fleets needing near-hand workspace state, offline edge receipts, preserved divergence, and explicit provenance |

## See it work

Requires Go 1.24+, Git, and macOS or Linux. Windows cross-process journal
locking is intentionally deferred.

```bash
go run ./cmd/fabric demo
```

The demo creates three isolated nodes and uses HTTP replication over loopback:

1. A chunked workspace is captured, forked with metadata only, and materialized.
2. Two edges accept conflicting agent state while the authority is unavailable.
3. One edge independently accepts and signs durability for the other actor.
4. One edge restarts and proves the acknowledged state survived.
5. Both histories replicate to the authority.
6. One becomes the shared ref; the other is preserved as a divergent head.
7. A two-parent merge transition finalizes and every node converges.
8. The authority verifies every stored object, transition, and receipt.

Expected final line:

```text
PASS: layered workspace forks, independent edge durability, restart recovery, replication, conflict preservation, audit, and convergence are real.
```

## Build

```bash
go test ./...
go build -o bin/fabric ./cmd/fabric
cp bin/fabric bin/git-fabric
cp bin/fabric bin/git-remote-fabric
export PATH="$PWD/bin:$PATH"
bin/fabric version
```

## Current beta binary and roles

```text
fabric CLI/daemon
  - filesystem content-addressed object store
  - public SHA-256 identities
  - private trust-domain opaque identities
  - AES-256-GCM encrypted private payloads
  - Ed25519 actors, capabilities, receipts, and journal records
  - append-only fsynced local and authority journals
  - HTTP or TLS peer replication
  - content-defined workspace chunks and delta layers
  - constant-time metadata forks and safe materialization
  - independent receipt.issue edge capabilities
  - Git source snapshot projection
  - audit, stats, and offline reachability GC
```

Run one process for local use. Run additional instances to create edge,
authority, and cache roles.

## Manual quick start

Initialize an authority:

```bash
fabric init --data .fabric/authority --authority
fabric domain-export --data .fabric/authority --out domain.bundle.json
```

Initialize an edge:

```bash
fabric init --data .fabric/edge --domain domain.bundle.json
fabric status --data .fabric/edge
```

Issue an actor transition capability using the `public_key` from `fabric status`:

```bash
fabric capability-issue \
  --data .fabric/authority \
  --subject '<EDGE_PUBLIC_KEY>' \
  --namespace demo \
  --operations transition.accept \
  --out actor-capability.json
```

Run the authority daemon:

```bash
fabric serve --data .fabric/authority --listen 127.0.0.1:7337
```

Project a Git commit, its tracked blobs, a chunked complete working tree, and
provenance into the three graphs:

```bash
printf '{"task":"explain this repository"}' |
  fabric git-snapshot --data .fabric/edge --repo . --provenance -
```

Use the returned roots with `fabric transition`, then:

```bash
fabric sync --data .fabric/edge --peer http://127.0.0.1:7337 --transition '<TXN_ID>'
```

For independent durability, issue a second node a `receipt.issue` capability,
run its daemon, then ask it to accept the actor's transition:

```bash
fabric accept \
  --data .fabric/actor \
  --peer https://edge.example \
  --transition '<TXN_ID>' \
  --capability edge-receipt-capability.json \
  --ca ./ca.pem
```

The accepting edge can replicate the transition and its own receipt to the
authority. The authority then finalizes through its local CLI or HTTP API.

## Security model

- Public objects use global `obj:sha256:` identities.
- Private objects use HMAC-derived handles scoped to one trust domain.
- Identical private bytes deduplicate inside a domain but do not expose equality
  across domains.
- Private payloads use fresh AES-GCM nonces and encrypted-at-rest storage.
- Transitions require a signed, time-limited namespace capability.
- Peer endpoints require a trust-domain credential and may use TLS 1.3 with
  `serve --tls-cert --tls-key`.
- Capability revocation is journaled and enforced on new acceptance.
- Receipts bind the accepting edge's last observed authority checkpoint, allowing
  legitimate offline work while rejecting writes from edges that observed a
  revocation.

The exported domain bundle is sensitive: it contains shared object and peer keys.
Use TLS outside a trusted host network. Per-node mTLS identity, automated key
distribution, and hardware/KMS-backed secrets are not part of v0.1.

## What v0.1 proves

| Draft 0.1 invariant | v0.1 |
|---|---|
| Immutable object identity | Implemented and tested |
| Journal replay reconstructs refs | Implemented and tested |
| Finalization binds all three roots | Implemented and tested |
| Acknowledged host-disk state survives restart | Implemented and tested |
| Concurrent ref conflicts never silently lose state | Implemented and tested |
| Private handles do not reveal cross-domain equality | Implemented and tested |
| Unauthorized peers cannot read private endpoints | Implemented with domain peer credentials |
| Revoked capabilities cannot authorize transitions | Implemented and tested |
| Git adapter preserves supported tracked bytes and blob identities | Implemented and tested |
| Local SHA-1 Git clone/fetch/push over `fabric://` | Implemented and process-tested |
| Workspace chunks survive edits with cross-layer reuse | Implemented and tested |
| Workspace forks avoid copying payload bytes | Implemented and tested |
| A distinct authorized edge can issue durability for an actor | Implemented and tested across OS processes |
| Node contents can be audited and unreachable aged objects identified | Implemented and tested |
| TLS peers can use an explicit CA | Implemented and tested |
| No incumbent service dependency | Implemented |

## What v0.1 does not yet prove

- live demand-paged filesystem mounts;
- multi-host latency, cache-hit, or origin-byte targets;
- acceptable reconciliation cost with many concurrent agents;
- per-node mTLS identity, automated retention policy, or KMS-backed keys.
- tracked Git blobs larger than 32 MiB; the adapter rejects them explicitly
  until source-object streaming lands.
- Git history bundles larger than 32 MiB.
- direct host-addressed `fabric://` discovery and SHA-256 remote-helper flows.
- Git tag publication and ref deletion; v0.1 publishes commit-backed branch refs
  only.

This is an OSS public beta, not a production security or availability claim. See
[docs/protocol.md](docs/protocol.md) for exact scope and deferred work.

## Documentation

- [v0.1 protocol and architecture](docs/protocol.md)
- [public beta operations](docs/beta.md)
- [security and trust domains](docs/security.md)
- [three-node demo](docs/demo.md)
- [product roadmap and prioritized backlog](docs/roadmap.md)

## License

Apache License 2.0. See [LICENSE](LICENSE).
