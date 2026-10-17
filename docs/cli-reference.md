# adsctl: CLI UX and command reference

This document records the original proposed interface. The initial executable now implements the Meta command groups, with the precise current limits listed in [implementation status](implementation.md). Use `adsctl commands` and command `--help` for compiled flags. Stages refer to the [architecture document](design.md). Examples use placeholder IDs; API payload examples illustrate shape and still require account/version-specific validation.

## 1. Command grammar and discoverability

Use plural resource names, a verb, then a positional object ID:

```text
adsctl meta campaigns list
adsctl meta campaigns get CAMPAIGN_ID
adsctl meta campaigns update CAMPAIGN_ID --file update.json
```

Use `adsets` to match Meta's familiar API spelling. Commands and flags use kebab-case; native JSON fields retain Meta's snake_case. IDs are strings. Accept bare account digits or `act_123` for `--account`, normalize to `act_123`, and return both the selected CLI account and native response fields unchanged. Other IDs are passed as IDs without invented prefixes. Writes never resolve names automatically.

`adsctl` with no arguments prints root help and exits 0. Invoking an incomplete group such as `adsctl meta campaigns` prints that group's help and exits 2. `--help` always works without config, secrets, or network. Help is text; data-producing commands default to JSON even in a TTY. Help and completion never fetch account names or secrets automatically.

```text
adsctl help meta insights query
adsctl commands --platform meta --capability read
adsctl commands --platform meta --capability write
adsctl meta schema get campaigns --operation create
adsctl meta schema get insights --operation query
adsctl docs --format markdown
adsctl completion zsh
adsctl version
```

`commands` emits command paths, arguments, flags, output schema, stage and semantic effect (`local`, `read`, `report-job`, `write`). It lists commands actually compiled into the binary, not the full roadmap. Schema output includes API version, supported fields, enums, examples and known conditional requirements; Meta remains the final validator. `docs` produces a compact agent reference from the same metadata. No remote MCP server is needed for agents that can invoke shell commands.

## 2. Complete proposed command tree

```text
adsctl
├── version
├── commands
├── docs
├── completion <bash|zsh|fish|powershell>
├── config
│   ├── path
│   ├── validate
│   └── profiles
│       ├── list
│       ├── get NAME
│       ├── set NAME
│       └── delete NAME
├── meta
│   ├── auth check
│   ├── schema get RESOURCE
│   ├── accounts <list|get ID>
│   ├── businesses <list|get ID>                         [stage 2]
│   ├── businesses accounts list BUSINESS_ID            [stage 2]
│   ├── campaigns <list|get ID>
│   ├── campaigns <create|validate>                      [stage 3/4]
│   ├── campaigns <update|pause|activate|archive|delete> ID
│   ├── campaigns budget set ID
│   ├── adsets <list|get ID>
│   ├── adsets <create|validate>
│   ├── adsets <update|pause|activate|archive|delete> ID
│   ├── adsets budget set ID
│   ├── ads <list|get ID>
│   ├── ads <create|validate>
│   ├── ads <update|pause|activate|archive|delete|preview> ID
│   ├── ads creative set ID
│   ├── creatives <list|get ID>
│   ├── creatives <create|validate>
│   ├── creatives <update|delete|preview> ID
│   ├── images <list|upload>
│   ├── videos <list|upload|get ID|wait ID>
│   ├── pages <list|get ID>
│   ├── instagram-accounts <list|get ID>
│   ├── pixels <list|get ID>
│   ├── audiences <list|get ID>
│   ├── targeting search
│   ├── insights
│   │   ├── query
│   │   └── jobs <create|get ID|wait ID|results ID>
│   ├── reports <summary|compare|rank|breakdown|export>
│   ├── issues list
│   ├── activity list
│   ├── recommendations list
│   ├── snapshots <create|diff>
│   ├── monitor <validate|check>
│   ├── launch
│   │   ├── template
│   │   ├── validate
│   │   ├── plan
│   │   ├── apply
│   │   ├── status RUN_ID
│   │   ├── resume RUN_ID
│   │   ├── reconcile RUN_ID
│   │   └── activate RUN_ID
│   └── runs <list|get RUN_ID>
└── google ...                                           [future]
```

