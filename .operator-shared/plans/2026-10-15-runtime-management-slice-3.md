# Runtime Management Slice 3 (Object Inspection) Implementation Plan

> TDD plan for `.operator-shared/specs/runtime-management-commands.md` §Slice 3.
> Ticket: #171 (child of #165). Branch: `issue-171-runtime-management-slice-3`.

**Goal:** add read-only `volume inspect`, `image inspect`, and `sandbox inspect`
commands under the private runtime home, each with the shared output conventions
(default human block, `--format json`, `-q`).

**Architecture:** each owning package exposes an `Inspect*` seam returning a
package-local detail DTO; the CLI resolves the Slice 0 output mode and renders
through a shared `printInspect` helper (identifier / JSON / aligned key/value
block). Two `msb` seam additions made inspection possible without disturbing the
existing config-only paths.

**Seams under test:** the package `Inspect*` functions (unit tests in
`volume`/`image`/`vm`) and the CLI commands (integration tests in
`cmd/agents-sandbox`, human + JSON + quiet + error paths).

## Global constraints

- Follow `AGENTS.md`: idiomatic Go, non-abbreviated identifiers, `termio` for all
  output, small focused files. Never `go vet`; use `make fmt`/`make lint`.
- Scope every command to the private runtime home; never touch `~/.microsandbox`.
- `-q` on an inspect command prints the object identifier only; `--format json`
  and `--quiet` stay mutually exclusive.

## Seam additions

- `msb.SandboxHandle.ID()` — stable identity, added to the interface, the real
  adapter, the mock, and covered by a unit test.
- `msb.Client.ImageInspectDetail(ctx, ref) (*msb.ImageDetail, error)` — a local
  `ImageDetail` (reference/digest/size/created + `msbSdk.ImageConfig` + layers),
  implemented over the existing `realImageInspect` seam. The config-only
  `ImageInspect` stays as-is (the build digest check still uses it).

## Tasks

1. **msb seams** — `ID()`, `ImageInspectDetail`, mocks, fail-fast, unit tests.
2. **`volume.InspectVolume`** — DTO + `GetVolume` mapping; unit tests for
   populated and missing volumes.
3. **`image.InspectImage`** — DTO + layer mapping; unit tests for populated,
   unknown layer sizes, and missing images.
4. **`vm.InspectSandbox`** — DTO, mount-kind/source mapping, guest-path sort;
   unit tests for populated, config-error, and missing sandboxes.
5. **CLI** — `inspect_output.go` (shared `printInspect` + DTOs/renderers),
   `commands_inspect.go` (three builders), wired into the image/volume/sandbox
   groups; `cli_inspect_test.go` covers human/JSON/quiet/conflict/unknown-format/
   missing/required-args for all three.
6. **Help/tree/tests + docs + CHANGELOG** — help leaf/subcommand lists, the
   `docs/commands.md` sections, and the `[Unreleased]` entry.

## Decisions

- Quiet = identifier only (matches the list convention).
- JSON identity/size fields are presentation-oriented human strings (matching the
  list JSON); collections are arrays; nullable fields render `-`.
- `ImageInspectDetail` is separate from `ImageInspect` to avoid widening a seam
  the build pipeline depends on; `SandboxHandle.ID` is the first use of the SDK
  identity in the seam.

## Verification

`make fmt`, `make lint`, `make test`, `make compile-all`, and `make check`
(includes `ci/check-docs.sh`).
