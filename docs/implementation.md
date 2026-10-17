# Implementation status

The initial executable covers all Meta command groups in the original design. This document describes the narrower implemented contract where the design included future behavior. `adsctl commands`, each command's `--help`, and `adsctl meta schema get` describe the compiled interface.

| Area | Implemented | Limits |
| --- | --- | --- |
| Credentials | External environment/file references, read/write profiles, app-secret proof | No OAuth, token refresh or permission introspection; auth check is an account read |
| Installation and CLI home | Checksummed macOS/Linux release installer; `~/.adsctl` or `ADSCTL_HOME` for config and state; config-relative secret files | Windows uses ZIP/manual PATH setup; legacy locations remain selectable with explicit flags |
| Discovery | Resources, business owned/client accounts, targeting | No combined business relationship query; Page listing follows credential identity access |
| Native Insights | Sync query, attribution, breakdowns, filters, pagination | Provider validates combinations and availability |
| Async Insights | Create/get/wait/results; account membership checked | Explicit jobs; higher-level reports currently use synchronous requests |
| Reports | Account summary and separate campaign contributors, period comparison, rank, breakdown, export, bounded portfolio workers | Compare requires entity totals without breakdowns; no currency aggregation or inferred missing values |
| Diagnostics | Account/ad issues, activity, provider recommendations | No automatic recommendation application; diagnostics are limited to exposed fields |
| Monitoring | Strict rule documents, exact thresholds, evidence gates, currency checks, completed dates, conversion buffer, local transitions, portfolios | One-shot; no notifications or scheduler; incomplete evidence leaves state unchanged |
| Snapshots | Campaign/ad set/ad/creative configuration, scoped complete diffs | Inventory is not a transactional provider snapshot; no inferred deletion from incomplete data |
| Mutations | Create/update/delete, pause/activate/archive, budgets, creative references, receipts | Supported native field subset; nested provider conditions are not exhaustively modeled |
| Validation | Local types/required fields, explicit ownership/parent reads, planned dependencies | Meta validation-only is not called; missing future parent validation is deferred |
| Media | Streaming images and checkpointed resumable videos; scoped video get/wait | Local 1 GiB file bound; video auto mode is resumable at every size, explicit single mode available; bounded transfer recovery, ambiguous start/finish reconciliation, configurable request/total deadlines |
| Launch | Template, plan/apply/status/resume/reconcile/activate, local run listing | Starter template is traffic/image; observed-field matching normalizes provider set ordering and creative name suffixes, and reports the drifting field path; image reconciliation verifies membership, not original byte identity |
| Output | JSON, JSONL records, generic tables, CSV export, errors/exits | Fetch results are buffered within bounds; generic tables have no colors |
| State | Private files, atomic replacement, local OS file locking | Persistent execution requires macOS/Linux; no distributed coordination or cross-directory deduplication |
| API versions | Version per profile and override, reported metadata | v25.0 compatibility target; no live or cross-version certification |
| Google | Reserved architectural extension | No compiled Google commands |

## Verification

`make check` runs formatting checks, `go vet`, pinned golangci-lint, race-enabled tests with coverage, and a binary build. GitHub Actions performs the equivalent checks. Tests use local fixtures; no tests require credentials or change remote advertising.

Behavior exercised includes:

- Native numeric precision, header authentication and secret redaction.
- Scoped pagination, overfull pages, foreign next URLs and bounded read retries.
- Attribution/date validation, explicit action metrics and zero/missing evidence.
- Rejection of global rankings from truncated inputs.
- Explicit write scope, cross-account denial, paused creation defaults, budget currency/ownership and observed amount verification.
- Unknown create outcomes, no automatic POST retry and applied-unverified receipts.
- Multipart uploads, strict documents, snapshot completeness and exclusive locks.
- Launch dependency resolution, file hashes, duplicate local apply prevention, known-ID resume, explicit reconciliation and child-before-parent activation.
- Stable JSON/JSONL envelopes and control-character-safe table cells.

Coverage is generated in `coverage.out`; inspect it with `go tool cover -func=coverage.out`. Coverage is informational rather than an arbitrary pass threshold. Provider behavior still needs a live account smoke test.

## Live acceptance checklist

Use explicitly provisioned test assets and write credentials when intentionally exercising writes. This checklist is release validation work, not a claim that it has run.

1. Check account access, version response, currency/timezone and supporting asset visibility.
2. Confirm one-page and continued reads, synchronous Insights, attribution and a completed async job.
3. Compare account summary and an export with Ads Manager using matching dates, attribution and fields.
4. Create paused resources with the supported template and check normalized read fields, preview, media processing and account membership.
5. Interrupt after a known ID; resume without duplication. Exercise an unknown outcome only in a controlled fixture or test setup.
6. Review activation output and intentionally activate recorded test objects; verify configured/effective status separately from delivery.
7. Check permissions, unavailable breakdowns, throttling and invalid field errors retain the documented classifications.

## Extension points

Cobra dispatch and workflow orchestration live under `internal/cli`; pure query, schema, reporting, monitoring and launch-plan logic has separate packages. The Graph transport owns authentication, pacing, bounded reads, redaction and non-retry semantics. Local state owns private serialization and locking. A future Google adapter should use its own native schemas and vocabulary while sharing envelopes, configuration selection and explicit account scope.

The module path is currently local (`adsctl`). Set the canonical repository module path before publishing an installable remote Go module. No release or remote push is implied by this implementation.
