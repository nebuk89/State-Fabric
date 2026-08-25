# Public Beta Operations

State Fabric v0.1 is suitable for design-partner evaluation on macOS and Linux.
It is not a production availability or security claim.

## Install

Download the archive for your platform from the GitHub prerelease, verify its
adjacent SHA-256 file, and place `fabric` on `PATH`.

Build from source:

```bash
go test ./...
go build -trimpath -o bin/fabric ./cmd/fabric
bin/fabric version
```

## Workspace lifecycle

Capture a complete workspace as content-defined chunks:

```bash
fabric workspace-capture \
  --data .fabric/edge \
  --dir ./working-tree \
  --private
```

Create a constant-time metadata fork:

```bash
fabric workspace-fork \
  --data .fabric/edge \
  --parent '<WORKSPACE_ROOT>'
```

Capture a delta against a parent:

```bash
fabric workspace-capture \
  --data .fabric/edge \
  --dir ./modified-tree \
  --parent '<PARENT_ROOT>' \
  --private
```

Materialize a root into a new directory:

```bash
fabric export-workspace \
  --data .fabric/edge \
  --workspace '<WORKSPACE_ROOT>' \
  --out ./materialized
```

Materialization refuses an existing destination and validates paths, entry types,
chunk links, object identities, and logical file sizes.

## Independent edge durability

The authority issues separate capabilities:

```bash
fabric capability-issue \
  --data .fabric/authority \
  --subject '<ACTOR_PUBLIC_KEY>' \
  --namespace demo \
  --operations transition.accept \
  --out actor-capability.json

fabric capability-issue \
  --data .fabric/authority \
  --subject '<EDGE_PUBLIC_KEY>' \
  --namespace demo \
  --operations receipt.issue \
  --out edge-capability.json
```

After the actor creates a transition, the edge independently accepts it:

```bash
fabric accept \
  --data .fabric/actor \
  --peer https://edge.example \
  --transition '<TXN_ID>' \
  --capability edge-capability.json \
  --ca ./ca.pem
```

The actor verifies and stores the returned receipt. The accepting edge can then
replicate the closure and its own receipt to the authority.

## TLS

Create or obtain a certificate whose SAN matches the peer hostname. Run:

```bash
fabric serve \
  --data .fabric/edge \
  --listen 0.0.0.0:7337 \
  --tls-cert ./server.crt \
  --tls-key ./server.key
```

Clients use `--ca ./ca.pem` on `accept`, `sync`, `pull-authority`, and remote
`stats`. TLS and the domain peer credential are both required by this setup.
v0.1 does not yet provide per-node mTLS identities.

## Audit and capacity

```bash
fabric stats --data .fabric/edge
fabric verify --data .fabric/edge
fabric gc --data .fabric/edge --grace 168h
```

`gc` is a dry run unless `--apply` is present. Destructive GC must run with the
daemon stopped. Every object reachable from any persisted transition is retained.
Untransitioned snapshots become eligible only after the grace period, so choose a
grace longer than the maximum publish delay.

## Beta safety rules

- Back up node data before destructive GC.
- Keep domain bundles and capability files at mode `0600`.
- Use short capability lifetimes.
- Use TLS outside a trusted host network.
- Do not expose a domain peer credential to untrusted nodes.
- Treat divergent refs as work requiring explicit policy or merge.
- Run `fabric verify` after repair, restore, or manual file operations.
- Preserve the Authority Journal; snapshots of refs are not authoritative.

## Known limits

- one authority per trust domain;
- one ref mutation per transition;
- no Windows cross-process journal lock;
- no live demand-paged filesystem mount;
- no per-node mTLS, KMS/HSM integration, or automated key rotation;
- no semantic merge engine;
- GC is offline and policy-light;
- tracked Git blobs are limited to 32 MiB and larger blobs fail explicitly;
- canonical graph-object envelopes are limited to 48 MiB so every stored object
  remains replicable over the v0.1 JSON transport;
- latency and economics have not yet been validated on a real fleet.