Resource mutations belong to stages 3/4; preview/media/supporting-asset discovery arrives as needed for stage 4. Insights jobs/reports belong to stage 1B. Issues/activity/recommendations/snapshots/monitoring/business discovery belong to stage 2. Pages/Instagram/pixels/audiences are discovery only initially. `images` identifies uploaded media by returned hashes; do not invent an image-ID CRUD interface. Video get/wait handles processing state.

## 3. Shared flags and input rules

| Flag | Meaning/default |
| --- | --- |
| `--config PATH` | Explicit non-secret config file |
| `--profile NAME` | Credential profile; required explicitly for remote advertising writes |
| `--account ID` | Selected account; required for account-scoped operations and explicitly for writes |
| `--api-version VERSION` | Explicit override of the profile version; schema/plan records it |
| `--output json\|jsonl\|table` | JSON default; JSONL only for commands advertising streaming support |
| `--timeout 2m` | Total operation deadline; increase explicitly for long waits |
| `--request-timeout 30s` | Per-request limit, also bounded by total deadline |
| `--verbose` | Redacted request diagnostics on stderr |
| `--color auto\|always\|never` | Human output only; auto uses TTY detection |
| `--fields FIELD,...` | Resource/query field projection on read commands |
| `--page-size 100` | Requested provider page size; actual size may differ |
| `--cursor TOKEN` | Continue the same scoped query |
| `--all` | Fetch all pages, still subject to bounds |
| `--max-items 10000` | Bound total rows for `--all`; reaching the bound marks incomplete |
| `--file PATH` | Native JSON payload for resource operations; `-` means stdin |
| `--dry-run` | Mutation plan without advertising changes |

Flags apply only where relevant. `--api-version` is scoped to Meta. A `--file` payload cannot be combined with flags writing the same payload fields; report the conflict instead of guessing precedence. Reject unknown write fields in the supported adapter schema. Read fields can pass through unknown names to Meta so new read fields do not require immediate CLI updates; label schema coverage accordingly.

Native request files are JSON. Workflow/rule/config documents are versioned YAML or JSON. No executable template expressions, shell expansion or arbitrary environment substitution inside payloads. Files resolve relative to the referring manifest; secret references live only in profile configuration. Exactly one command may consume stdin.

## 4. Setup and account discovery

Provision token values through the runner's environment or mounted secrets. The CLI stores references:

```sh
adsctl config profiles set agency-read --platform meta \
  --token-env META_AGENCY_READ_TOKEN --api-version v25.0 --mode read

adsctl config profiles set client-b --platform meta \
  --token-file /run/secrets/client-b-token --api-version v25.0 --mode read

adsctl config profiles get agency-read
adsctl config validate
adsctl meta auth check --profile agency-read --account act_123
adsctl meta accounts list --profile agency-read --all --output table
adsctl meta accounts get act_123 --profile agency-read
```

Profile reads display secret source names/paths, never values. Updating a profile requires explicit fields; switching token source removes the prior reference atomically. Deleting a profile removes only local config and fails if a portfolio still references it; it does not revoke the remote token. Missing secrets produce a configuration error naming the missing reference.

`accounts list` discovers accessible accounts, not accounts owned by the identity. Business-specific discovery later distinguishes `--relationship owned|client|all` and reports permission limitations. Never equate a business listing failure with loss of direct account access.

## 5. Inspect configuration

```sh
adsctl meta campaigns list --profile agency-read --account act_123 \
  --effective-status ACTIVE,PAUSED --fields id,name,objective,status,effective_status

adsctl meta adsets list --profile agency-read --account act_123 --campaign 456
adsctl meta ads list --profile agency-read --account act_123 --adset 789
adsctl meta ads get 101 --profile agency-read --account act_123 \
  --fields 'id,name,status,effective_status,creative{id,name},adset_id,campaign_id'
adsctl meta creatives get 202 --profile agency-read --account act_123
```

Collection scoping uses parent edges where supported. `ads list --campaign` and `--adset` are mutually exclusive. A get by ID verifies membership when an account scope is supplied; it never retries against another account. Default field sets are small, documented in schema, and include IDs/names/status and parent IDs where available. Native Graph field-expansion syntax is preserved as a single fields expression.

`issues list` collects account availability and available resource issue/review/effective-status fields. A reported issue is distinct from an adsctl request error. `activity list --since ... --until ...` reads available provider change history. `recommendations list` returns eligible Meta recommendations, labeled as provider suggestions rather than guaranteed improvements; no automatic application.

