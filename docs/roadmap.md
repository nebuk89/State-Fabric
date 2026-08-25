# State Fabric Product Roadmap

## The bet

State Fabric should not win by being a marginally faster Git remote. It should
win by making the **complete agent workspace** a portable, forkable,
content-addressed object.

The narrow product promise is:

> For teams running fleets of coding agents, State Fabric forks complete warm
> workspaces near compute so agents start without re-cloning or re-installing,
> continue through partitions, and never silently lose a divergent result.

The first user is a coding-agent platform team running many tasks per repository.
That team controls its runtime, has repeated state, pays origin and startup costs,
and can integrate a native client without waiting for Git UX to represent
asynchronous finality.

## What exists

The OSS v0 proves:

- immutable Source, Workspace, and Provenance graph roots;
- node-local signed host-disk durability receipts;
- crash and restart recovery;
- signed Authority Journal replay;
- authenticated HTTP replication;
- deterministic conflict preservation and merge convergence;
- public and trust-domain-private object identities;
- Git SHA-1 and SHA-256 snapshot projection.

It does not yet prove the product's differentiated value. Workspace snapshots are
whole-file objects, receipt signer and transition actor are the same node, and the
demo runs on one host. No real workload has yet demonstrated lower task latency,
origin traffic, or cost.

## Decision gates

Each phase must earn the right to fund the next one.

| Gate | Question | Pass condition | Failure response |
|---|---|---|---|
| G0: problem | Is state reconstruction large enough to matter? | At least one representative fleet shows state reconstruction at 10% or more of task wall-clock, or locality has a credible path to meaningful cost savings | Stop, or narrow to a smaller durability/provenance product |
| G1: workspace | Can the fabric materially improve startup? | Fork a 50+ GiB workspace in under 1 second and cut p95 time-to-first-test by at least 50% against honest baselines | Reconsider the workspace-graph architecture |
| G2: network | Does nearby durability work on real hosts? | p95 accepted host-disk transition below 250 ms in-rack with zero acknowledged-state loss under crash tests | Narrow topology or durability promise |
| G3: trust | Can a distinct edge credibly accept for an actor? | Receipt signer differs from actor; durability class is verifiable; kill-the-host tests lose zero acknowledged work | Limit the product to single-node/local durability |
| G4: concurrency | Is asynchronous finality usable at fleet scale? | Deterministic convergence with 10+ agents and an acceptable measured reconciliation burden | Add stronger policy/serialization or narrow concurrent-write claims |
| G5: economics | Is the fabric worth operating? | Latency and egress savings exceed storage, replication, and operations cost at target density | Do not broaden deployment |

## 30/60/90-day plan

### Days 0-30: prove the problem

- Secure one design partner operating a real coding-agent fleet.
- Instrument task wall-clock into state acquisition, dependency hydration, model
  inference, tool execution, testing, and finalization.
- Record origin bytes, workspace bytes, cache reuse, task density, and cost.
- Benchmark against cold clone/install, Git partial clone, and the partner's
  existing warm-cache approach.
- Make and record the G0 decision.
- Integrate the metrics into a repeatable benchmark harness in this repository.

### Days 31-60: prove warm-workspace value

- Replace whole-file workspace snapshots with a layered extent/chunk graph.
- Add constant-time copy-on-write workspace forks.
- Add demand-paged hydration and read-byte accounting.
- Run a real agent task against a 50+ GiB logical workspace.
- Demonstrate the G1 target and run edge/authority/cache on separate hosts for G2.

### Days 61-90: prove distributed trust and concurrency

- Separate actor identity from accepting-edge identity.
- Issue receipts from an independent edge with an explicit durability class.
- Build crash-boundary and kill-the-host durability tests.
- Run 10+ concurrent agents against one namespace/ref.
- Measure divergent heads, automated resolutions, human interventions, and time
  to convergence.
- Make and record G3 and G4 decisions.
- Put one real partner pipeline stage on State Fabric with north-star telemetry.

## North-star metric

**Percentage of agent tasks served from nearby state, paired with p95
time-to-first-test improvement.**

A cache hit without a user-visible speed or cost improvement is not success.

Supporting metrics:

| Metric | Why it matters |
|---|---|
| State reconstruction share of wall-clock | Validates the problem |
| Workspace fork latency | Measures the differentiated primitive |
| Hydrated bytes / logical workspace bytes | Measures demand-hydration efficiency |
| Origin bytes and egress cost per task | Measures infrastructure value |
| p95 durable-acceptance latency | Measures locality |
| Acknowledged-state loss | Must remain zero |
| Reconciliation actions per 100 transitions | Measures asynchronous-finality UX |
| Cost per 1,000 tasks | Tests economic viability |

## Prioritized backlog

Priority is ordered by user value and risk reduction, not by protocol layer.

### P0 - Validate the problem

| ID | Backlog item | Acceptance criteria |
|---|---|---|
| VAL-01 | Recruit one agent-fleet design partner | Named owner, representative repository, workload sample, data-access agreement, and weekly review cadence |
| VAL-02 | Build task-cost instrumentation | Breaks task time into state, inference, tools, tests, and finalization; records bytes and cost; adds less than 2% overhead |
| VAL-03 | Define honest baselines | Repeatable cold clone/install, partial clone, and existing warm-cache measurements |
| VAL-04 | Run the G0 study | At least 100 representative tasks; distributions and confidence bounds reported; explicit go/narrow/stop decision |
| VAL-05 | Publish benchmark harness | One command reproduces workload setup, run, metrics, and comparison output |

