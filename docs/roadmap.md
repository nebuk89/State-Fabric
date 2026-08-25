# State Fabric Product Roadmap

## The bet

State Fabric should not win by replacing Git. It should make the **complete agent
workspace** a portable, forkable, content-addressed object while agents continue
to use ordinary Git commands.

The narrow product promise is:

> For teams running fleets of coding agents, State Fabric forks complete warm
> workspaces near compute so agents start without re-cloning or re-installing,
> continue through partitions, and never silently lose a divergent result.

The first user is a coding-agent platform team running many tasks per repository.
That team controls its runtime, has repeated state, and pays origin and startup
costs. Its agents should use `git clone`, `git fetch`, and `git push` against
`fabric://` origins while the platform adopts additional State Fabric semantics
deliberately.

## Git integration decision

- Use Git's supported remote-helper extension through `git-remote-fabric`.
- Do not vendor or fork the Git binary.
- Do not make Git hooks a correctness boundary; hooks may become optional
  convenience automation.
- Keep clone, fetch, and push in the normal Git CLI.
- Keep `git-fabric` narrow: `git fabric checkpoint` and `git fabric status`
  cover State Fabric semantics with no faithful Git equivalent.

The local node/cache, accepting edge receipt, and Authority Journal form the
durability path behind the helper.

## Positioning

State Fabric is not a claim that GitHub should be replaced: Git and GitHub remain
the stronger default for ecosystem interoperability, human collaboration,
governance, and operational maturity. Cursor's
[Git at any scale](https://cursor.com/blog/git-at-any-scale) describes a different
strength: centralized Git serving at scale using normal Git repositories as warm
NVMe caches over an authoritative object-store write-ahead log.

State Fabric is exploring the layer those systems do not target directly:
near-agent complete workspace state, signed offline edge receipts, explicit
divergence preservation, and agent provenance. Its performance and production
economics remain unproven.

## What exists

The OSS v0.1 public beta proves:

- immutable Source, Workspace, and Provenance graph roots;
- node-local signed host-disk durability receipts;
- crash and restart recovery;
- signed Authority Journal replay;
- authenticated HTTP replication;
- deterministic conflict preservation and merge convergence;
- public and trust-domain-private object identities;
- Git SHA-1 and SHA-256 snapshot projection;
- content-defined workspace chunks and layered delta manifests;
- constant-time metadata forks and safe workspace materialization;
- independent edge receipts authorized with `receipt.issue`;
- process-isolated actor, edge, and authority operation over TCP;
- TLS peers with explicit CA trust;
- full node audit, operational stats, and offline reachability GC.

It does not yet prove the product's differentiated value on a real fleet. There
is no live demand-paged mount, the process proof still runs on one host, and no
representative workload has demonstrated lower task latency, origin traffic, or
cost.

## v0.1 implementation progress

| Backlog area | Status |
|---|---|
| Layered workspace manifest and content-defined chunks | Implemented and tested |
| Metadata-only fork and copy-on-write delta capture | Implemented and tested |
| Safe workspace materialization | Implemented and tested |
| Independent actor and accepting-edge identities | Implemented and tested across OS processes |
| Delegated `receipt.issue` capability | Implemented and tested |
| TLS server and explicit client CA | Implemented and tested |
| Audit, stats, and offline reachability GC | Implemented and tested |
| Git source bundle capture/import primitives | Implemented and tested |
| Local SHA-1 `git-remote-fabric` clone/fetch/push | Implemented and process-tested |
| Peer-backed cache hydration for Git clone/fetch/push | Implemented and process-tested over HTTP |
| `git-fabric` checkpoint/status commands | Implemented and process-tested |
| Direct host-addressed discovery and SHA-256 remote-helper support | Not started |
| Live demand hydration and read-byte telemetry | Not started |
| Multi-host fault campaign | Not started |
| Durability-class evidence beyond host-disk | Not started |
| Fleet contention and reconciliation study | Not started |
| Design-partner workload and economics | Not started |

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
- Specify and test the `git-remote-fabric` ref/object mapping without modifying
  Git or relying on hooks.
- Make and record the G0 decision.
- Integrate the metrics into a repeatable benchmark harness in this repository.

### Days 31-60: prove warm-workspace value

- Integrate the layered chunk graph and metadata forks into a real agent runtime.
- Add demand-paged hydration and read-byte accounting.
- Run a real agent task against a 50+ GiB logical workspace.
- Demonstrate the G1 target and run edge/authority/cache on separate hosts for G2.

### Days 61-90: prove distributed trust and concurrency

- Run independent actor/edge receipts across separate physical hosts.
- Extend durability evidence beyond the current signed `host-disk` class.
- Build power-loss and kill-the-host durability tests.
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

### P1 - Preserve ordinary Git workflows

| ID | Backlog item | Acceptance criteria |
|---|---|---|
| GIT-01 | Implement `git-remote-fabric` clone | `git clone fabric://...` produces a valid checkout through Git's remote-helper protocol |
| GIT-02 | Implement fetch and push | Unmodified `git fetch` and `git push` exchange refs and objects with deterministic conflict preservation |
| GIT-03 | Bind pushes to State Fabric transitions | Source, Workspace, and Provenance roots reach an accepting edge receipt and Authority Journal without a required hook |
| GIT-04 | Add compatibility coverage | SHA-1 and SHA-256 repositories pass clone/fetch/push process tests with supported Git versions |
| GIT-05 | Define narrow State Fabric commands | Only semantics Git cannot express are considered for `git fabric checkpoint/status` |

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

## Out of scope by design

- vendoring or forking the Git binary;
- requiring Git hooks for correctness;
- replacing normal clone, fetch, or push with proprietary agent commands.

## Explicitly deferred

Do not spend roadmap capacity on these until G0-G4 pass:

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
- presenting a functional public beta as proof of real-fleet performance;
- hiding State Fabric-only semantics inside surprising Git behavior.

**Continue**

- durability and no-silent-loss invariants;
- falsifiable hypotheses and explicit kill criteria;
- OSS reproducibility with standard Git extension points;
- provider independence as strategic leverage, not headline value.

**Start**

- measuring a real fleet before adding protocol surface;
- completing and validating the `git-remote-fabric` path;
- building the layered, forkable workspace graph;
- testing multi-host behavior and independent-edge receipts;
- quantifying reconciliation burden and unit economics.
