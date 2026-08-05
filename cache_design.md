# Cache / State Redesign

Design document for reworking rdm-integration's Redis-backed state. Prepared 2026-08-05
after the silent-failure incident investigation; intended to be executed in a dedicated
session. Status: design agreed (Eryk + Claude, 2026-08-05); execution not started.
Read §8 (decisions log) before proposing design changes — rejected alternatives
are listed there with their reasons. Context: see git history `21fbbd3..12f7055` (the incident fixes) and the
regression `7f15343` (presence guard lost in a refactor, undetected for 2.5 years).

## 1. Why

One day of intensive testing surfaced this class of problems, all rooted in derived
state being trusted over reality:

- Files deleted/replaced in the Dataverse UI stayed "present and equal" in compare
  (stale immortal cache, fixed tactically in `3a47309`/`50c03a2`).
- A failed flush silently requeued and looped across container restarts (fixed in
  `12f7055`), each round clobbering freshly computed hashes with a stale snapshot
  of the whole-map cache — compare showed endless rehashing of untouched files.
- Hash jobs spawned by compare are starved while a transfer holds the dataset lock.
- The frontend re-serves stale compare results on tab navigation; a job killed by a
  redeploy leaves the UI polling a void forever; no-op jobs give no feedback.
- Progress reporting is implemented *through* the hash cache (store-every-10-files),
  entangling memoization with UI progress.

## 2. Current state inventory (every Redis usage)

| Key pattern | Content | TTL | Writers | Readers | Problems |
|---|---|---|---|---|---|
| `hashes: <pid>` | whole-map JSON: path → {LocalHashType/Value, RemoteHashes{type→hash}} | **none** | write() per transferred file; doRehash every 10 files + at end; batch-delete removes entries | localRehash (compare), filterRedundant, calculateHash | Immortal; keyed by *path* (aliasing on delete/replace); whole-map read-modify-write → last-writer-wins clobbering between concurrent jobs; doubles as progress channel |
| `<pid> -> <file>` | `Written` / `Deleted` marker | 5 min | persisting.go after write/delete | localRehash overlay | Overlay lies about the listing; cleanup goroutine (10 s delay) dies on restart; TTL is the only real cleanup |
| `error <pid>` | last job error string | 5 min | sendJobFailedMail, doFlush rollback path | common/compare.go:111 (UI error display) | One slot per dataset; overwritten; no job identity |
| `<uuid>` (compare/compute response) | cached response JSON | cacheMaxDuration | common/compare.go, compute | frontend polling | Deleted on ready-read (compare.go:72) — repeated polls after ready get nothing; frontend tab cache re-uses old keys and shows stale results |
| `lock: <pid>` | dataset lock | LockMaxDuration | acquire/release in job.go | job scheduling | Transfer jobs hold it for minutes–hours; hash-only jobs starve; unlock only at job end |
| `jobs` (list) | serialized Job queue | persistent | addJob (LPush) | popJob (RPop) | Jobs survive deploys with no re-validation — a stale/looping job resumes invisibly (observed live) |
| `<pluginId>-<sessionId>` | OAuth token | LockMaxDuration | oauth cache | oauth cache | OK |
| `ddi-cdi:*` | tool output cache | bounded | ddi_cdi.go | ddi_cdi.go | OK, out of scope |

Known point bugs, each absorbed by a design section (step 6 verifies):
- `persisting.go:336` rollback deletes key `k` (bare path) — the marker key is
  `"<pid> -> <k>"`; the delete has never hit anything. → moot once markers are
  removed (§4.2, step 2).
- `compare.go:72` delete-on-read makes the cached response single-shot; interacts
  badly with frontend re-polling. → §4.5.
- `checkTransferredSize` treats size 0 as "unknown" — iRODS sizes are authoritative
  incl. 0; needs an explicit size-known signal from plugins. → §4.4, step 0.
- `io.go` write(): `if hashValue != remoteHashValue { knownHashes[...] = ... }` —
  hashes cached only on mismatch; near-dead after the mismatch-errors fix. →
  replaced by unconditional caching at registration (§4.1, step 1).

## 3. Design principles

1. **The destination listing and source listing are the only truth** for presence
   and current content. No cache or marker may ever assert existence.
2. **Caches are self-validating memos**: path-keyed for latest-version-only
   semantics (overwrite-on-change self-cleans), carrying the destination hash
   they were computed for, so a stale entry fails validation rather than lies.
