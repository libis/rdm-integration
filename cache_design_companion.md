# Cache redesign — execution notes

Companion to `cache_design.md`. Not design changes — execution risk, ordering, and
verification. Produced 2026-08-26 from a review session. The design itself stands;
nothing here proposes altering §1–§8.

## Framing

**Steps 0–4 are recoverable; §4.6 is not.** Principle 4 says wiping Redis costs
recomputation, never correctness. If that holds, the rollback for every one of
steps 0–4 is `FLUSHDB` + redeploy. Worst realistic outcome is an overnight rehash
storm on prod — availability cost, not data cost.

§4.6 (storage GC) is the only step where a bug destroys something that cannot be
recomputed. Allocate caution accordingly.

**Scope reality:** the doc says "executed in a dedicated session." It is seven steps
across two repos plus a new public repo — months, not a session. The entry cost
reading as the whole thing is probably why this keeps not starting. Two entry points
below commit to nothing else.

## Do first (no dependencies, no commitment)

- [ ] **Principle-4 test harness, written against the *current* code.** Flush Redis
      mid-transfer, mid-compare, and between job phases; assert convergence to
      correct state. It should already pass today. Once green, every subsequent step
      has a tested rollback — this is the single highest-value item here, because it
      converts the whole plan from irreversible-feeling to demonstrably reversible.
- [ ] **Step 0 (`RemoteFileSizeKnown`).** Genuinely independent, small, testable per
      plugin. Ships alone.
- [ ] **GC script in report-only mode.** Deletes nothing. Produces the prod orphan
      inventory needed before any other GC decision.

## Already in place — don't rebuild

The August incident fixes all shipped with tests. These encode the exact failure
modes the redesign must not reintroduce, and they are behaviour-level, not
implementation-level:

- `core/rehashing_test.go` — `3a47309` (stale cache for deleted files) and
  `50c03a2` (validity check: cached rehash must match live destination hash)
- `core/persisting_test.go` — `12f7055` (flush error propagation)
- `dataverse/dataverse_write_test.go` — `aeab1a0` (fail fast on rejected registration)
- `core/io_test.go`, `dataverse/max_file_size_test.go`, `plugin/types/unrecoverable_test.go`,
  `plugin/impl/irods/query_test.go` — `21fbbd3` (silent transfer failures)

Run these against each step. The `50c03a2` test is the specific guard against
repeating `7f15343` — §4.1 promotes the validity check to schema, but a schema field
is inert unless something reads it, and the test is what keeps the read alive.

## Step-level notes

### Step 1 — split it
Currently bundles two independent changes: memo layout (whole-map → `HSET`) and lock
discipline (hash-only jobs stop taking the dataset lock). If behaviour changes you
won't know which caused it.

- [ ] 1a: memo layout only.
- [ ] 1b: lock removal from hash-only jobs. This is the one with concurrency
      consequences.
- [ ] Test the new partial-failure semantics: per-field `HSET` means a job dying
      mid-way leaves a partially populated memo, where the old whole-map write left
      it untouched. Self-validating entries should make this benign — verify, don't
      assume.

### Step 3 — the quiet one
Highest correctness risk in steps 0–4, and it fails silently. Status enum is mirrored
as numeric literals in the frontend (`data.state.service.ts`,
`folder.action.update.service.ts`). A cached old bundle against a renumbered backend
mislabels file state and the user acts on it. Nothing goes red.

- [ ] **Append new statuses only. Never renumber existing values.**
- [ ] Shared constants change ships to both repos in the same release.
- [ ] Make both of the above a test, not a sentence in a doc.

## §4.6 — storage GC

- [ ] **Verify `cleanStorage`'s own test coverage in Dataverse `develop` before
      pointing it at a full production sweep.** `dryrun` defaulting to *false* with
      no age guard suggests lightly-exercised upstream code, and the keep-set logic
      is being trusted wholesale.
- [ ] **Pilot test with deliberately created orphans — not by reviewing a prod list.**
      Manual verification of prod candidates is unfalsifiable: after years of
      accumulation every name looks like plausible junk, so approval carries no
      information. On pilot, create known ground truth:
      - a direct upload started and abandoned
      - a completed S3 PUT with no `/addFiles` registration
      - a normally registered file (must NOT be proposed)
      - a Globus transfer left holding its lock (must be skipped via the lock rule)
      A sweep that proposes the registered file, or misses a known orphan, fails
      visibly.
- [ ] Report-only until pilot reports are reviewed. `--delete` only after.
- [ ] Prod backlog clearing is a separate, later decision from establishing that the
      sweep is correct.

**Reassurance worth keeping:** the age filter fails safe. Nothing makes a new S3
object look old — `CopyObject`, re-upload, and restore-from-backup all set a fresh
`LastModified`. The failure direction is "skips junk this week," never "deletes a
live file."

## Verification environment

- [ ] Use the `libis/rdm-deployment` integration env for the above rather than pilot
      where possible — it can simulate the orphan scenarios directly.

## Spec hygiene (applies when picking this up)

- [ ] **Tag which claims in `cache_design.md` are verified versus assumed.** One
      section does this well — "verified against Dataverse `develop`, 2026-08-06" —
      and it's why that section is trustworthy. Elsewhere claims sit at the same
      confidence without provenance. An agent cannot tell the difference and will
      implement assumptions as settled facts. Specifically worth marking:
      - the flush cycle bounding the unregistered window to minutes
      - S3 objects never being visible mid-write (single PUT and multipart)
      - iRODS sizes being authoritative including 0
- [ ] Re-check the risk classification itself. It was made once, up front, and spec
      depth is locked to it. Revisit when your own review involvement changes —
      the same lag that left backend coverage at 28% after you started trusting
      agent changes on Go.

## Adjacent prerequisite

- [ ] **Nightly CI.** This plan leans on the test suite at every step, and nothing
      currently runs it automatically. A `schedule`-only workflow (no `push`, no
      `pull_request`) sends failure mail to whoever last edited the workflow file —
      i.e. you, not colleagues. Confirm on one repo before announcing it.