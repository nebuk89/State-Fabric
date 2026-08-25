# Three-Node Demo

Run:

```bash
go run ./cmd/fabric demo
```

The command creates an authority and two edge nodes in isolated directories.
They share a private trust domain but have independent actor identities.
Before partitioning, each edge receives the base transition and mirrors the
authority checkpoint. Replication later traverses the complete causal transition
closure.

## Sequence

```mermaid
sequenceDiagram
    participant A as Edge A
    participant B as Edge B
    participant C as Authority

    C->>C: finalize base
    Note over A,B: authority unavailable
    A->>A: accept transition + fsync receipt
    B->>B: accept conflicting transition + fsync receipt
    A->>A: restart and replay
    A->>C: replicate object closure + transition
    B->>C: replicate object closure + transition
    C->>C: finalize A; preserve B as divergent
    A->>C: replicate two-parent merge
    C->>C: finalize merge
    C-->>A: signed authority log
    C-->>B: signed authority log
```

The demo fails immediately if:

- a receipt references an incomplete object closure;
- a transition or receipt signature fails;
- restart loses acknowledged state;
- replication changes an object ID;
- either concurrent history disappears;
- authority journal mirroring forks;
- the nodes disagree after the merge.

Use `--dir` to preserve the generated state for inspection:

```bash
go run ./cmd/fabric demo --dir ./demo-state
go run ./cmd/fabric refs --data ./demo-state/edge-a
```