```sh
adsctl meta issues list --profile agency-read --account act_123
adsctl meta activity list --profile agency-read --account act_123 \
  --since 2026-10-01T00:00:00+07:00 --until 2026-10-09T00:00:00+07:00
adsctl meta recommendations list --profile agency-read --account act_123
```

## 6. Query performance

```sh
adsctl meta insights query --profile agency-read --account act_123 \
  --level campaign --date-preset last_7d \
  --fields campaign_id,campaign_name,spend,impressions,clicks,ctr,cpc,actions,action_values \
  --attribution adset --action-report-time impression --all

adsctl meta insights query --profile agency-read --account act_123 \
  --campaign 456 --level ad --since 2026-10-01 --until 2026-10-07 \
  --time-increment 1 --fields ad_id,ad_name,spend,impressions,inline_link_clicks

adsctl meta insights query --profile agency-read --account act_123 \
  --level campaign --date-preset last_7d \
  --breakdowns publisher_platform,platform_position --fields spend,impressions
```

| Query flag | Contract |
| --- | --- |
| `--level account\|campaign\|adset\|ad` | Required for account-scoped queries; object scope can default to its own level |
| `--campaign ID`, `--adset ID`, `--ad ID` | Mutually exclusive root object; level must be that level or a descendant |
| `--date-preset VALUE` | Native preset; mutually exclusive with an explicit range |
| `--since YYYY-MM-DD --until YYYY-MM-DD` | Required together; inclusive report dates in account timezone |
| `--time-increment all_days\|monthly\|N` | Native aggregation vocabulary; `1` means daily, default `all_days` |
| `--breakdowns A,B` | Native breakdown dimensions |
| `--action-breakdowns A,B` | Native action dimensions |
| `--attribution adset\|account\|windows` | Default `adset`, explicitly recorded |
| `--attribution-windows A,B` | Required with `windows`, forbidden with other modes |
| `--action-report-time impression\|conversion\|mixed` | Default `impression`, recorded in metadata |
| `--filter-file PATH` | Native `filtering` array for Insights |
| `--sort VALUE` | Native sort expression, such as `spend_descending` |

`adset` maps to `use_unified_attribution_setting=true`; `account` maps to `use_account_attribution_setting=true`; `windows` supplies `action_attribution_windows` without those setting flags. Never send contradictory modes. At account/campaign level, ad sets may have different attribution settings: label the result as mixed where known, not one universal window. Invalid field/breakdown/attribution combinations return structured validation or provider errors.

Default fields: identity fields appropriate to the chosen level, date_start/date_stop, spend, impressions, clicks. Expensive/unique/action metrics are requested explicitly. Metadata differentiates requested fields from present fields and cannot certify unavailable data merely because HTTP returned 200.

### Large reports and jobs

```sh
adsctl meta insights jobs create --profile agency-read --account act_123 \
  --level ad --date-preset last_30d --time-increment 1 \
  --fields ad_id,ad_name,spend,impressions,actions

adsctl meta insights jobs get 303 --profile agency-read --account act_123
adsctl meta insights jobs wait 303 --profile agency-read --account act_123 --timeout 10m
adsctl meta insights jobs results 303 --profile agency-read --account act_123 --all
```

`jobs create --wait --timeout 10m` optionally waits for provider completion and returns job status; rows still come from `jobs results`. `jobs wait` polls with backoff, prints progress to stderr only, and fails on provider job failure. A timeout returns the resumable job ID. Do not silently resubmit a job. `query` remains synchronous, avoiding surprising changes to its return type. Higher-level reports can use jobs internally but must expose a resumable local run if they exceed their deadline.

## 7. Reports, exports and portfolios

```sh
adsctl meta reports summary --profile agency-read --account act_123 --date-preset last_7d

adsctl meta reports compare --profile agency-read --account act_123 \
  --level campaign --date-preset last_7d --compare previous-period

adsctl meta reports rank --profile agency-read --account act_123 \
  --level ad --date-preset last_7d --metric spend --order desc --top 10

adsctl meta reports breakdown --profile agency-read --account act_123 \
  --date-preset last_7d --breakdowns publisher_platform,platform_position

adsctl meta reports rank --profile agency-read --account act_123 \
  --level ad --date-preset last_7d --metric cpa \
  --action-type offsite_conversion.fb_pixel_purchase --min-spend 25.00 --order asc --top 10

adsctl meta reports export --profile agency-read --account act_123 \
  --level ad --since 2026-10-01 --until 2026-10-07 \
  --fields ad_id,spend,impressions,actions --format csv --directory ./exports/oct-week1

adsctl meta reports summary --portfolio clients --date-preset last_7d
```