3. **Progress/state is its own channel** with explicit lifecycle, never smuggled
   through a memo.
4. **Derived state is disposable**: wiping all of Redis (except the queue and oauth)
   must cost only recomputation, never correctness.
5. **Jobs have identity**: everything a job writes is attributable and inspectable.

## 4. Target design

### 4.1 Hash memo (replaces `hashes: <pid>`)

**A Redis hash per dataset** (native `HSET`/`HGETALL`, not a JSON string):
key `hashes:<pid>`, field = file path, field value = per-file JSON
`{ "destHashType": "MD5", "destHashValue": "…", "remoteHashes": { "<type>": "…" }, "lastUsed": … }`,
**no TTL**.

This deliberately preserves BOTH original design rationales, which the
historical code got right and any redesign must not lose:

1. **Path identity — latest version only.** A changed file overwrites its
   field; the memo self-cleans for the common case. Content-addressed keys
   were considered and rejected: sync tools that update datasets by
   delete-all-then-re-add would mint new storage identifiers for every file
   on every sync, accumulating junk per cycle. Path keying copes by
   construction.
2. **Single round trip for compare.** `HGETALL hashes:<pid>` reads the whole
   dataset's memo in one query — same cost profile as today's `GET` of the
   JSON map. No point-query storm on gazillion-file datasets. (Bonus for the
   §6 lazy-tree future: `HMGET` enables per-folder-page reads when compare
   itself becomes paged.)

What actually changes versus the original is exactly two mechanics — the ones
behind the observed failures:

- **Writes become per-field atomic** (`HSET <pid> <path> <json>` at the moment
  each hash is computed) instead of read-whole-map / mutate / write-whole-map
  at job end. The last-writer-wins clobbering between concurrent jobs
  disappears; same-file concurrency degrades to per-file last-writer-wins,
  which is benign. This also decouples progress from the memo for free: no
  more store-every-10-files-as-progress (job state records take that role,
  §4.2), and hash-only jobs stop needing the dataset lock.
- **Validity is part of the schema.** Each field records the destination hash
  it was computed for; a read is valid only when it matches the live listing
  (the `50c03a2` check, promoted from bugfix to design invariant — documented
  here precisely so a future "trimming" refactor cannot silently drop it
  again, as `7f15343` did).

Notes:

- The original was not "weird" — it was coherent; its failures came from an
  *undocumented invariant* (presence implied by the old guard) plus the bulk
  store cadence. The io.go mismatch-only cache write is the one true oddity
  and is replaced by unconditional caching at registration.
- No expiry: ~200 bytes per field vs. re-streaming TB-scale files. Junk that
  overwrite cannot clean (permanently deleted files, removed datasets) is
  GC's job (§4.6): listing diff → `HDEL` dead fields / `DEL` dead datasets.
- Migration: the key name can stay `hashes: <pid>` conceptually, but the type
  changes (string → hash), so old keys must be dropped (see runbook); memo
  refills lazily — first compare per dataset recomputes missing rehashes once.

### 4.2 Job state record (replaces markers, `error <pid>`, store-as-progress)

Key: `jobstate:<jobId>` (jobId = uuid, generated at submission) with TTL
(e.g. 48 h, refreshed while running). Index: `jobstate-index:<pid>` → set of
recent jobIds for the dataset (same TTL).

```json
{
  "id": "…", "persistentId": "doi:…", "kind": "transfer|hash|delete|globus",
  "phase": "queued|running|flushing|done|failed|cancelled",
  "files": { "total": 29, "done": 12, "failed": 0, "deleted": 5 },
  "current": { "name": "Fig7/data.pkl", "bytesDone": 123456789, "bytesTotal": 48318382080 },
  "startedAt": "…", "updatedAt": "…",
  "error": "unrecoverable error detected: …",
  "results": [ { "file": "…", "outcome": "added|replaced|deleted|failed|skipped-noop", "reason": "…" } ]
}
```

- Written by the job loop at every phase transition and by workers at throttled
  intervals (every N files or M seconds or X bytes — gives byte-level progress
  for large single files, which the current design cannot show at all).
