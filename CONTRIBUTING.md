# Contributing

State Fabric is an experimental public beta. Contributions should preserve its
core invariants: immutable identity, explicit authority, no silent state loss,
and fail-closed verification.

## Development

Requires Go 1.24+ and Git.

```bash
gofmt -w cmd internal
go test ./...
go test -race ./...
go vet ./...
go build -o bin/fabric ./cmd/fabric
go run ./cmd/fabric demo
```

Add focused tests for durability boundaries, malformed input, restart behavior,
cross-process behavior, and private/public identity semantics. Do not weaken
validation to make incompatible state load successfully.

## Pull requests

- Explain the user or operator problem.
- State which protocol invariant changes.
- Include compatibility and migration impact.
- Add or update tests and public documentation.
- Keep dependencies minimal and justify any new one.

Report security issues through the process in [SECURITY.md](SECURITY.md).
