# State Fabric

**Save, fork, and move the whole working state of a coding agent, not just its Git commits.**

## The problem

A coding agent needs more than your repo. It needs a whole working machine: the
checked out code, the files it hasn't committed yet, and everything it took to
get productive, like installed dependencies, build output, and search indexes.

Right now every agent task rebuilds all of that from nothing. It clones the repo,
runs `npm install` or `bazel build`, rebuilds its indexes, and warms up. That is
slow and it costs money, and it happens over and over, once per task, across a
whole fleet of agents. Git looks after your commits, but it knows nothing about
the rest of the working state, so none of it can be saved, shared, or reused.

It gets worse when several agents touch the same repo at once. Their results
collide, whoever pushes last wins, and the other agent's work quietly disappears.

## What State Fabric does

State Fabric saves an agent's whole working state as content addressed objects.
That gives you four things.

**Start a new agent from a warm machine, right next to your compute.** Instead of
cloning and reinstalling, the agent forks a workspace that already has the code,
the uncommitted changes, and the dependencies in place, and it does that in about
the same time no matter how big the workspace is.

**Keep working when the network drops.** A machine you have authorized can accept
work and sign for it even when the central authority is unreachable, then catch
up later. Once work is acknowledged, it survives a crash or a restart.

**Never quietly lose a result.** If two agents end up with different results,
State Fabric keeps both and marks them so someone can sort it out on purpose.
There is no last writer wins and nothing gets thrown away.

**See who did what.** Every change stores the agent's instructions and its
evidence next to the code, so you can audit an automated change later.

