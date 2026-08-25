# State Fabric

**Warm, forkable workspace state for coding-agent fleets.**

State Fabric v0.1 is an OSS public beta of a protocol that makes three
content-addressed
graphs canonical:

- source state;
- complete workspace state;
- agent provenance.

A signed Authority Journal finalizes transitions between those roots. Git is a
compatibility adapter rather than the internal storage model.

The product bet is that a fleet should be able to fork a complete warm workspace
near compute, run without reconstructing it from an origin, and preserve every
divergent result. v0.1 implements content-defined workspace chunks, delta layers,
constant-time metadata forks, safe materialization, and independent edge
durability. Live demand-paged mounts are the next workspace gate.

This repository contains a real single-binary implementation of the v0.1
semantics. It does not require Kubernetes, an external database, a blockchain,
GitHub, Cursor, or a proprietary persisted-filesystem service.

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
bin/fabric version
```

## One binary, multiple roles

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
  - Git source projection
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
