# couchcli

CLI (`couch`) for discovering and controlling networked home devices — TVs, audio gear, lights, smart home devices, anything remote-controllable on the LAN.

## Commands

- Build: `go build ./...`
- Test: `go test ./...`
- Vet: `go vet ./...`

## Architecture

- `internal/device` — driver-neutral domain vocabulary: value types, capability interfaces, shared errors. Imports no driver or protocol packages.
- `internal/discovery` — sweeps the network once with generic scanners (`mdns`, `ssdp` subpackages, no brand knowledge); drivers implement `discovery.Matcher` to claim their devices from the shared findings as `device.Candidate`s.
- Capabilities, not device categories, are the unit of design. There is no "TV" or "light" type — a device is whatever set of capabilities its driver implements, and commands target one capability each.
- `internal/philips` — pure JointSpace protocol client. Speaks protocol vocabulary (keys, endpoints), knows nothing about the domain layer.
- Drivers adapt protocol clients to `device` capabilities. Dependencies point inward: `cmd` → `device` ← drivers → protocol clients. Never the reverse.
- `cmd` — cobra commands. Talks to `device` interfaces, not protocol clients.

## Go style

- Define interfaces where they are consumed, keep them small and single-purpose. Accept interfaces, return concrete types.
- Prove interface satisfaction at compile time: `var _ device.MediaController = (*Device)(nil)`.
- `context.Context` is always the first parameter.
- Use modern stdlib: `wg.Go(...)` over `wg.Add`/`defer wg.Done`, `slices`/`maps` helpers, `netip` over `net.IP`.
- Wrap errors with `%w` and context about the operation; callers branch with `errors.Is`/`errors.As`. Sentinel errors live in the package that defines the vocabulary (e.g. `device.ErrUnsupported`).
- Brand-specific features stay in their driver; don't invent generic abstractions with one implementation.
- Table-driven tests. Test logic-bearing code; don't test pure data types.

## Comment style

Comments must state something the code cannot: a constraint, an invariant, a protocol quirk, a why. If deleting the comment loses no information, delete it.

- No comments that restate the name, the signature, or the next line. A trivial exported method whose behavior is obvious from its name (`String()`, a getter) needs no doc comment.
- Doc comments on exported symbols earn their place by adding semantics: units, valid ranges, fallback behavior, what callers should do with an error.
- Never write comments addressed to a reviewer — nothing about what a change does, why it's correct, or where code came from.
- Package docs: a few lines on what the package is and its one load-bearing rule, not a tour of its contents.
- Prefer one line. If a comment needs a second paragraph, it's usually two facts — check that both are real.