Underneath, State Fabric keeps three sets of content addressed objects: the
source (your Git history), the whole workspace (the full filesystem, including
uncommitted files), and the provenance (the agent's instructions and evidence). A
signed, append only journal acts as the referee. It decides the official state
and records every change, so nobody can quietly rewrite history. Journals are a
well understood idea on their own; the difference here is what this one tracks,
which is the whole workspace and its provenance, not only commits.

Git stays exactly the way agents already use it. State Fabric is the storage and
durability layer behind Git, not a replacement for it.

## The demo

https://github.com/user-attachments/assets/b27ae889-14da-48e7-93a9-b91a2d519e7e

One Git workflow, two machines, durable state. This is a real run. It starts with
an ordinary `git push` on this host, then a separate remote Linux machine clones
the repo, changes it, and pushes back, and finally a brand new local cache pulls
the result down. Both sides check, on their own, that they hold the same 15
objects, 2 signed transitions, and 2 durability receipts. The agents only ever
run plain Git.

## Agents keep using plain Git

Nobody has to learn a new source control tool. Point Git at a `fabric://` remote
and use the same commands you always use:

```bash
git clone 'fabric:///absolute/path/to/node?namespace=team-repository'
git fetch origin
git push origin HEAD
```

A small `git-remote-fabric` helper plugs into Git's built in
[remote helper protocol](https://git-scm.com/docs/gitremote-helpers) and turns
those commands into State Fabric operations. We do **not** fork or vendor Git,
and nothing depends on Git hooks to work correctly.

A separate `git-fabric` command covers the few things Git cannot express on its
own: `git fabric checkpoint` and `git fabric status` for workspace, provenance,
receipt, and journal state.

## Where this is today

Being straight with you: this is an early open source **public beta**. The big
payoff, instant forks of huge warm workspaces streamed on demand and a proven
speed and cost win on a real fleet, is **not demonstrated yet**. What v0.1 does
prove is the foundation it stands on: workspace content split into reusable
chunks, forks that copy metadata only, signed durability receipts that survive
restarts, conflicts that are preserved rather than lost, and plain Git clone,
fetch, and push over `fabric://` (SHA-1, on local and peer backed caches). Live
on demand hydration and multi machine performance are what we work on next. See
[docs/roadmap.md](docs/roadmap.md) for the exact gates and the open work.

## How it compares

This puts State Fabric next to two things people already know. The middle column
describes what [Cursor wrote about running Git at scale](https://cursor.com/blog/git-at-any-scale),
which is their published design.

| Question | Plain Git and GitHub | Cursor's Git at scale (as published) | State Fabric |
|---|---|---|---|
| How is it put together? | Everyone has a full copy of the repo, and GitHub adds a mature place to collaborate and govern | A central service that serves ordinary Git repos from fast local caches, backed by a log in S3 that is the source of truth | Separate State Fabric nodes sitting behind a standard Git remote helper, tracking three sets of objects with a signed journal |
| What do agents type? | Normal Git, plus GitHub's website, API, and CLI | Normal Git | Normal `git clone`, `fetch`, and `push`, with a couple of small `git fabric` commands only for things Git cannot say |
| What counts as the official state? | Git commits, trees, blobs, and branches; GitHub keeps collaboration data separately | Git objects and branch updates written through the central log | The source, whole workspace, and provenance, made official by the signed journal |
| Does it track uncommitted work? | No. Your working files and stashes stay on your machine unless you upload them yourself | Not the focus; it serves repositories | Yes. The workspace is meant to hold the entire filesystem, including files you haven't committed |
| Is state kept near the agent? | Every clone is local, and GitHub hosts the remote copy | Warm caches on fast local disks, refilled from the central log | A local node or cache is meant to keep workspace content close to the agent. Streaming it on demand is not proven yet |
| Can you keep working offline? | Local commits work offline, but a push is only acknowledged by the remote, with no portable signed proof | A push is acknowledged once it lands in the central log | An authorized machine can sign a proof to its own disk while the authority is unreachable, then sync it up later |
| What happens when two changes collide? | The second push is usually rejected, and you merge or rebase, with GitHub reviews and rules | Branch updates use compare and swap and retry | The stale change is not thrown away. It is signed and kept as a separate result for someone to reconcile on purpose |
| Is there a record of who did what? | Author and committer info, signatures, reviews, and CI | Standard Git metadata; no separate record for agents | Every change is tied to the agent's instructions and evidence |
| Who is in charge of trust? | Git is decentralized; GitHub adds accounts, permissions, branch protection, and audit | The central log is authoritative and run by the hosted service | Signed identities, scoped permissions, disk proofs, and one signed journal per trust domain in v0.1 |
| How mature is it, and who is it for? | The industry standard for source control and for people and tools working together | A production design tuned for serving Git at scale from caches | An experimental open source beta for agent fleets that want workspace state close by, offline proofs, no lost results, and a record of what each agent did |

## See it work

You need Go 1.24 or newer, Git, and macOS or Linux. Windows is not supported yet,
because the cross process journal locking it needs is still to come.

```bash
go run ./cmd/fabric demo
```

The demo spins up three separate nodes on your machine and talks between them over
loopback HTTP:

1. It captures a workspace, forks it by copying only metadata, and materializes it.
2. Two machines accept conflicting agent work while the authority is offline.
3. One machine accepts and signs durability for the other on its behalf.
4. One machine restarts and shows the acknowledged work is still there.
5. Both histories sync to the authority.
6. One becomes the shared branch; the other is kept as a separate result.
7. A merge with two parents finalizes, and every node ends up agreeing.
8. The authority rechecks every stored object, transition, and receipt.

You should see this final line:

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

## What the beta binary can do

One `fabric` binary is both the command line tool and the daemon. It gives you:

```text
- a content addressed object store on the filesystem
- public SHA-256 identities for shared objects
- private, opaque identities scoped to a single trust domain
- AES-256-GCM encryption for private data
- Ed25519 signatures for identities, permissions, receipts, and journal records
- append only, fsynced journals for the local node and the authority
- peer replication over HTTP or TLS
- workspace content split into reusable chunks with layered deltas
- forks that copy metadata only, and safe materialization
- permission to let one machine sign durability on another's behalf (receipt.issue)
- snapshots of Git source into the object store
- audit, stats, and cleanup of unreachable old objects
```

Run a single process for local use. Run more copies to play the edge, authority,
and cache roles.

## Manual quick start

Set up an authority:

```bash
fabric init --data .fabric/authority --authority
fabric domain-export --data .fabric/authority --out domain.bundle.json
```

Set up an edge:

```bash
fabric init --data .fabric/edge --domain domain.bundle.json
fabric status --data .fabric/edge
```

Give the edge permission to accept transitions, using the `public_key` shown by
`fabric status`:

```bash
fabric capability-issue \
  --data .fabric/authority \
  --subject '<EDGE_PUBLIC_KEY>' \
  --namespace demo \
  --operations transition.accept \
  --out actor-capability.json
```

Start the authority daemon:

```bash
fabric serve --data .fabric/authority --listen 127.0.0.1:7337
```

Snapshot a Git commit, its tracked files, the whole working tree, and some
provenance into the three sets of objects:

```bash
printf '{"task":"explain this repository"}' |
  fabric git-snapshot --data .fabric/edge --repo . --provenance -
```

Take the roots it returns, create a transition with `fabric transition`, then sync
it to the authority:

```bash
fabric sync --data .fabric/edge --peer http://127.0.0.1:7337 --transition '<TXN_ID>'
```

To have a second machine sign durability on the actor's behalf, give it a
`receipt.issue` permission, start its daemon, and ask it to accept the actor's
transition:

```bash
fabric accept \
  --data .fabric/actor \
  --peer https://edge.example \
  --transition '<TXN_ID>' \
  --capability edge-receipt-capability.json \
  --ca ./ca.pem
```

That machine can then send the transition and its own receipt up to the
authority, which finalizes it through its local command line or HTTP API.

## Security model

- Shared objects use global `obj:sha256:` identities.
- Private objects use HMAC based handles that only mean something inside one
  trust domain.
- Identical private bytes are stored once inside a domain, but two domains can't
  tell whether they hold the same bytes.
- Private data is encrypted at rest, with a fresh AES-GCM nonce each time.
- A transition needs a signed permission that names a namespace and expires.
- Talking to a peer needs a trust domain credential, and can use TLS 1.3 with
  `serve --tls-cert --tls-key`.
- Revoking a permission is written to the journal and enforced the next time
  someone tries to accept work.
- A receipt records the last authority checkpoint the machine had seen, so
  genuine offline work is allowed but a machine that already saw a revocation is
  turned away.

One thing to be careful with: the exported domain bundle is sensitive, because it
holds shared object and peer keys. Use TLS outside a trusted network. Per machine
mTLS identity, automatic key distribution, and keys held in hardware or a KMS are
not in v0.1.

## What v0.1 proves

| Claim | Status in v0.1 |
|---|---|
| Object identities never change | Done and tested |
| Replaying the journal rebuilds the branches | Done and tested |
| Finalizing a change ties together all three roots | Done and tested |
| Acknowledged on disk work survives a restart | Done and tested |
| Two colliding changes never silently lose work | Done and tested |
| Private handles don't reveal matches across domains | Done and tested |
| Unauthorized peers can't read private endpoints | Done, using domain peer credentials |
| Revoked permissions can't authorize a change | Done and tested |
| The Git adapter keeps tracked bytes and blob identities intact | Done and tested |
| Local SHA-1 Git clone, fetch, and push over `fabric://` | Done and process tested |
| Workspace chunks survive edits and get reused across layers | Done and tested |
| Forks don't copy the actual file bytes | Done and tested |
| A second authorized machine can sign durability for an actor | Done and tested across OS processes |
| You can audit a node and spot unreachable old objects | Done and tested |
| TLS peers can use an explicit CA | Done and tested |
| No dependency on any incumbent service | Done |

## What v0.1 does not prove yet

- streaming a workspace's files on demand as it is read;
- real numbers for latency, cache hits, or bytes pulled from origin across machines;
- how expensive reconciliation gets with many agents at once;
- per machine mTLS identity, automatic retention policy, or keys held in a KMS;
- tracked Git files bigger than 32 MiB, which the adapter refuses for now until
  it can stream large source objects;
- Git history bundles bigger than 32 MiB;
- finding a `fabric://` node by host address, and the SHA-256 remote helper path;
- publishing Git tags or deleting branches; v0.1 only publishes branches backed
  by a commit.

This is an open source public beta, not a promise about production security or
uptime. See [docs/protocol.md](docs/protocol.md) for the exact scope and what is
deferred.

## Documentation

- [v0.1 protocol and architecture](docs/protocol.md)
- [public beta operations](docs/beta.md)
- [security and trust domains](docs/security.md)
- [three node demo](docs/demo.md)
- [product roadmap and backlog](docs/roadmap.md)

## License

Apache License 2.0. See [LICENSE](LICENSE).
