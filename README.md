# State Fabric

**Agent-native distributed state, accepted nearby and verified everywhere.**

State Fabric is an OSS v0 of a protocol that makes three content-addressed
graphs canonical:

- source state;
- complete workspace state;
- agent provenance.

A signed Authority Journal finalizes transitions between those roots. Git is a
compatibility adapter rather than the internal storage model.

This repository contains a real single-binary implementation of the v0
semantics. It does not require Kubernetes, an external database, a blockchain,
GitHub, Cursor, or SV3.

## See it work

Requires Go 1.24+, Git, and macOS or Linux. Windows cross-process journal
locking is intentionally deferred.

```bash
go run ./cmd/fabric demo
```

The demo creates three isolated nodes and uses real HTTP replication:

1. Two edges accept conflicting agent state while the authority is unavailable.
2. Each edge returns a signed, fsynced durability receipt.
3. One edge restarts and proves the acknowledged state survived.
4. Both histories replicate to the authority.
5. One becomes the shared ref; the other is preserved as a divergent head.
6. A two-parent merge transition finalizes.
7. Every node mirrors the signed authority journal and converges.

Expected final line:

```text
PASS: nearby durability, restart recovery, replication, conflict preservation, and convergence are real.
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
  - HTTP peer replication
  - Git and complete-workspace snapshot adapter
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

Issue the edge a capability using the `public_key` from `fabric status`:

```bash
fabric capability-issue \
  --data .fabric/authority \
  --subject '<EDGE_PUBLIC_KEY>' \
  --namespace demo \
  --out capability.json
```

Run the authority daemon:

```bash
fabric serve --data .fabric/authority --listen 127.0.0.1:7337
```

Project a Git commit, its tracked blobs, the complete working tree, and
provenance into the three graphs:

```bash
printf '{"task":"explain this repository"}' |
  fabric git-snapshot --data .fabric/edge --repo . --provenance -
```

Use the returned roots with `fabric transition`, then:

```bash
fabric sync --data .fabric/edge --peer http://127.0.0.1:7337 --transition '<TXN_ID>'
```

The authority can finalize the transition through its local CLI or HTTP API.

## Security model

- Public objects use global `obj:sha256:` identities.
- Private objects use HMAC-derived handles scoped to one trust domain.
- Identical private bytes deduplicate inside a domain but do not expose equality
  across domains.
- Private payloads use fresh AES-GCM nonces and encrypted-at-rest storage.
- Transitions require a signed, time-limited namespace capability.
- Peer HTTP endpoints require a trust-domain credential.
- Capability revocation is journaled and enforced on new acceptance.
- Receipts bind the accepting edge's last observed authority checkpoint, allowing
  legitimate offline work while rejecting writes from edges that observed a
  revocation.

The exported domain bundle is sensitive: it contains v0 shared object and peer
keys. Bind the daemon only to a trusted network. Production TLS, key
distribution, delegated cache capabilities, and hardware/KMS-backed secrets are
not part of v0.

## What v0 proves

| Draft 0.1 invariant | v0 |
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
| No incumbent service dependency | Implemented |

This is an OSS v0, not a production security or availability claim. See
[docs/protocol.md](docs/protocol.md) for exact scope and deferred work.

## Documentation

- [v0 protocol and architecture](docs/protocol.md)
- [security and trust domains](docs/security.md)
- [three-node demo](docs/demo.md)

## License

Apache License 2.0. See [LICENSE](LICENSE).