- `skipped-noop` outcome makes filtered-out actions visible ("file was already
  removed") — fixes the invisible no-op delete.
- UI progress = poll `jobstate-index:<pid>` + records. The Written/Deleted
  markers and their fragile delayed-cleanup goroutine are deleted outright:
  after a terminal phase the frontend triggers a fresh compare instead of the
  backend overlaying beliefs onto the listing.
- Startup re-validation: when popJob resumes a queued job after restart, check
  its jobstate — `cancelled`/`failed` → drop instead of blindly resuming
  (prevents the observed cross-deploy zombie resume).

### 4.3 File status derivation (pure function, no state)

`status(file)` computed per compare from: source listing, destination listing,
hash memo, active job records. Enum (superset of current statuses):

- `equal` — both sides present, hashes match (memo hit or same hash type)
- `changed` — both present, hashes differ
- `new-in-source` — not in destination
- `missing-in-source` — only in destination
- `hash-pending` — memo miss; hash job queued/running (UI: spinner + progress
  from the job record, not `"?"` sentinel strings in the hash field)
- `in-progress` — an active job is transferring/deleting this file (from job
  record, not markers)

The `"?"`-in-hash-field convention disappears; the response carries an explicit
status and, where applicable, the jobId to poll.

Enum audit (part of this step): today `tree` defines file status
`Equal=0, New=1, Updated=2, Deleted=3, Unknown=4` and action
`Ignore=0, Copy=1, Update=2, Delete=3`, mirrored as numeric literals in the
frontend (`data.state.service.ts`, `folder.action.update.service.ts`, and
their consumers). Observed gaps: `Unknown` conflates "hash pending" with
"in-progress by a job"; "missing in source" is represented via `Deleted`+action
semantics rather than a distinct status; nothing distinguishes
"size unknown" (see §4.4) in the display. The new enum must be designed
against the frontend rendering table (status × action → icon/label/selectable)
and shipped as a shared constants change on both repos in the same release.

### 4.4 Plugin size contract (size unknown vs. authoritative 0)

`RemoteFileSize == 0` currently means both "empty file" (iRODS: authoritative)
and "size not available" (GitLab). This weakened `checkTransferredSize` (a
truncated-to-0 stream from a size-authoritative source passes the guard) and
has the same ambiguity in the max-size pre-check (`> maxFileSize` never fires
for unknown sizes — correct, but indistinguishable from empty).

Change: add `RemoteFileSizeKnown bool` to `tree.Attributes` (explicit flag
rather than a -1 sentinel, so the JSON stays honest). Audit every plugin and
set it deliberately: irods (always true, incl. 0), local (true), sftp (true),
onedrive/osf/redcap/github (verify per API), gitlab (false). Consumers:
`checkTransferredSize` guards whenever `RemoteFileSizeKnown`, incl. size 0;
the compare pre-check and the UI size display use it to distinguish "0 B" from
"unknown". This belongs to the same work because file state — including size —
is part of what compare/state must represent truthfully.

### 4.5 Compare response cache & frontend contract

- Keep the `<uuid>` response cache, but: no delete-on-read; bounded TTL; response
  includes `computedAt` + the pid's current `jobstate` snapshot.
- Frontend rules (rdm-integration-frontend work): a new compare key on every tab
  activation or an explicit invalidation event on job completion; polling any
  jobId ends with a timeout state ("job state unknown — refresh") instead of
  spinning forever on a missing record.

### 4.6 Garbage collection — Redis AND storage (in scope, not optional)

Monthly scheduled cleanup, reviving the parked orphan-cleanup work: `cleanup()`
in `core/persisting.go` still carries the commented-out
`Destination.CleanupLeftOverFiles(...)` call, and `dataverse.CleanupLeftOverFiles`
(wrapping `cleanStorage`, version-gated by `filesCleanup`) is implemented and
dormant — that was the first step of this GC, parked long ago. Motivation is
concrete: the 2026-08-05 incident loop alone left multiple orphaned 45 GiB
objects on pilot, and prod datasets carry years of orphans (QWUDVE had dozens).

Two sweeps, one schedule (~monthly):

1. **Storage orphans**: per dataset, run `cleanStorage` — first pass
   `dryrun=true`, log/report the candidates and total bytes, then delete.
   Safety: skip datasets with an active job or lock; keep the dry-run report
   (jobstate-style record) so deletions are auditable. Note cleanStorage
   semantics: "Found" = registered files (kept), "Deleted" = orphans removed.
2. **Redis memo GC**: with the per-dataset hash (§4.1) this is a listing diff —
   for each dataset in `datasets-seen`, fetch the live listing, `HSCAN` the
   memo, and `HDEL` fields whose path no longer exists; `DEL` the whole key for
   datasets that are gone entirely. `lastUsed` is a tiebreaker for reporting,
   not a deletion criterion — never delete an entry for a path that still
   exists (the no-rehash-of-huge-files principle outranks tidiness).

Which datasets to sweep: maintain a `datasets-seen` set (pid added on every
compare/job) rather than enumerating all of Dataverse. Scheduler options for
the session to pick: in-app ticker (simplest, but think multi-instance),
or host cron hitting a new admin endpoint (matches existing ops style —
make scripts + cron). Either way: report by email, dry-run mode as a flag.

## 5. Execution plan (PR-sized steps, in order)

0. **Size contract** (independent, small — can ship first): `RemoteFileSizeKnown`
   in `tree.Attributes`, set deliberately in every plugin, consumed by
   `checkTransferredSize`, the max-size pre-check, and the UI size display
   (§4.4). Tests per plugin.
1. **Memo rekeying** — `core/rehashing.go` (localRehash → memo lookups; doRehash
   → per-key writes; drop getKnownHashes/storeKnownHashes), `core/persisting.go`
   + `core/io.go` (cache at registration), `core/filterRedundant` (memo lookup).
   Remove the dataset-lock requirement from hash-only jobs. Tests: memo hit/miss,
   delete/replace-outside-integration, concurrent writers.
2. **Job state records** — `core/job.go` (lifecycle writes, startup re-validation),
   `core/persisting.go` (progress + per-file outcomes incl. skipped-noop),
   new read endpoint for the frontend. Delete markers + `error <pid>` + the
   delayed-cleanup goroutine. Tests: phase transitions, restart resume/drop,
   throttled progress.
3. **Status derivation + response** — replace `"?"` convention with the status
   enum; wire jobstate into compare responses. Update `app/frontend` API types.
4. **Frontend batch** (separate repo): tab-cache invalidation, poll timeouts,
   no-op/job-outcome display, byte-progress bar for large files.
5. **Garbage collection** (§4.6): revive CleanupLeftOverFiles, storage sweep +
   memo GC + `datasets-seen` index + scheduler + dry-run/report. Depends on
   step 1 (memo format) and step 2 (jobstate for audit records).
6. **Point-bug sweep**: verify each §2 bug is really gone via its owning section; fix stragglers.
7. **Scalability analysis** (§6): produce the lazy-tree/cursor design doc for
   many-file datasets, after steps 1–3 are in.

Rollout: each step deployable alone; step 1 invalidates old hash state (lazy
refill, one recompute round per dataset — announce on pilot). Steps are
backward-compatible with the frontend until step 3.

### Migration runbook (pilot, then production, at step-1 deploy)

Old-format keys are unreadable by the new code and must be dropped explicitly
(both environments, `rdm-cache-1`):

```
# on the host (pilot: libis-p-rdm-1, prod: libis-p-rdm-2), container rdm-cache-1:
docker exec rdm-cache-1 sh -c "redis-cli --scan --pattern 'hashes: *' | xargs -r -L 500 redis-cli DEL"   # whole-map hash caches (step 1)
docker exec rdm-cache-1 sh -c "redis-cli --scan --pattern '* -> *'    | xargs -r -L 500 redis-cli DEL"   # Written/Deleted markers (step 2)
docker exec rdm-cache-1 sh -c "redis-cli --scan --pattern 'error *'   | xargs -r -L 500 redis-cli DEL"   # old error slots (step 2)
```

Do **NOT** touch: `jobs` (queue — drain or verify empty before deploying
instead), `lock: *` (expire on their own), oauth `<pluginId>-<sessionId>`
tokens, `ddi-cdi:*`. Deploy order: verify no running jobs → deploy → drop keys
→ smoke-compare a small and a huge-file dataset (the latter confirms hashes are
recomputed once and memoized forever). Warn pilot/prod users: first compare per
dataset after migration triggers one full rehash round — schedule the prod
deploy accordingly (overnight for datasets with TB-scale files).

## 6. Large-dataset scalability — resolved direction, remaining analysis

Datasets with very many files (10k–100k+) strain the current design: compare
builds and ships the full flat node map, the frontend renders it whole, and
every hash decision walks the entire map. The recent Dataverse UI work
(IQSS/dataverse#12382 — lazy React file tree + cursor API, not yet merged)
proves the pattern to port:

- **Opaque-cursor keyset pagination** on the listing endpoint (stable order,
  bounded pages), ETag/304 caching for immutable versions, covering index.
- **Lazy folder loading** — the tree opens 100k-file datasets by fetching
  children per folder on expand.
- **Tri-state folder checkboxes** — the UI analog of our hard problem.

**Resolved direction (2026-08-05): laziness at the right layer — no lazy join.**
A truly lazy end-to-end compare (never materializing both listings) is
rejected: source APIs are too diverse to cursor a merge over (iRODS lists
per-collection, GitHub returns whole recursive trees, OneDrive does deltas,
SFTP walks), and empirically the pain at 50k files was never backend memory —
it was frontend rendering and the folder-state algorithm. Therefore:

1. **Compare stays an eager, full, server-side join** — required anyway for
   hashing decisions and rollups; 100k nodes ≈ tens of MB in Go; plugin API
   diversity becomes a non-issue (each plugin keeps its cheapest full listing).
2. **The compare *result* becomes a per-dataset Redis hash** (same native-HSET
   pattern as the memo, one level up): field = path, value = node status JSON.
   The frontend fetches **per folder page** (`HMGET` / `HSCAN MATCH prefix`)
   instead of one giant JSON payload — this is where #12382's cursor idea
   genuinely transfers: paged access to a computed result, not to sources.
3. **Folder aggregates: computed once, updated in O(depth).** Per-folder
   counters (per-status, selected/total) are produced in the same compare
   pass. UI (de)selection updates walk only the ancestor chain, adjusting
   counters and re-deriving each ancestor's tri-state from counts — never a
   whole-tree recomputation. This algorithm is the heart of the feature; the
   deep-structure-change-on-deselect case must be a unit-tested O(depth) op.
4. **Frontend renders only visible rows** (virtual scroll + lazy folder expand
   over the paged result) — the actual cure for UI freeze at 100k+.

Still to analyze in the execution session: exact paging API shape (cursor vs
folder-prefix), where selection state lives (client-only counters vs shared),
and the prior folder-size solution (check whether it was DB-side in Dataverse /
#12382's covering index) for reusable ideas.

Prerequisites unchanged: the state refactor (steps 0–3) must land first — the
result-hash and rollups must not repeat the markers/whole-map mistakes.

## 7. Open questions for the execution session

- TTLs: memo has none (decided — rehash cost of TB-scale files dwarfs storage;
  see §4.1; GC per §4.6 handles hygiene). jobstate 48 h? response cache
  duration? GC cadence — monthly? — and scheduler shape (in-app ticker vs.
  host cron + admin endpoint)?
- Should the transfer job also drop the dataset lock during the pure-streaming
  phase (only lock for flush)? Would unblock parallel hash jobs entirely, but
  needs thought re: concurrent submits of the same file.
- Globus jobs: their state lives server-side in Dataverse — represent them in
  jobstate as `kind: globus, phase: delegated` with the task id?
- Queue: stay on Redis list, or move to per-dataset lists to make the startup
  re-validation and inspection simpler?

## 8. Decisions log (rejected alternatives — do not re-litigate without new facts)

| Decision | Rejected alternative | Why rejected |
|---|---|---|
| Path-keyed memo (latest version only) | Content-addressed keys `(storageIdentifier, hash)` | Sync tools that delete-all-then-re-add mint new storage identifiers every cycle → junk per sync even with unchanged content; we never need old versions |
| One Redis hash per dataset (`HSET`/`HGETALL`) | One string key per file | Point-query storm on many-file datasets; whole-map single-read was an original design goal worth keeping |
| No TTL on memo | Sliding 30-day TTL | Recomputing a hash for a TB-scale file means re-streaming it; ~200 B storage is nothing; GC (§4.6) handles hygiene instead |
| Eager full compare + paged result access | Lazy end-to-end join with cursors on sources | Source APIs too diverse to cursor a merge over; empirically the 50k-file pain was frontend rendering + folder-state algorithm, not backend memory |
| Explicit `RemoteFileSizeKnown` flag | `-1` sentinel for unknown size | Honest JSON; zero stays a real size; sentinel invites the next 0-vs-unknown confusion |
| Validity check on memo read (destination hash recorded in entry) | Trusting cache by key alone | The `7f15343` regression: presence/validity invariants that live only in code conditions get trimmed by refactors — schema-level validity survives them |

The meta-lesson behind this table (and this document): the 2.5-year `7f15343`
regression happened because design rationale lived nowhere. When changing this
design, update this log.