`reports summary` uses account metrics for totals and separately gathers contributors. `compare previous-period` resolves an adjacent equal-length completed period and returns both date ranges; explicit `--compare-since/--compare-until` is also supported. With a current-day range, require explicit acknowledgment via `--include-partial-period` and disclose the unequal observation risk.

`rank` fetches all eligible rows before sorting. Minimum-spend values use account currency major units, exact decimals, and return that unit explicitly. Zero-denominator CPA/ROAS is unavailable; insufficient-evidence entities are separate. An exhausted fetch bound produces a partial/incomplete result and nonzero exit, never a global ranking of a truncated page.

`--portfolio` is mutually exclusive with profile/account flags. Portfolios contain explicit profile/account pairs, are read-only and bounded by `--concurrency` (default 2). Results are scoped per member; totals are not combined across currencies. The same account listed under different credentials is flagged as duplicate and excluded from any aggregate unless explicitly selected once. A failed member yields a partial result and nonzero exit.

Exports fetch all pages and write `rows.csv` or `rows.jsonl` plus `metadata.json` containing query, scope, completeness and warnings. CSV encodes nested native fields as JSON cells; it does not silently flatten actions. Existing export directories are rejected to avoid accidental replacement. Interrupted exports retain a partial manifest; success receipts appear only after finalization.

## 8. Monitoring and snapshots

```sh
adsctl meta snapshots create --profile agency-read --account act_123 --save before.json
adsctl meta snapshots create --profile agency-read --account act_123 --save after.json
adsctl meta snapshots diff --before before.json --after after.json

adsctl meta monitor validate --file rules.yaml
adsctl meta monitor check --file rules.yaml --profile agency-read --account act_123
adsctl meta monitor check --file rules.yaml --portfolio clients --fail-on-finding
```

Snapshots contain a bounded configuration inventory, selected fields, API version and completeness; no credentials. Diffs match by ID and ignore declared volatile fields. Refuse to call a missing object “deleted” when either inventory was incomplete. Snapshot scope/version differences are surfaced before comparison.

```yaml
version: 1
id: client-daily-check
rules:
  - id: rejected-ads
    type: delivery-status
    resource: ads
    effective_status_in: [DISAPPROVED, WITH_ISSUES]
  - id: spend-threshold
    type: metric-threshold
    resource: account
    date_preset: yesterday
    metric: spend
    operator: gt
    threshold: '100.00'
    currency: USD
  - id: purchase-cpa
    type: metric-threshold
    resource: campaign
    date_preset: last_7d
    attribution: adset
    action_report_time: impression
    metric: cpa
    action_type: offsite_conversion.fb_pixel_purchase
    operator: gt
    threshold: '30.00'
    currency: USD
    min_spend: '100.00'
    min_actions: 5
    exclude_recent_days: 1
```

Currency mismatches are rule errors, not implicit conversions. `exclude_recent_days` shifts the resolved window back by complete account-local days and is recorded. Without `--fail-on-finding`, a successful evaluation exits 0 even if rules fire; unavailable required evidence or evaluation failure remains nonzero. That flag adds exit 11 for firing findings. Local state records deduplication/recovery; `--state-dir` supports an explicit persistent location. No scheduled tasks or outbound notifications are created by these commands.

## 9. Manage existing advertising

```sh
adsctl meta campaigns pause 456 --profile agency-write --account act_123
adsctl meta campaigns activate 456 --profile agency-write --account act_123 --dry-run
adsctl meta campaigns activate 456 --profile agency-write --account act_123

adsctl meta adsets budget set 789 --profile agency-write --account act_123 \
  --daily-budget-minor 2500 --currency USD

adsctl meta adsets update 789 --profile agency-write --account act_123 --file targeting-update.json
adsctl meta ads creative set 101 --profile agency-write --account act_123 --creative 202
adsctl meta campaigns archive 456 --profile agency-write --account act_123
adsctl meta ads delete 101 --profile agency-write --account act_123
```

