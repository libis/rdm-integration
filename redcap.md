# REDCap2 Plugin Design, Status, And Implementation Plan

**Navigation:** [← Back to README](README.md#available-plugins)

## Table of Contents

- [Summary](#summary)
- [Current Implementation Status (2026-03-10)](#current-implementation-status-2026-03-10)
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

### Generated File Layout (Implemented)

**Report mode** (`exportMode: "report"`):

1. `redcap/report-<id>/data.csv` or `data.json`
2. `redcap/report-<id>/metadata.csv` (filtered to exported fields)
3. `redcap/report-<id>/project_info.json`
4. `redcap/report-<id>/events.csv` (longitudinal projects)
5. `redcap/report-<id>/form_event_mapping.csv` (longitudinal projects)
6. `redcap/report-<id>/manifest.json` (export config + timestamp + REDCap version + warnings)

**Records mode** (`exportMode: "records"`):

1. `redcap/records/data.csv` or `data.json`
2. `redcap/records/metadata.csv` (filtered to exported fields)
3. `redcap/records/project_info.json`
4. `redcap/records/events.csv` (longitudinal projects)
5. `redcap/records/form_event_mapping.csv` (longitudinal projects)
6. `redcap/records/manifest.json`

### Not Implemented Yet

1. XML data export.
2. Advanced de-identification modes beyond `blank` (drop/mask/pseudonymize/encrypt).
3. DDI-CDI/Croissant/RO-Crate metadata exporters.
4. Attachment/file-field download modes.

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
3. `recordType`: `flat` or `eav`
4. `csvDelimiter`: comma or tab
5. `rawOrLabel`: `raw`, `label`, or `both`
6. `rawOrLabelHeaders`: `raw` or `label`
7. `variables[]` with anonymization mode: `none` or `blank`

### Report Mode Only

8. `reportId` (required — entered manually; REDCap API has no report-listing endpoint)

### Records Mode Only

9. `fields`
10. `forms`
11. `events`
12. `records`
13. `filterLogic`
14. `dateRangeBegin`
15. `dateRangeEnd`
16. `exportSurveyFields`: include survey identifier and timestamp fields (default `false`)
17. `exportDataAccessGroups`: include Data Access Group field (default `false`)

### Planned Controls

1. XML output support

### Attachment Controls

1. `include_attachments`: default `false`
2. `attachments_mode`: `reference-only` or `download`
3. `attachments_max_size_mb`

Rationale:

1. For many projects, upload/file fields should remain references in MVP.
2. Full attachment download can be expensive and should be explicit.

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

### Policy Model

De-identification should be policy-driven, not ad-hoc. The built-in REDCap parameters described [above](#redcap-built-in-de-identification-parameters) should be used as the first layer (server-side stripping), with our policy model applied as a second layer (client-side transforms).

Suggested policy file (`redcap2-policy.json`):

1. `drop_fields`: remove columns entirely
2. `blank_fields`: keep column but replace all values with empty values
3. `mask_rules`: regex or function-based transforms
4. `pseudonymize_fields`: deterministic irreversible tokenization
5. `encrypt_fields`: reversible encryption

### Methods

1. **Server-side suppression (NEW — via built-in REDCap parameters)**
   - `exportSurveyFields=false`: suppress survey identifier and timestamp fields
   - `exportDataAccessGroups=false`: suppress data access group field
   - safest option — data never leaves REDCap
2. **Drop**
   - safest client-side option for direct identifiers
3. **Blank**
   - preserves schema, no values
   - can be auto-applied to REDCap identifier-tagged fields
4. **Deterministic pseudonymization (non-reversible)**
   - e.g. HMAC-based token with secret key
   - consistent per value, not reversible
5. **Reversible encryption**
   - only if strictly required
   - requires key management, key rotation, audit policy, and strict access controls

Important:

1. "Anonymized and reversible" is not anonymous in strict privacy sense.
2. If reversibility is needed, call it pseudonymization/encryption and treat it as sensitive.

### Recommended Defaults

1. Use server-side suppression (`exportSurveyFields=false`, `exportDataAccessGroups=false`) as the baseline.
2. Auto-blank REDCap identifier-tagged fields (detected from metadata) by default; allow user override.
3. Default to `blank` or `drop` for any remaining known identifiers.
4. Make reversible encryption opt-in and disabled by default.
5. Store no raw keys in job payloads or logs.

[↑ Back to Top](#redcap2-plugin-design-status-and-implementation-plan) | [→ Metadata Outputs](#metadata-outputs)

---

## Metadata Outputs

Requested targets:

1. DDI-CDI
2. Croissant (including CDIF profile compatibility target)
3. RO-Crate

### Recommended Strategy

Use one internal normalized metadata model, then fan out to exporters.

Normalized model should include:

1. project-level metadata
2. table/file-level metadata
3. variable-level metadata
4. code lists/value labels
5. provenance (source report/mode, options, timestamp)

Then:

1. emit `*.jsonld` for DDI-CDI
2. emit `croissant.json` (or JSON-LD form as needed by tooling)
3. emit `ro-crate-metadata.json`

### Integration with Existing DDI-CDI Stack

Option A:

1. Generate CSV + metadata sidecars in `redcap2`
2. Use existing DDI-CDI generation pipeline on resulting tabular files

Option B:

1. Add a direct REDCap->DDI-CDI generator path
2. Reuse helper code from existing `ddi-cdi` components where practical

MVP recommendation: Option A.

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
8. Add unit tests for each parameter combination.

### Phase 4: De-Identification Engine [Next]

1. Add policy schema and validation.
2. Implement field-level transforms (drop/blank/mask/pseudonymize).
3. Add optional reversible encryption with key-provider abstraction.
4. Add audit/provenance output listing transformed fields and method.
5. Add strict safeguards:
   - no key logging
   - no raw-value logging
   - secure defaults

### Phase 5: Metadata Exporters [Next]

1. Define normalized metadata model.
2. Implement exporter adapters:
   - DDI-CDI
   - Croissant
   - RO-Crate
3. Expose format toggles in UI.
4. Add schema validation tests for each output type.

### Phase 6: Hardening And Rollout [Next]

1. Performance test with large REDCap projects.
2. Security review (keys, logs, PII handling, transport).
3. Add operator documentation and troubleshooting.
4. Run pilot with limited users.
5. Keep `redcap` plugin as stable fallback until `redcap2` is proven.

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
3. Which de-identification policy should be default at KU Leuven:
   - drop identifiers
   - blank identifiers
   - deterministic pseudonymization
4. Are reversible transformations acceptable under institutional policy?
5. Should metadata outputs be generated during sync, after sync, or both?
6. Should attachments be supported in MVP or deferred?

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
