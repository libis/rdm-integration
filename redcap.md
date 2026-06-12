# REDCap2 Plugin Design, Status, And Implementation Plan

**Navigation:** [← Back to README](README.md#available-plugins)

## Table of Contents

- [Summary](#summary)
- [Current Implementation Status (2026-03-10)](#current-implementation-status-2026-03-10)
- [Review, Research, And Decisions (2026-06-11)](#review-research-and-decisions-2026-06-11)
- [Export Mode Design](#export-mode-design)
- [Target User Flow](#target-user-flow)
- [Syncable File Model](#syncable-file-model)
- [Export Controls](#export-controls)
- [REDCap Built-In De-Identification Parameters](#redcap-built-in-de-identification-parameters)
- [De-Identification And Encryption](#de-identification-and-encryption)
- [Metadata Outputs](#metadata-outputs)
- [Architecture In rdm-integration](#architecture-in-rdm-integration)
- [Step-By-Step Implementation Plan](#step-by-step-implementation-plan)
- [Testing Plan](#testing-plan)
- [Open Questions](#open-questions)
- [References](#references)

[↑ Back to Top](#redcap2-plugin-design-status-and-implementation-plan)

---

## Summary

This document describes the `redcap2` plugin, which coexists with the current `redcap` plugin.

- Keep current `redcap` unchanged (File Repository mode).
- Add `redcap2` for direct API exports (without manual "export then save to File Repository").
- Start with a **report-first** workflow, then expand to more advanced export/de-identification/metadata features.

Key point: manual export/save was required in the old `redcap` plugin because it uses REDCap `fileRepository` list/export actions (`folder_id` / `doc_id` flow).

**PoC branch:** The proof-of-concept is being developed on the `redcap_v2` branch (same branch name in both the backend and frontend repositories).

[↑ Back to Top](#redcap2-plugin-design-status-and-implementation-plan) | [→ Current Implementation Status](#current-implementation-status-2026-03-10)

---

## Current Implementation Status (2026-03-10)

### Implemented

1. New backend plugin `redcap2` was added and registered.
2. `redcap2` supports two export modes selectable in the UI:
   - **Report mode** (`exportMode: "report"`): exports a saved REDCap report by ID via `content=report`.
   - **Records mode** (`exportMode: "records"`): exports all project records via `content=record` with optional filters.
3. `redcap2` supports a variable-list mode for the intermediate settings screen (`pluginOptions.request = "variables"`):
   - In report mode: fetches only the CSV header row of the report (header-only request, avoids full download).
   - In records mode: fetches the full field list from `content=metadata`.
4. `redcap2` `Query()` and `Streams()` generate syncable virtual files directly from REDCap API exports (no file repository dependency).
5. Frontend intermediate page (`/redcap2-export/:id`) with:
   - Report / All records toggle.
   - Report ID field (report mode only).
   - Common export controls: format, record type, CSV delimiter, raw/label, header labels.
   - Record-only filters: fields, forms, events, records, filter logic, date range (records mode only).
   - "Include survey fields" and "Include Data Access Groups" toggles (records mode only, default off).
   - Variable anonymization table with auto-detection of REDCap identifier-tagged fields.
6. End-to-end `pluginOptions` payload propagated through options, compare, and stream/store requests.
7. Export parameter routing is correct per mode:
   - `applySharedExportParams`: `type`, `csvDelimiter`, `rawOrLabel`, `rawOrLabelHeaders` — sent for both modes.
   - `applyRecordOnlyFilters`: `fields`, `forms`, `events`, `records`, `filterLogic`, `dateRangeBegin`, `dateRangeEnd`, `exportSurveyFields`, `exportDataAccessGroups` — sent for records mode only (these are not supported by `content=report`).
8. Bundle cache keyed by `exportMode` + all stable options (including `exportSurveyFields`, `exportDataAccessGroups`); `generatedAt` excluded.
9. REDCap built-in de-identification support:
   - `exportSurveyFields` and `exportDataAccessGroups` exposed as records-mode toggles (server-side suppression).
   - Identifier-tagged fields auto-detected from `content=metadata` (`identifier` column) and pre-selected as `blank` in the variable anonymization table; users can override to `none`.
10. Existing `redcap` plugin remains available and unchanged for fallback.
11. Unit test suite (2026-06-11) covering option parsing/normalization, report-vs-records parameter routing, blank anonymization (CSV and JSON), virtual node generation, hash determinism, bundle caching, and the variables/Options flow (~91% statement coverage, `image/app/plugin/impl/redcap2/*_test.go`).

### Generated File Layout (Implemented)

**Report mode** (`exportMode: "report"`):

1. `redcap/report-<id>/data.csv` or `data.json`
2. `redcap/report-<id>/metadata.csv` (filtered to exported fields; dropped variables excluded)
3. `redcap/report-<id>/project_info.json`
4. `redcap/report-<id>/events.csv` (longitudinal projects)
5. `redcap/report-<id>/form_event_mapping.csv` (longitudinal projects)
6. `redcap/report-<id>/project_metadata.xml` (CDISC ODM, metadata only; failure-tolerant)
7. `redcap/report-<id>/croissant.json` (Croissant 1.0, mime `application/ld+json`)
8. `redcap/report-<id>/ro-crate-metadata.json` (RO-Crate 1.2, profile mime, exact filename for Dataverse detection)
9. `redcap/report-<id>/ddi-cdi.jsonld` (DDI-CDI 1.0, DDI-CDI profile mime)
10. `redcap/report-<id>/manifest.json` (export config + timestamp + REDCap version + audit + warnings)

**Records mode** (`exportMode: "records"`): same layout under `redcap/records/`.

### Not Implemented Yet

1. ~~Advanced de-identification modes beyond `blank`~~ — **done** (Phase 4, 2026-06-11): `drop` + HMAC-SHA256 `pseudonymize` with researcher-managed base64 key. Reversible encryption remains **out of scope** (decision 2026-06-11).
2. ~~DDI-CDI/Croissant/RO-Crate metadata exporters~~ — **done** (Phase 5, 2026-06-11): all three generated on every export from one normalized model (no toggles; deselectable per file in compare — decision revision 2026-06-11), plus `project_metadata.xml` (ODM, metadata-only).
3. Attachment/file-field download — **deferred**; file-upload fields are documented in the manifest instead (decision 2026-06-11).
4. XML **data** export (the metadata-only `content=project_xml` sidecar shipped in Phase 5).
5. Remaining hardening (Phase 6): configurable HTTP timeout, performance test with large projects, security review, pilot re-test.

[↑ Back to Top](#redcap2-plugin-design-status-and-implementation-plan) | [→ Review, Research, And Decisions](#review-research-and-decisions-2026-06-11)

---

## Review, Research, And Decisions (2026-06-11)

A full review of the branch against main plus web research on the REDCap API (triangulated from PHPCap, REDCap.jl, PyCap, REDCapR sources and university changelog mirrors), the integration landscape, and metadata standards produced these findings and decisions.

### Verified REDCap API facts

1. **De-identification is enforced server-side by the token user's Data Export Rights** (No Access `0` / Full `1` / De-Identified `2` / Remove All Identifier Fields `3`, per instrument). "De-Identified" strips tagged identifiers + unvalidated text + notes fields, hashes the record ID, and **removes** date fields on API exports (date *shifting* is an interactive-export option only, not available via API). Token rights are therefore the institutional de-id baseline; the plugin's per-variable transforms are a second layer.
2. `content=report` accepts `report_id`, `format` (`csv`/`json`/`xml`/`odm`), `rawOrLabel`, `rawOrLabelHeaders`, `exportCheckboxLabel`, `csvDelimiter`, `decimalCharacter` — and **no `type` parameter** (reports are always flat) and no record filters. Our parameter routing was already correct; the UI offered a Record type toggle in report mode that had no effect (fixed in Phase 3.9).
3. **`rawOrLabel=both` is not a real parameter** of REDCap 13.x–15.x (a PyCap docstring fossil; PHPCap/REDCap.jl/REDCapR/.NET all validate `raw|label`). Removed from the UI.
4. `rawOrLabelHeaders` applies to CSV flat exports only. `csvDelimiter` applies to CSV only (also accepts `;`, `|`, `^` besides comma/tab).
5. EAV exports have columns `record, [redcap_event_name,] field_name, value` — field names appear as *row values*, not headers. Checkbox fields in flat exports expand to `field___code` columns. Both break naive header-name blanking (fixed in Phase 3.9).
6. `content=project_xml&returnMetadataOnly=true` returns complete project metadata (CDISC ODM 1.3.1, incl. value labels) in one call — the recognized archival gold standard. Never export it *with* data (would bypass blanking).
7. Attachments: `content=file` exports exactly one file per call (record × field × event × instance); no batch endpoint. Confirms deferral.
8. Useful newer parameters to consider later: `exportBlankForGrayFormStatus` (13.x), `combineCheckboxOptions` (15.6.0+, collapses checkbox expansion), `decimalCharacter`, `exportCheckboxLabel`. Project info exports `project_pi_email` since 15.5.20.

### Landscape

1. rdm-integration is the **only REDCap→Dataverse integration referenced in the official Dataverse guides**; no competing maintained tool exists (Fiocruz effort unreleased; datalad-redcap is a stalled prototype that validates our idempotent-hash model).
2. Closest design relative: **Yale YES3 Exporter** (export-specific dictionaries with per-field distributions, de-id conditioning incl. REDCap-compatible date shifting + record-ID hashing, versioned export specs, audit trail) — the model for our manifest/audit evolution.
3. Nobody has published a REDCap→DDI-CDI or REDCap→Croissant mapping — genuine novelty space.
4. Manifest best practice (union of YES3/REDCapExporter/datalad-redcap): REDCap version, project id/title, exporting user + export-rights level, exact API parameters, de-id transforms applied, per-file checksums.

### Metadata standards

1. **Croissant**: 1.0 (2024-03), 1.1 (2026-01). **Dataverse 6.10 (2026-03) ships a built-in Croissant exporter**, but it cannot recover variable labels/value lists from CSVs — a deposited `croissant.json` built from the REDCap dictionary complements it exactly. Validate with `mlcroissant`. Consumed today by Google Dataset Search, NeurIPS (required), HF/Kaggle/OpenML.
2. **RO-Crate**: 1.2 (2025-06) formalizes detached crates; **Process Run Crate** profile fits export-run provenance; Dataverse has a beta previewer that renders a deposited `ro-crate-metadata.json`; KU Leuven already maintains gdcc/exporter-ro-crate.
3. **DDI-CDI**: 1.0 final (2025-01), JSON-LD encoding, no production consumers yet — but LIBIS maintains cdi-viewer (SHACL validation) and this repo already has a DDI-CDI pipeline; strategic in-house value remains high.

### Decisions (2026-06-11)

1. **Default de-id policy**: blank identifier-tagged fields by default (current auto-detection), layered on token export-rights as baseline. `drop`/HMAC `pseudonymize` become opt-in per field in Phase 4.
2. **Reversible encryption**: out of scope (irreversible transforms only).
3. **Metadata exporters**: all three (Croissant + RO-Crate + DDI-CDI) in a single phase from one normalized metadata model, generated during export as bundle virtual files (selectable in the compare tree).
4. **Attachments**: deferred; the manifest documents the project's file-upload fields as not-exported references.
5. **No sidecar toggles** (revision, 2026-06-11): the sidecars are always generated; users deselect unwanted files in the compare step instead of pre-toggling generation. The ODM sidecar is likewise always generated (failure-tolerant).
6. **Pseudonymization keys are researcher-managed** (2026-06-11): base64 key pasted in the UI (min 16 bytes decoded, recommended `openssl rand -base64 32`); server stores nothing, manifest records a SHA-256 fingerprint only.

### Review findings driving Phase 3.9

Both repos build and pass all tests; architecture and `pluginOptions` job-lifecycle wiring (compare → Redis job → worker → Streams) verified sound. The gaps are all in the de-identification path, where silent failure is unacceptable:

1. **P1 — Blanking silently no-ops in EAV mode** (field names are row values in the `field_name` column, not headers). Applies to CSV and JSON EAV.
2. **P1 — Blanking silently no-ops with label headers** (`rawOrLabelHeaders=label` makes headers labels; rules carry field names).
3. **P1 — Checkbox fields leak**: flat exports expand to `field___code` columns that name-equality matching misses; identifier auto-detection misses them too.
4. **P1 — Frontend: variables never load on the first-time report flow** (no trigger on report-ID entry, no reload button) — identifier auto-blanking silently skipped on exactly the first-use path.
5. **P2 — `metadata.csv` near-empty in EAV/label-header modes** (filtered by data headers, which are not field names in those modes).
6. **P2 — UI offered nonexistent API options** (`rawOrLabel=both`; Record type in report mode).
7. **P3 — Bundle cache** holds full exports in RAM with TTL-only eviction (size cap added); compare→store TTL gap can rebuild against changed data (next compare detects it — documented behavior).
8. **P3 — Manifest** lacked export-rights context, project identity, attachment documentation, and per-rule anonymization audit.

[↑ Back to Top](#redcap2-plugin-design-status-and-implementation-plan) | [→ Export Mode Design](#export-mode-design)

---

## Export Mode Design

Both modes are now implemented as peer citizens in the same UI and backend.

### Report Mode (`exportMode: "report"`)

- Exports a saved REDCap report by ID via `content=report`.
- The report definition in the REDCap UI controls which fields, records, and filters are included — no extra filter parameters are sent by the plugin.
- User enters the report ID manually (the standard REDCap API has **no endpoint to list reports**; IDs are visible in "My Reports & Exports" in the REDCap web UI).
- Variable list for anonymization is fetched by a CSV header-only request against the report endpoint (avoids downloading full data just to get field names). Falls back to `content=metadata` if that fails.
- Best choice when: the user has already curated a report in REDCap and wants to export exactly that snapshot.

### Records Mode (`exportMode: "records"`)

- Exports directly via `content=record` with optional server-side filters.
- No report ID needed — works on any project without prior report setup.
- Supports all REDCap record-export filter parameters: `fields`, `forms`, `events`, `records`, `filterLogic`, `dateRangeBegin`, `dateRangeEnd`.
- Variable list for anonymization is fetched from `content=metadata` (all project fields).
- Best choice when: the user wants an ad-hoc export with dynamic filters, or no report has been configured.

### API Parameter Routing

The `content=report` endpoint does **not** accept record-filter parameters.
The split into `applySharedExportParams` and `applyRecordOnlyFilters` enforces this:

| Parameter | Report mode | Records mode |
|---|---|---|
| `type` (flat/eav) | ✓ | ✓ |
| `csvDelimiter` | ✓ | ✓ |
| `rawOrLabel` | ✓ | ✓ |
| `rawOrLabelHeaders` | ✓ | ✓ |
| `fields` | — | ✓ |
| `forms` | — | ✓ |
| `events` | — | ✓ |
| `records` | — | ✓ |
| `filterLogic` | — | ✓ |
| `dateRangeBegin` / `dateRangeEnd` | — | ✓ |
| `exportSurveyFields` | — | ✓ |
| `exportDataAccessGroups` | — | ✓ |
| `report_id` | required | — |

[↑ Back to Top](#redcap2-plugin-design-status-and-implementation-plan) | [→ Target User Flow](#target-user-flow)

---

## Target User Flow

### Report Mode Flow

1. User selects `REDCap Reports (beta)` source plugin.
2. User enters REDCap URL and API token.
3. On the intermediate export settings page:
   - Select **Report** mode (default).
   - Enter **Report ID** (find it in REDCap under "My Reports & Exports").
   - Choose format (`csv`/`json`), record type, delimiter, raw/label options.
   - Optionally configure per-variable anonymization (`none`/`blank`).
4. Compare step shows generated virtual files under `redcap/report-<id>/`.
5. User selects files and syncs to Dataverse.

### Records Mode Flow

1. User selects `REDCap Reports (beta)` source plugin.
2. User enters REDCap URL and API token.
3. On the intermediate export settings page:
   - Select **All records** mode.
   - Choose format, record type, delimiter, raw/label options.
   - Optionally set fields, forms, events, records, filter logic, date range.
   - Optionally configure per-variable anonymization.
4. Compare step shows generated virtual files under `redcap/records/`.
5. User selects files and syncs to Dataverse.

[↑ Back to Top](#redcap2-plugin-design-status-and-implementation-plan) | [→ Syncable File Model](#syncable-file-model)

---

## Syncable File Model

`redcap2` exposes **generated virtual files** through `Query()` and `Streams()`.

File paths per mode:

**Report mode:**

1. `redcap/report-<id>/data.csv` or `data.json`
2. `redcap/report-<id>/metadata.csv`
3. `redcap/report-<id>/project_info.json`
4. `redcap/report-<id>/events.csv` (longitudinal only)
5. `redcap/report-<id>/form_event_mapping.csv` (longitudinal only)
6. `redcap/report-<id>/manifest.json`

**Records mode:**

1. `redcap/records/data.csv` or `data.json`
2. `redcap/records/metadata.csv`
3. `redcap/records/project_info.json`
4. `redcap/records/events.csv` (longitudinal only)
5. `redcap/records/form_event_mapping.csv` (longitudinal only)
6. `redcap/records/manifest.json`

Planned naming extensions (later):

1. Additional metadata sidecars for standards exporters (DDI-CDI, Croissant, RO-Crate).

Design requirements:

1. Deterministic path/ID based on mode + options.
2. Stable hashing for change detection.
3. Each generated file can be independently selected in the tree.

[↑ Back to Top](#redcap2-plugin-design-status-and-implementation-plan) | [→ Export Controls](#export-controls)

---

## Export Controls

### Implemented Controls (both modes)

1. `exportMode`: `report` or `records`
2. `dataFormat`: `csv` or `json`
3. `csvDelimiter`: comma or tab (CSV format only; REDCap also accepts `;`, `|`, `^` — not yet exposed)
4. `rawOrLabel`: `raw` or `label` (`both` removed — not a real REDCap API parameter)
5. `rawOrLabelHeaders`: `raw` or `label` (CSV flat exports only)
6. `variables[]` with anonymization mode: `none` or `blank`

### Report Mode Only

7. `reportId` (required — entered manually; REDCap API has no report-listing endpoint)

Note: report exports are always flat — `content=report` has no `type` parameter.

### Records Mode Only

8. `recordType`: `flat` or `eav` (records mode only; blanking is EAV-aware)
9. `fields`
10. `forms`
11. `events`
12. `records`
13. `filterLogic`
14. `dateRangeBegin`
15. `dateRangeEnd`
16. `exportSurveyFields`: include survey identifier and timestamp fields (default `false`)
17. `exportDataAccessGroups`: include Data Access Group field (default `false`; REDCap only honors it when the project has DAGs and the API user is not in a DAG)

### Possible Future Controls

1. `exportCheckboxLabel`, `decimalCharacter`, `exportBlankForGrayFormStatus`, `combineCheckboxOptions` (15.6.0+)
2. Additional CSV delimiters (`;`, `|`, `^`)

### Attachments (Decision 2026-06-11: Deferred)

File-upload fields are **not downloaded**. The manifest lists the project's file-upload fields (detected from `field_type=file` in the dictionary) so deposits document that binaries exist in REDCap but were deliberately not exported. Rationale: the API allows only one file per call (expensive at scale), and attachment content (consent scans, images) is the most identifying material in a project — none of the tabular de-identification machinery can inspect it. Any future download mode must be opt-in, size-capped, and flagged as not de-identified.

[↑ Back to Top](#redcap2-plugin-design-status-and-implementation-plan) | [→ REDCap Built-In De-Identification Parameters](#redcap-built-in-de-identification-parameters)

---

## REDCap Built-In De-Identification Parameters

The REDCap record-export API (`content=record`) natively supports several de-identification parameters that can be applied **server-side** before data leaves REDCap. This section analyzes these parameters and how they relate to the manual per-variable anonymization we currently implement client-side.

### Available API Parameters

The `content=record` endpoint accepts these de-identification-related parameters:

| Parameter | Type | Default | Description |
|---|---|---|---|
| `exportSurveyFields` | boolean | `false` | Include survey-specific fields (`redcap_survey_identifier`, `[instrument]_timestamp`). Set to `false` to strip them. |
| `exportDataAccessGroups` | boolean | `false` | Include the `redcap_data_access_group` field. Set to `false` to strip it. |
| `exportCheckboxLabel` | boolean | `false` | Export checkbox labels instead of raw values (relevant for label-based anonymization). |
| `filterLogic` | string | — | Server-side record filtering. Already implemented. Can exclude records containing sensitive values. |

Additionally, REDCap Data Dictionaries allow project admins to tag fields with **Identifier** status (`identifier = y`). While this designation is visible in the `content=metadata` export (the `identifier` column), there is **no** API parameter to automatically strip all identifier-tagged fields from a record export. That logic must be implemented client-side by the exporting tool.

### What The REDCap Report-Export API Does NOT Offer

The `content=report` endpoint does **not** accept any of the de-identification parameters above. Reports are exported as configured in the REDCap UI. However, when creating or editing a report in the REDCap web interface, the user can choose:

1. **"Remove all tagged Identifier fields"** — the report definition itself excludes fields marked as identifiers.
2. **"Hash the Record ID"** — the report replaces the record ID with a hashed value.
3. **"Remove all free-text fields"** — strips notes/text fields.
4. **"Remove dates and shift to date"** — date-shifts or removes date fields.

These options are set **in the REDCap UI when creating the report** and take effect before the API returns data. They are not settable via the API at export time.

### Comparison: Built-In vs. Our Current Client-Side Approach

| Capability | REDCap built-in (server-side) | Our current approach (client-side) |
|---|---|---|
| Suppress survey identifier fields | `exportSurveyFields=false` (records mode) | **Implemented** — toggle on settings page (records mode) |
| Suppress Data Access Groups | `exportDataAccessGroups=false` (records mode) | **Implemented** — toggle on settings page (records mode) |
| Strip identifier-tagged fields | Not available as API parameter; only in report definitions | **Implemented** — auto-detected from metadata, pre-selected as `blank` |
| Hash record ID | Report-level setting in REDCap UI only | Not yet implemented |
| Blank/drop arbitrary fields | Not available | `variables[].anonymization = "blank"` per field |
| Remove free-text fields | Report-level setting in REDCap UI only | Not available (would need new mode) |
| Date-shift dates | Report-level setting in REDCap UI only | Not available |
| Exclude specific fields | `fields` param (records mode — positive filter) | `variables[].anonymization = "blank"` per field |
| Server-side record filter | `filterLogic` (records mode) | Already implemented |

### Recommendations

1. ~~**Expose `exportSurveyFields` and `exportDataAccessGroups` as toggles in records mode.**~~ **Done.**
   Implemented as "Include survey fields" and "Include Data Access Groups" checkboxes on the records-mode settings page. Default is `false` (off). Backend sends the parameters in `applyRecordOnlyFilters` only when the user opts in.

2. ~~**Auto-detect identifier-tagged fields from metadata.**~~ **Done.**
   Backend parses the `identifier` column from `content=metadata` CSV and returns `Selected: true` on those fields in the variable-list response. Frontend pre-selects those variables as `blank` in the anonymization table. Users can override to `none`.

3. **For report mode, document that de-identification is best done in the REDCap report definition itself.**
   Since the report API has no de-identification parameters, users should be advised to enable "Remove all tagged Identifier fields", "Hash the Record ID", etc. when creating the report in REDCap. The manifest should record whether the report was configured for de-identification (this info is not available from the API, so it should be a user attestation or checkbox in the UI).

4. **Do not try to replicate date-shifting or record ID hashing client-side in the near term.**
   REDCap's date-shifting uses project-level offsets that are not exposed via the API. Reimplementing this would be complex and fragile. If date-shifting is needed, users should use a report with date-shifting enabled, or use records mode and apply a post-processing step.

5. **Keep the manual per-variable `blank` mode as the primary client-side tool for both modes.**
   It is more flexible than anything REDCap offers at the API level and complements the built-in parameters well. The planned `drop`/`mask`/`pseudonymize` extensions remain valuable for cases that built-in parameters cannot cover.

### Implementation Details

`exportSurveyFields` and `exportDataAccessGroups` backend wiring:

- Two fields added to `pluginOptions`: `ExportSurveyFields bool` and `ExportDataAccessGroups bool`.
- Sent in `applyRecordOnlyFilters` (records-mode only) when the user opts in.
- Included in `bundleCacheKey` for correct cache separation.
- Two checkboxes on the frontend settings page (records mode only, defaults off).

Identifier auto-detection wiring:

- `identifierFieldsFromMetadata()` parses the `identifier` column from the `content=metadata` CSV.
- `listVariablesFromMetadata()` and `listVariablesFromReport()` return `SelectItem` entries with `Selected: true` for identifier-tagged fields.
- Frontend reads the `selected` flag and pre-sets those variables' anonymization to `blank` (user can override to `none`).

Both changes are backward-compatible with the existing payload structure.

[↑ Back to Top](#redcap2-plugin-design-status-and-implementation-plan) | [→ De-Identification And Encryption](#de-identification-and-encryption)

---

## De-Identification And Encryption

### Layered Model (Decisions 2026-06-11)

De-identification is policy-driven and layered:

1. **Layer 0 — Token export rights (server-side, strongest).** REDCap enforces the token user's Data Export Rights on every API export: "De-Identified" strips tagged identifiers + free-text + notes, hashes the record ID, and removes dates; "Remove All Identifier Fields" strips tagged fields. Institutions should issue de-identified-rights tokens where possible. The plugin records the effective stripping in the manifest (dictionary-vs-export column diff).
2. **Layer 1 — Built-in suppression parameters.** `exportSurveyFields=false`, `exportDataAccessGroups=false` (implemented, default off).
3. **Layer 2 — Per-variable client-side transforms.** `blank` (implemented, EAV/checkbox/label-header aware as of Phase 3.9), `drop` and deterministic HMAC `pseudonymize` (Phase 4).

### Methods

1. **Blank** (default for identifier-tagged fields)
   - preserves schema, no values; auto-applied to REDCap identifier-tagged fields, user can override
2. **Drop** (Phase 4)
   - removes the column entirely; safest for direct identifiers when schema preservation is not needed
3. **Deterministic pseudonymization (Phase 4, non-reversible)**
   - HMAC-SHA256 token with a secret key; consistent per value, not reversible
4. **Reversible encryption — OUT OF SCOPE** (decision 2026-06-11)
   - "anonymized and reversible" is not anonymous; if linkability is needed, use deterministic pseudonymization and treat the key as sensitive

### Defaults

1. Token export-rights as institutional baseline (documented, recorded in manifest).
2. Server-side suppression toggles default off (survey fields, DAGs excluded by default).
3. Auto-blank REDCap identifier-tagged fields (detected from metadata) by default; user can override.
4. Flag unvalidated text/notes fields as PHI-risk in the variables UI (Phase 4) — free text is the recognized weak point.
5. Store no raw keys or values in job payloads or logs; every transform is recorded in the manifest's anonymization audit.

[↑ Back to Top](#redcap2-plugin-design-status-and-implementation-plan) | [→ Metadata Outputs](#metadata-outputs)

---

## Metadata Outputs

Decision (2026-06-11): **all three exporters in a single phase**, generated from one normalized metadata model **during export** as virtual files in the same bundle (deterministic, cacheable, individually selectable in the compare tree).

Targets and their research-validated value:

1. **Croissant 1.0** (`croissant.json`) — highest external value, lowest effort. Complements Dataverse 6.10's built-in Croissant exporter, which cannot recover variable labels/value lists from CSVs. RecordSet/Field model maps near-1:1 from the REDCap dictionary (field→Field, label→description, choices→`sc:Enumeration` RecordSets, validation→dataType); FileObjects carry the bundle's MD5 hashes. Validate with `mlcroissant` in CI. Target 1.0 for Dataverse-ecosystem consistency; 1.1/RAI later.
2. **RO-Crate 1.2** (`ro-crate-metadata.json`) — packaging + provenance. Use the **Process Run Crate** profile to describe the export run (REDCap instance/project/version, export parameters, tool version, timestamp). Rendered by the Dataverse beta previewer; aligns with KU Leuven's gdcc/exporter-ro-crate work. Plain schema.org JSON-LD — writable from Go without a library.
3. **DDI-CDI 1.0** (`ddi-cdi.jsonld`) — highest fidelity (variable cascade, substantive/sentinel value domains for missing codes, wide-table structure), strategic for KU Leuven (existing in-repo DDI-CDI pipeline + LIBIS cdi-viewer for SHACL validation), and genuine novelty (no published REDCap→DDI-CDI mapping exists). Reuse existing ddi-cdi helpers where practical, enriched with dictionary labels/value domains the generic CSV profiler cannot infer.

Normalized model (one struct, three emitters):

1. project-level metadata (`project_info` + dictionary)
2. table/file-level metadata (bundle files, hashes, sizes, delimiters)
3. variable-level metadata (names, labels, types, validation, checkbox expansions)
4. code lists/value labels (`select_choices_or_calculations`)
5. provenance (source mode/report, options, timestamps, anonymization audit, REDCap version)

Additional Phase 5 items:

1. Implement the plugin `Metadata()` hook (registry already supports it; github/gitlab set the precedent) to prefill Dataverse citation metadata from project info (title, PI name, `project_pi_email` on 15.5.20+, notes).
2. Metadata-only CDISC ODM sidecar via `content=project_xml&returnMetadataOnly=true` (`project_metadata.xml`) — one API call, archival gold standard, always generated (failure-tolerant). Never with data (would bypass the transforms).

[↑ Back to Top](#redcap2-plugin-design-status-and-implementation-plan) | [→ Architecture In rdm-integration](#architecture-in-rdm-integration)

---

## Architecture In rdm-integration

### Backend (this repo)

1. `image/app/plugin/impl/redcap2/common.go`
2. `image/app/plugin/impl/redcap2/options.go`
3. `image/app/plugin/impl/redcap2/query.go`
4. `image/app/plugin/impl/redcap2/streams.go`
5. `image/app/plugin/registry.go` with `redcap2`
6. `image/app/frontend/default_frontend_config.json` add `redcap2` entry
7. `conf/frontend_config.json` add `redcap2` entry
8. plugin request structs now include `pluginOptions`:
   - `OptionsRequest`
   - `CompareRequest`
   - `StreamParams`

Planned backend extensions (not yet implemented):

1. `image/app/plugin/impl/redcap2/metadata.go`
2. `image/app/plugin/impl/redcap2/deidentify.go`
3. `image/app/plugin/impl/redcap2/exporters/` (DDI-CDI/Croissant/RO-Crate)

### Frontend (separate repo)

Implemented frontend `redcap2` UX:

1. Intermediate export settings page (`/redcap2-export/:id`).
2. Report / All records mode toggle (report mode is default).
3. Report ID field (visible in report mode only).
4. Common export controls: format, record type, delimiter, raw/label, header labels.
5. Record-only filter fields: fields, forms, events, records, filter logic, date range (visible in records mode only).
6. "Include survey fields" and "Include Data Access Groups" toggles (records mode only, default off).
7. Variable anonymization table (`none`/`blank`) with auto-detection of REDCap identifier-tagged fields (pre-selected as `blank`).
8. Generated files preview updates to show mode-appropriate paths.
9. `pluginOptions` payload propagated through options, compare, and stream/store requests.

Planned frontend extensions:

1. Richer de-identification config panel.
2. Metadata format toggles and generators.

Constraint:

Current generic request model is string-heavy (`option`, `repoName`, etc.). `pluginOptions` is now used for structured `redcap2` settings and should remain the extension mechanism.

[↑ Back to Top](#redcap2-plugin-design-status-and-implementation-plan) | [→ Step-By-Step Implementation Plan](#step-by-step-implementation-plan)

---

## Step-By-Step Implementation Plan

### Phase 0: Design Lock [Completed]

1. ~~Confirm whether report listing endpoint exists on target REDCap instance.~~
   Confirmed: standard REDCap API has **no** report-listing endpoint; report ID is entered manually.
2. Confirm minimum REDCap version and API rights assumptions.
3. Lock MVP scope:
   - report mode + records mode (both implemented)
   - csv/json initial scope
   - report-sidecar generation
   - no attachment download
   - no reversible encryption in MVP

### Phase 1: Backend `redcap2` MVP [Completed]

1. Scaffold `redcap2` plugin package.
2. Implement API client helpers for report export + metadata export.
3. Implement `Query()` to create virtual nodes for generated files.
4. Implement `Streams()` to generate bytes on demand.
5. Implement deterministic hashes in `Query()` for generated files.
6. Add logging, error handling, and timeout strategy for long exports.
7. Register plugin in `registry.go`.

### Phase 2: Frontend MVP Wiring [Completed]

1. Add `redcap2` entry to frontend config.
2. Add required fields and intermediate settings page:
   - URL
   - token
   - report ID (text input on export page)
   - export controls (including rawOrLabel, rawOrLabelHeaders)
   - variable anonymization
3. Pass settings into compare/stream requests.
4. Verify compare tree and sync workflow end-to-end.

### Phase 3: Record Mode Controls [Completed]

1. ~~Add record-mode API path (`content=record`).~~
2. ~~Add fields/forms/events/records/filter/date-range options.~~
3. ~~Add flat/eav export mode.~~
4. ~~Separate `applySharedExportParams` from `applyRecordOnlyFilters`.~~
5. ~~Add report/records mode toggle to frontend.~~
6. ~~Expose `exportSurveyFields` and `exportDataAccessGroups` as records-mode toggles.~~
7. ~~Auto-detect identifier-tagged fields from metadata and pre-blank them.~~
8. ~~Add unit tests for each parameter combination.~~

### Phase 3.9: De-Id Correctness And API Fidelity [Completed — 2026-06-11]

Fixes the review findings before new features (see [Review, Research, And Decisions](#review-research-and-decisions-2026-06-11)):

1. EAV-aware blanking: blank the `value` cell of rows whose `field_name` matches a blanked field (CSV and JSON EAV).
2. Checkbox-aware matching: a blank rule for `field` also matches expanded `field___code` columns.
3. Label-header support: when `rawOrLabelHeaders=label`, translate headers back to field names via the dictionary (incl. `Label (choice=...)` checkbox headers) before blanking and metadata filtering.
4. Anonymization audit in the manifest: per-rule match counts; warnings for rules that matched nothing.
5. Correct `metadata.csv` filtering per mode (EAV field_name values; label-header translation; checkbox base names).
6. Frontend: load variables on report-ID entry (blur) + explicit Reload button; concurrent-load guard.
7. Remove `rawOrLabel=both` (not a real API parameter); record type control restricted to records mode; stop sending `type` to `content=report`; send `csvDelimiter`/`rawOrLabelHeaders` only when applicable.
8. Manifest enrichment: project id/title, file-upload-field documentation (attachments decision), dictionary-vs-export column diff (reveals token-rights stripping).
9. Bundle cache size cap (bound PII residency in RAM).

### Phase 4: De-Identification Engine [Completed — 2026-06-11]

1. ~~Add `drop` and deterministic HMAC-SHA256 `pseudonymize` per-variable modes~~ — done. Key: researcher-managed base64 (min 16 bytes, validated client- and server-side with the `openssl rand -base64 32` hint); pseudonyms are full lowercase-hex HMAC-SHA256; empty cells stay empty. EAV: record-ID transforms also rewrite the `record` linking column; dropping the record-ID field in EAV is rejected. Rules match checkbox expansion columns by their own name as well as the base name.
2. ~~Flag unvalidated text/notes fields as PHI-risk~~ — done via `SelectItem.Note` + warning icon in the variables table.
3. Token export-rights context: documented in the manifest (`dictionary_fields_not_exported`, excluding client-side drops) and in the user guide; no extra UI surface (kept light).
4. ~~Extend the anonymization audit~~ — done (mode + matched counts + record-column notes; `anonymization` manifest section with method + key fingerprint).
5. ~~Safeguards~~ — done: key never logged or echoed; manifest redacts the `records` filter when the record-ID field is transformed and `filterLogic` when it references transformed fields; cache key covers the key (hashed).
6. ~~Reversible encryption~~ — out of scope (decision 2026-06-11).
7. Fixed along the way: the variables list is now always fetched with raw, comma-delimited headers (label-header and tab-delimiter exports previously yielded rule names that never matched).

### Phase 5: Metadata Exporters [Completed — 2026-06-11]

1. ~~Normalized metadata model~~ — done (`sidecars.go`: files with md5/size/encoding, variables joined from the post-transform data columns and the dictionary incl. code lists, provenance, key fingerprint).
2. ~~All three exporters~~ — done:
   - `croissant.json` (Croissant 1.0, canonical context, FileObject distribution, CSV RecordSet with schema.org dataTypes; no RecordSet for JSON exports). Mime `application/ld+json`; validate manually with `mlcroissant validate --jsonld`.
   - `ro-crate-metadata.json` (RO-Crate 1.2, detached crate, Process Run Crate provenance: CreateAction + plugin/REDCap SoftwareApplication). Mime = Dataverse 6.3+ filename-detection string = RO-Crate previewer contentType.
   - `ddi-cdi.jsonld` (DDI-CDI 1.0 JSON-LD mirroring `cdi_generator_jsonld.py` structure — WideDataSet/WideDataStructure/LogicalRecord/InstanceVariables/CodeLists/PrimaryKey/PhysicalSegmentLayout — so the cdi-viewer SHACL shapes apply). Mime = deployed CDI previewer contentType (`DdiCdiMimeType`).
3. ~~Generate during export~~ — done; **no UI toggles** (decision revision): always generated, deselectable per file in compare. Per-file mime plumbing added through `tree.Node.Attributes.MimeType` → both upload paths (native multipart Content-Type; direct-upload jsonData mime).
4. ~~Plugin `Metadata()` hook~~ — done (title, notes+purpose, PI, grant number, IRB number, urn:redcap project id).
5. ~~`project_metadata.xml`~~ — done (always generated, failure-tolerant warning).
6. ~~Schema validation tests~~ — structural Go tests for all three outputs + determinism e2e; external validation documented in the user guide.
7. New external tool conf: `conf/dataverse/external-tools/12-jsonld-previewer.json` (cdi-viewer registered for bare `application/ld+json`, fires on croissant.json).
8. **Variable-level metadata + validation fixes (2026-06-12):**
   - `croissant.json` and `ro-crate-metadata.json` now carry `schema:variableMeasured` following the CDIF 1.1 Discovery-profile shape (PropertyValue with name, description, alternateName, numeric minValue/maxValue from `text_validation_min/max`, code lists as `valueReference` DefinedTerms with termCode = the value in the data). Inline in Croissant; flattened contextual entities in RO-Crate (spec requires flattened JSON-LD). Verified: `mlcroissant validate --jsonld` exits clean (only citeAs/license "recommended" warnings).
   - `ddi-cdi.jsonld` code lists restructured per the official DDI-CDI 1.0 SHACL shapes (the ones bundled with the cdi-viewer): each `Code` now `uses_Notation` (TypedString content = the value as it appears in the data) and `denotes` a `Category` (ObjectName = the label); `CodeList` carries `allowsDuplicates` and an ObjectName name; the `PrimaryKey` is reachable via `DataStructure_has_PrimaryKey`; `PrimaryKeyComponent` uses the full `correspondsTo_DataStructureComponent` term. Verified with pyshacl against `libis/cdi-viewer` `shapes/ddi-cdi-official.ttl`: **Conforms = True** (previously 13 violations, the "Less than 1 values" errors seen in the previewer).
   - Validation workflow: `SIDECAR_DUMP_DIR=/tmp/dump go test ./app/plugin/impl/redcap2/ -run TestDumpSidecarsForValidation` writes sample sidecars for pyshacl/mlcroissant runs.
   - **Context note (2026-06-12):** `ddi-cdi.jsonld` references the canonical published DDI-CDI context URL, like the rest of the ecosystem (incl. `cdi_generator_jsonld.py`). The hosted copy (ddi-cdi.github.io/m2t-ng) currently contains stray git conflict markers (invalid JSON — report upstream); strict consumers cannot resolve it until that is fixed. The cdi-viewer previewer is immune: its vendored context fallback was repaired (the fallback path pointed to `shapes/` while the file lives in `public/shapes/`, so it 404'd and the viewer silently expanded documents with an empty context, reporting mass "less than 1 values" violations on correct documents). Viewer fix in the cdi-viewer repo, commit 04547d7.
   - Future work: full CDIF 1.1 Data Description profile (double-typing variableMeasured as `cdi:InstanceVariable` + skos code lists) once the profile leaves review (currently `reviewRevision`, prefixes at 0.1; "Semantic Croissant" has no published pattern yet).

### Phase 6: Hardening And Rollout [In Progress — 2026-06-12]

1. ~~Performance + configurable timeout~~ — done (2026-06-12). `options.redcapHttpTimeout` (Go duration string, default `5m`) in the backend config bounds REDCap API requests. Benchmarks added (`bench_test.go`); measured on dev hardware:
   - flat CSV, 50k rows × 50 cols (~19 MB), pseudonymize+blank+drop: ~150 MB/s (~0.13 s)
   - same input, no rules (parse + audit only): ~460 MB/s
   - EAV CSV, 2.5M value rows (~50 MB) with record-column pseudonymization: ~79 MB/s (~0.64 s) after memoizing record-column HMACs (was ~34 MB/s — the same record ID recurs once per field)
   - all three sidecars for a 500-variable dictionary: ~7.5 ms
   Also removed the per-file payload copy in `Streams` (bundle contents are immutable; halves peak memory while streaming).
2. ~~Security review~~ — done (2026-06-12), see [Security Review](#security-review-2026-06-12).
3. ~~User documentation~~ — done: [REDCAP_INTEGRATION.md](REDCAP_INTEGRATION.md) (features, key generation/management, PHI disclaimer, sidecars/previewers, manifest reference).
4. Re-test on pilot (first pilot deploy of Phases 0–3.9 done 2026-06-11 via `make dev_build`). **Remaining: user re-tests the Phase 4–6 build.**
5. Keep `redcap` plugin as stable fallback until `redcap2` is proven.
6. Revisit attachments (opt-in, size-capped, flagged as not de-identified) based on pilot feedback.

### Security Review (2026-06-12)

Scope: redcap2 plugin, key handling end to end, logging, PII residency, transport.

**Verified safe:**

1. **Pseudonymization key path**: frontend holds the key in in-memory state only (`credentials.service.ts` uses signals, no localStorage/sessionStorage), so a page refresh discards it; it transits to the backend inside `pluginOptions` over HTTPS exactly like repository API tokens; in Redis it exists only inside the queued job payload (`LPush`/`RPop` — removed when the worker pops it, re-added only on retry), the same residency as every plugin's token; it is never logged (audited all `Logger` calls in the plugin and the job pipeline), never echoed in validation errors (tested), never written to any generated file (manifest carries only the SHA-256 fingerprint — tested), and enters the bundle cache key only as MD5 input (one-way).
2. **Logging**: the plugin logs only file counts, export mode, report ID, cache decisions, and sidecar warnings — no record data, no tokens, no key material.
3. **Transport**: the redcap2 HTTP client builds its own `http.Transport`, so it does **not** inherit the `InsecureSkipVerify: true` that `config.init()` sets on `http.DefaultTransport` — REDCap TLS certificates are verified by this plugin.
4. **PII residency in memory**: bundle cache is process-local with a 5-minute TTL, 64 MB per-bundle cap (oversized bundles are rebuilt on demand, never cached), and lazy eviction on every set.
5. **Manifest hygiene**: records filter redacted when the record-ID field is transformed; filter logic redacted when it references transformed fields; client-side drops excluded from the token-rights diff; REDCap API token absent everywhere (POST form body, never URLs or generated files).
6. **Defaults**: `exportSurveyFields` and `exportDataAccessGroups` are off by default (`redcap_survey_identifier` is often directly identifying).

**Accepted/documented (no change):**

1. Queued job payloads in Redis contain the repo token and, when used, the pseudonymization key — pre-existing posture shared by all plugins; mitigate by restricting Redis access (password support exists: `pathToRedisPassword`).
2. REDCap error bodies are surfaced to the user verbatim; REDCap error messages do not echo submitted record data.
3. `common/get_metadata.go` logs the citation-metadata response (project title, PI, ...) — pre-existing app-wide behavior, not record data.
4. The global `http.DefaultTransport` certificate-verification skip in `config.init()` is app-wide and predates this work; flagged for a future app-level review, out of redcap2 scope.

[↑ Back to Top](#redcap2-plugin-design-status-and-implementation-plan) | [→ Testing Plan](#testing-plan)

---

## Testing Plan

### Unit Tests

1. Request payload construction for each mode.
2. Parsing of report/record responses.
3. Virtual node generation and deterministic IDs.
4. Hash determinism for unchanged payloads.
5. De-identification policy behavior.
6. Exporter output shape checks.

### Integration Tests

1. `compare -> sync` with report mode.
2. `compare -> sync` with record mode and filters.
3. Dataverse ingest compatibility for generated data files.
4. DDI-CDI/Croissant/RO-Crate generation from same source snapshot.

### Security Tests

1. Ensure keys are never logged.
2. Verify reversible encryption requires explicit opt-in.
3. Validate redaction of error messages containing sensitive values.

[↑ Back to Top](#redcap2-plugin-design-status-and-implementation-plan) | [→ Open Questions](#open-questions)

---

## Open Questions

1. ~~Do all target REDCap instances expose report listing?~~ **Resolved:** The standard REDCap API does not expose a report-listing endpoint. Report IDs are entered manually.
2. ~~Should record mode be a separate flow or a toggle?~~ **Resolved:** Implemented as a toggle on the same settings page.
3. ~~Which de-identification policy should be default at KU Leuven?~~ **Resolved (2026-06-11):** Blank identifier-tagged fields by default, layered on token export-rights as the institutional baseline; drop/pseudonymize opt-in per field (Phase 4).
4. ~~Are reversible transformations acceptable under institutional policy?~~ **Resolved (2026-06-11):** Out of scope. Irreversible transforms only (blank/drop/HMAC pseudonymize).
5. ~~Should metadata outputs be generated during sync, after sync, or both?~~ **Resolved (2026-06-11):** During export, as virtual files in the bundle (deterministic, cacheable, selectable in the compare tree). All three exporters in one phase.
6. ~~Should attachments be supported in MVP or deferred?~~ **Resolved (2026-06-11):** Deferred; manifest documents file-upload fields as not-exported references. Future download support must be opt-in, size-capped, and flagged as not de-identified.

[↑ Back to Top](#redcap2-plugin-design-status-and-implementation-plan) | [→ References](#references)

---

## References

1. REDCap Reports API (report export by report ID):
   - https://redcap-tools.github.io/PyCap/api_reference/reports/
2. REDCap Records API (field/form/event/filter/date params):
   - https://redcap-tools.github.io/PyCap/api_reference/records/
3. REDCap Metadata API:
   - https://redcap-tools.github.io/PyCap/api_reference/metadata/
4. REDCap Instruments API:
   - https://redcap-tools.github.io/PyCap/api_reference/instruments/
5. REDCap Events API:
   - https://redcap-tools.github.io/PyCap/api_reference/events/
6. REDCap File Repository semantics (`folder_id` vs `doc_id`):
   - https://rdrr.io/github/nutterb/redcapAPI/man/exportFileRepositoryListing.html
7. REDCap report export workflow reference:
   - https://docs.datalad.org/projects/redcap/en/latest/generated/man/datalad-export-redcap-report.html

[↑ Back to Top](#redcap2-plugin-design-status-and-implementation-plan)