Budget commands accept exactly one of `--daily-budget-minor` or `--lifetime-budget-minor`, plus a currency matching the account. For USD, 2500 represents USD 25.00. Determine campaign/ad-set budget ownership first; do not move budget ownership implicitly. Raw JSON payloads use Meta's native units. No binary floating-point conversion or universal multiplication by 100.

`activate ID` changes that object only. A parent may still block delivery, and paused children stay paused. Receipts include verification of effective state. `update --file` contains only intended writable fields. Creative `update` is restricted to supported metadata; copy/media changes create a new creative and use `ads creative set`. A provider-normalized creative name is not treated as drift, and `creatives update ID --name` corrects it without recreating the accepted creative. Archive and delete execute explicitly without prompts; they are never used as automatic cleanup on workflow failure.

`validate --operation create|update [--id ID] --file request.json` separates local checks from supported remote validation. Unsupported checks are labeled, not passed off as successful validation. All write actions support `--dry-run`, but not all have a server validation mode.

## 10. Create an ad with individual commands

Discover prerequisites before creating resources:

```sh
adsctl meta pages list --profile agency-write --account act_123
adsctl meta instagram-accounts list --profile agency-write --account act_123
adsctl meta pixels list --profile agency-write --account act_123
adsctl meta audiences list --profile agency-write --account act_123
adsctl meta targeting search --profile agency-write --account act_123 \
  --type adgeolocation --query Jakarta

adsctl meta images upload --profile agency-write --account act_123 --file ./banner.jpg
adsctl meta videos upload --profile agency-write --account act_123 --file ./demo.mp4
adsctl meta videos wait 404 --profile agency-write --account act_123 --timeout 10m

adsctl meta campaigns create --profile agency-write --account act_123 \
  --name 'October traffic' --objective OUTCOME_TRAFFIC --special-ad-categories NONE
adsctl meta adsets create --profile agency-write --account act_123 --file adset.json
adsctl meta creatives create --profile agency-write --account act_123 --file creative.json
adsctl meta creatives preview 202 --profile agency-write --account act_123 --format DESKTOP_FEED_STANDARD
adsctl meta ads create --profile agency-write --account act_123 \
  --name 'Banner A' --adset 789 --creative 202
```

Video uploads accept `--mode auto|resumable|single`. The default `auto` chooses resumable for **every video**, independent of file size. This is a client policy, not a claim about a universal Meta request-size limit. Images and explicit `--mode single` use one multipart request. Both paths enforce a local nonempty regular-file bound of 1 GiB; provider limits still apply. `--dry-run` reports the selected mode and local bound without contacting Meta or creating upload state.

Resumable uploads use Meta's video host and start/transfer/finish protocol. Transfers stream exactly the server's requested byte ranges, with bounded retries only within the existing session. The receipt includes `upload_id`, `response.id` (the video ID once known), the phase and confirmed offsets. `--state-dir` overrides the private checkpoint directory, otherwise `$ADSCTL_HOME/state` or `~/.adsctl/state` is used. Checkpoints contain the file SHA-256/size, session/video IDs, offsets, profile/account/API/endpoint scope, and no credentials. Keep them until processing is confirmed.

```sh
adsctl meta videos upload --profile agency-write --account act_123 \
  --file ./demo.mp4 --mode resumable --request-timeout 5m --timeout 30m
# Use the upload_id from that receipt after an interruption:
adsctl meta videos upload --profile agency-write --account act_123 \
  --file ./demo.mp4 --resume upload_<id> --request-timeout 5m --timeout 30m
adsctl meta videos wait <video-id> --profile agency-write --account act_123 --timeout 10m
```

Resume rejects a changed file or scope, even if the checkpoint is already complete. A completed upload returns the same video ID without creating an asset. Cancellation and deadlines preserve checkpoints; `--request-timeout` bounds each request and `--timeout` bounds the full operation, including hashing and recovery. An ambiguous start remains blocked because no acknowledged session identity is available; inspect account inventory before deciding whether another upload is appropriate. An ambiguous finish is reconciled by reading the saved video ID's status and is never blindly repeated. If status is not yet conclusive, resume the same ID later. The command uploads an advertising asset; it does not activate ads or publish an organic post.

HTTP failures expose `error.http_status`. HTTP 413 means the request body was rejected as too large and recommends a smaller copy or resumable video mode; arbitrary non-JSON response bodies are not emitted. Uncertain mutation outcomes exit 10 and keep their receipts for reconciliation. Launch apply/resume uses resumable uploads with one stable checkpoint per video step, preserving the video ID and avoiding duplicate assets across interrupted runs.

