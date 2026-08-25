# Security and Trust Domains

## Public versus private

Privacy is part of addressing, not an authorization check added after storage.

Public objects have global content identities and may be openly replicated.
Private objects have domain-scoped opaque handles and encrypted payloads.

Two trust domains storing identical private plaintext produce different handles.
Two nodes in the same domain produce the same handle and can deduplicate.

## Keys in v0

An authority creates:

- a domain identity key for private handles;
- a domain encryption key for private object payloads;
- a peer credential for the HTTP replication API;
- an Ed25519 authority key for capabilities and journal records.

Every node also has an independent Ed25519 actor identity.

`fabric domain-export` creates a sensitive edge bundle containing the shared
domain object and peer keys, but not the authority private signing key.

## Offline revocation semantics

Each durability receipt commits to the last Authority Journal record observed by
the accepting edge.

- If that checkpoint includes a capability revocation, acceptance is rejected.
- If the edge was genuinely offline before revocation, its receipt remains
  eligible until the capability's signed expiry.
- The authority verifies the checkpoint is part of its own journal before
  ingesting or finalizing the transition.

This is deliberate partition behavior: revocation stops connected nodes
immediately and bounds disconnected nodes by short capability lifetimes. v0
does not provide trusted hardware time or prove that a compromised edge reported
its newest checkpoint.

## Capabilities

Capabilities are signed by the authority and bind:

- trust domain;
- namespace;
- subject public key;
- sorted operations;
- issue and expiry time;
- delegation depth.

v0 transitions require `transition.accept`. Capability revocations are signed
authority-journal events and apply to subsequent transition acceptance.

## Threats covered by v0

- object mutation under an existing ID;
- private plaintext at rest;
- cross-domain private equality probing by object handle;
- forged actors, transitions, receipts, capabilities, and journal records;
- stale ref overwrite;
- journal truncation/fork import;
- unauthenticated peer API access;
- acknowledged state disappearing after a normal restart.

## Deferred production controls

- TLS/mTLS and workload identity;
- KMS/HSM-backed keys;
- per-cache attenuated read capabilities;
- key rotation and cryptographic deletion workflows;
- secure-time or hardware-attested authority checkpoints;
- hostile-machine and memory-extraction protection;
- Byzantine authority consensus or transparency witnesses;
- process-kill and power-loss certification for every filesystem/platform.