### P1 - Deliver the killer workspace primitive

| ID | Backlog item | Acceptance criteria |
|---|---|---|
| WSP-01 | Define layered workspace manifest | Canonical extent/chunk schema, sparse files, modes, symlinks, deletions, and parent-layer semantics specified with golden vectors |
| WSP-02 | Implement content-defined chunking | Stable chunk identities under local edits; benchmarked CPU and deduplication tradeoffs |
| WSP-03 | Implement constant-time fork | A 50+ GiB logical workspace forks in under 1 second without copying payload bytes |
| WSP-04 | Implement copy-on-write mutation | Modified extents create a new root while unchanged extents retain identity |
| WSP-05 | Implement demand hydration | Agent task reads fetch only required chunks; missing/corrupt chunks fail explicitly |
| WSP-06 | Add hydration telemetry | Reports logical bytes, fetched bytes, cache level, misses, and time-to-first-test |
| WSP-07 | Run G1 benchmark | At least 50% lower p95 time-to-first-test than the best honest baseline |

### P1 - Prove real topology

| ID | Backlog item | Acceptance criteria |
|---|---|---|
| NET-01 | Create multi-host test environment | Edge, cache, and authority run on separate hosts with controllable latency and partitions |
| NET-02 | Add network fault harness | Reproducible loss, delay, partition, process crash, and host kill scenarios |
| NET-03 | Measure nearby acceptance | p50/p95/p99 latency and fsync cost reported by durability class |
| NET-04 | Run G2 durability campaign | Zero acknowledged-state loss across crash boundaries; p95 below 250 ms in-rack |

### P1 - Make receipts independent

| ID | Backlog item | Acceptance criteria |
|---|---|---|
| RCP-01 | Separate actor and accepter roles | Protocol permits a distinct authorized edge to validate, persist, and sign a receipt for an actor transition |
| RCP-02 | Define durability-class evidence | Receipt binds storage class and verifiable policy; downgrade and forgery tests fail closed |
| RCP-03 | Add delegated acceptance capability | Authority scopes which edges may accept for which actors/namespaces and durability classes |
| RCP-04 | Add kill-the-edge test suite | Every acknowledgement boundary is followed by process and host failure; zero acknowledged work lost |
| RCP-05 | Run G3 review | Threat model and external security review approve the independent-edge claim |

### P2 - Make asynchronous finality usable

| ID | Backlog item | Acceptance criteria |
|---|---|---|
| CON-01 | Build N-agent contention simulator | Replays 2, 10, 50, and 100 writers with configurable partitions and ref policies |
| CON-02 | Add reconciliation policies | Fast-forward, deterministic merge queue, task-scoped refs, and explicit human escalation supported |
| CON-03 | Build divergence UX/API | Operators can explain every head, cause, actor, and next action without reading raw journals |
| CON-04 | Measure reconciliation burden | Reports automated resolutions and human actions per 100 transitions |
| CON-05 | Run G4 evaluation | Deterministic convergence and partner-approved reconciliation burden |

### P2 - Operate private workspaces safely

| ID | Backlog item | Acceptance criteria |
|---|---|---|
| OPS-01 | Implement reachability and retention GC | Storage remains bounded under churn; live roots and policy holds are never collected |
| OPS-02 | Implement cryptographic erasure | Domain/key rotation and deletion receipts are testable without claiming deletion of public replicas |
| OPS-03 | Replace shared peer credential | Per-node mTLS identity and least-privilege authorization; compromised edge cannot read the entire domain by default |
| OPS-04 | Add KMS-backed key provider | Private signing and domain keys can be externalized without changing object semantics |
| OPS-05 | Add observability | Metrics, structured logs, traces, health, repair state, and capacity alerts cover every acknowledgement path |
| OPS-06 | Add repair and audit tooling | Operator can verify closure, replay journal, diagnose divergence, and repair from an authorized replica |

### P3 - Integrate the first real workload

| ID | Backlog item | Acceptance criteria |
|---|---|---|
| INT-01 | Define a minimal agent-runtime SDK | Create/fork/hydrate/transition/accept/finalize APIs with Go reference client and protocol fixtures |
| INT-02 | Integrate one pipeline stage | One design-partner task type runs on State Fabric without changing its task semantics |
| INT-03 | Add rollout controls | Shadow mode, fallback, per-repository enablement, and no-loss rollback path |
| INT-04 | Run G5 economics review | Cost per 1,000 tasks and saved wall-clock/egress support continued investment |

## Explicitly deferred

Do not spend roadmap capacity on these until G0-G4 pass:

- full Git smart-protocol server;
- Git LFS and submodule breadth;
- multi-authority consensus;
- regional placement prediction;
- transparency witnesses;
- semantic merge engines;
- broad desktop-developer UX;
- provider-specific integrations unrelated to the first design partner.

## Stop / continue / start

**Stop**

- leading with the protocol instead of a fleet operator's task latency and cost;
- describing v0 self-acceptance as independent nearby-node durability;
- expanding generic Git compatibility before the native agent workflow works.

**Continue**

- durability and no-silent-loss invariants;
- falsifiable hypotheses and explicit kill criteria;
- one-binary OSS reproducibility;
- provider independence as strategic leverage, not headline value.

**Start**

- measuring a real fleet before adding protocol surface;
- building the layered, forkable workspace graph;
- testing multi-host behavior and independent-edge receipts;
- quantifying reconciliation burden and unit economics.