`--special-ad-categories NONE` maps to an empty declaration array, subject to the applicable category requirements; other values map to the specified categories. Defaults for campaign/ad-set/ad creation explicitly supply `PAUSED`. Scalars are convenience flags; resource `--file` provides the native payload instead. Status never changes from a validation command.

Example `adset.json` for a traffic template:

```json
{
  "name": "Indonesia traffic",
  "campaign_id": "456",
  "daily_budget": 2500,
  "billing_event": "IMPRESSIONS",
  "optimization_goal": "LINK_CLICKS",
  "bid_strategy": "LOWEST_COST_WITHOUT_CAP",
  "targeting": {"geo_locations": {"countries": ["ID"]}},
  "status": "PAUSED"
}
```

Example `creative.json`, using the uploaded image hash and an accessible Page:

```json
{
  "name": "Banner A",
  "object_story_spec": {
    "page_id": "505",
    "link_data": {
      "image_hash": "UPLOADED_IMAGE_HASH",
      "link": "https://example.com/product",
      "message": "Explore our product",
      "name": "A clear product headline",
      "call_to_action": {"type": "LEARN_MORE", "value": {"link": "https://example.com/product"}}
    }
  }
}
```

These are illustrative, not complete for every account/objective/version. Identity permissions, targeting restrictions and creative enhancement settings require validation. Previews return provider HTML/URLs inside JSON; do not auto-open or execute them. A human can explicitly save preview output to a file for inspection. No image-generation service is embedded in the CLI.

## 11. Complete campaign launch

```sh
adsctl meta launch template --objective traffic --format image --save launch.yaml
adsctl meta launch validate --file launch.yaml --profile agency-write --account act_123
adsctl meta launch plan --file launch.yaml --profile agency-write --account act_123 --save plan.json
adsctl meta launch apply --plan plan.json --profile agency-write --account act_123
adsctl meta launch status run_001
adsctl meta launch resume run_001 --profile agency-write --account act_123
adsctl meta launch activate run_001 --profile agency-write --account act_123
```

```yaml
version: 1
kind: meta-launch
name: october-traffic
assets:
  - key: banner
    type: image
    file: ./banner.jpg
campaigns:
  - key: traffic
    payload:
      name: October traffic
      objective: OUTCOME_TRAFFIC
      special_ad_categories: []
      status: PAUSED
adsets:
  - key: indonesia
    campaign_ref: traffic
    payload:
      name: Indonesia traffic
      daily_budget: 2500
      billing_event: IMPRESSIONS
      optimization_goal: LINK_CLICKS
      bid_strategy: LOWEST_COST_WITHOUT_CAP
      targeting:
        geo_locations:
          countries: [ID]
      status: PAUSED
creatives:
  - key: banner-a
    image_ref: banner
    payload:
      name: Banner A
      object_story_spec:
        page_id: '505'
        link_data:
          link: https://example.com/product
          message: Explore our product
          name: A clear product headline
          call_to_action:
            type: LEARN_MORE
            value:
              link: https://example.com/product
ads:
  - key: banner-ad
    adset_ref: indonesia
    creative_ref: banner-a
    payload:
      name: Banner A
      status: PAUSED
```

`campaign_ref` supplies `campaign_id`; `adset_ref` supplies `adset_id`; `creative_ref` supplies `creative.creative_id`; image references supply `object_story_spec.link_data.image_hash`. Video templates use a different explicit mapping. Conflicting native IDs and references are rejected. Existing resources can be referenced by native IDs instead, after account/access checks. Plans record profile/account/version/currency, dependency graph, exact payloads, file hashes and validation coverage. Media is uploaded only during apply.

`launch validate` runs local checks plus supported remote checks without creating assets. Server validation requiring not-yet-created parents is `deferred`. Plan files do not contain credentials and cannot introduce arbitrary Graph hosts/paths; apply revalidates operations against the command schema.

If a step's outcome is unknown, reconcile explicitly:

```sh
adsctl meta launch reconcile run_001 --step campaigns.traffic --remote-id 456 \
  --profile agency-write --account act_123
```

This verifies the remote object's account/type and expected fields before attaching it to the journal. An explicit `--retry-unknown` alternative on `reconcile` records the operator's decision to retry that step and discloses duplicate risk; it never happens automatically. `resume` cannot bypass unknown creates, except for a resumable video session that can safely continue or reconcile its saved finish. For a known video session, use `resume`; `--retry-unknown` refuses to replace that session. For an unacknowledged video start, an explicit `--retry-unknown` decision records the prior checkpoint before permitting a new start with duplicate risk. Journal inspection is local and needs no token. Resumes require unchanged plan identity, checked files, correct credential scope and exclusive local ownership.

## 12. Output, pagination and error UX

A collection result keeps native data under `data`; a get uses an object. Shared top-level fields are stable:

```json
{
  "schema_version": "1",
  "ok": true,
  "data": [{"id": "456", "name": "October traffic", "status": "PAUSED", "effective_status": "PAUSED"}],
  "meta": {
    "platform": "meta",
    "command": "meta campaigns list",
    "profile": "agency-read",
    "account_id": "act_123",
    "api_version": "v25.0",
    "fetched_at": "2026-10-09T15:00:00Z",
    "complete": false,
    "pagination": {"has_more": true, "next_cursor": "OPAQUE_CONTINUATION"}
  },
  "warnings": [],
  "error": null
}
```

The cursor binds account, profile, version, fields and query parameters; changing these invalidates it. It contains no token and is opaque continuation state, not a durable snapshot or authorization mechanism. A list defaults to one provider page and reports `complete:false` when more remain. `--all` follows pages; reaching a limit/deadline/failure gives incomplete metadata. Inventory can change during pagination, so even exhaustion is not a transactional snapshot.

High-level reports fetch the complete required input, or return nonzero with `partial:true`. Native rows remain unchanged; derived values live in explicitly labeled report structures with formula/action-type/unit metadata. `warnings` contain machine codes, scope, message and evidence where possible.

On failure before any useful output, stdout is empty and stderr contains the structured error envelope:

```json
{
  "schema_version": "1",
  "ok": false,
  "data": null,
  "meta": {"platform": "meta", "profile": "agency-read", "account_id": "act_123"},
  "warnings": [],
  "error": {
    "code": "AUTH_INVALID",
    "message": "The credential is invalid or expired.",
    "retryable": false,
    "provider": {"type": "OAuthException", "code": 190, "error_subcode": null, "fbtrace_id": "TRACE_ID"},
    "suggested_action": "Replace the token referenced by profile agency-read, then run auth check."
  }
}
```

Partial portfolio results, interrupted exports, applied-but-unverified writes and unknown mutation outcomes also produce an `ok:false` result receipt on stdout when available, plus a structured failure on stderr and a nonzero exit. Agents inspect both `ok` and exit status. `retryable` means safe automatic retry, not merely that another attempt might succeed; ambiguous writes set it false.

JSONL streams typed `meta`, `data`, `checkpoint`, `error` and terminal `end` records. Data records wrap native rows. Terminal records include completeness/row count; a missing terminal record or nonzero exit means incomplete output. Output is never a mixture of progress text and JSON. For JSONL, terminal error information also appears on stderr. Default JSON is buffered/spooled until a final envelope can be emitted.

Table output is intentionally lossy and shows account/currency/period context, relevant columns and a continuation/completeness footer. No color in JSON. Example:

```text
Account act_123 | USD | Campaigns
ID    NAME              CONFIGURED  EFFECTIVE
456   October traffic   PAUSED      PAUSED
457   Website sales     ACTIVE      ACTIVE
2 rows | complete
```

| Exit | Meaning |
| --- | --- |
| 0 | Successful command, including deliberate one-page reads or a pending submitted report job |
| 1 | Unexpected internal failure |
| 2 | CLI usage or local configuration error |
| 3 | Invalid/expired credentials |
| 4 | Permission or asset-access denial |
| 5 | Input/provider validation failure |
| 6 | Throttling exhausted allowed retry budget |
| 7 | Timeout/cancellation with resumable state where available |
| 8 | Other provider/network failure |
| 9 | Partial operation or incomplete required input, including applied-unverified results |
| 10 | Unknown mutation outcome requiring reconciliation |
| 11 | Findings fired with explicit `--fail-on-finding` |

Help should provide one minimal example, an agent JSON example, required scope, effects, supported formats, and relevant error codes. Cobra's own automatic usage/error printing is suppressed for execution failures so the central renderer controls machine output. No browser opens, prompts, account selection menus, or automatic confirmation retries occur.
