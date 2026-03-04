# REDCap2 Plugin Design And Implementation Plan

**Navigation:** [← Back to README](README.md#available-plugins)

## Table of Contents

- [Summary](#summary)
- [Why Report-First Is The Right Default](#why-report-first-is-the-right-default)
- [Target User Flow](#target-user-flow)
- [Syncable File Model](#syncable-file-model)
- [Export Controls](#export-controls)
- [De-Identification And Encryption](#de-identification-and-encryption)
- [Metadata Outputs](#metadata-outputs)
- [Architecture In rdm-integration](#architecture-in-rdm-integration)
- [Step-By-Step Implementation Plan](#step-by-step-implementation-plan)
- [Testing Plan](#testing-plan)
- [Open Questions](#open-questions)
- [References](#references)

[↑ Back to Top](#redcap2-plugin-design-and-implementation-plan)

---

## Summary

This document proposes a new plugin, `redcap2`, to coexist with the current `redcap` plugin.

- Keep current `redcap` unchanged (File Repository mode).
- Add `redcap2` for direct API exports (without manual "export then save to File Repository").
- Start with a **report-first** workflow, then expand to full record export controls.

Key point: current behavior requiring manual export/save is expected because the existing plugin only uses REDCap `fileRepository` list/export actions.

[↑ Back to Top](#redcap2-plugin-design-and-implementation-plan) | [→ Why Report-First Is The Right Default](#why-report-first-is-the-right-default)

---

## Why Report-First Is The Right Default

Report export is the best first target for `redcap2`:

1. It matches how many REDCap users already work ("My Reports & Exports").
2. Report definitions provide field selection and filter logic in REDCap UI.
3. API supports report export by `report_id`.
4. It minimizes frontend complexity for MVP.

Recommendation:

1. `redcap2` MVP should support `report_id` export first.
2. Add advanced record export mode as second phase.

Note:

- We should verify whether the REDCap API on our server exposes a "list reports" endpoint.
- If not available, the first MVP can accept manual `report_id` entry (visible in REDCap report list UI).

[↑ Back to Top](#redcap2-plugin-design-and-implementation-plan) | [→ Target User Flow](#target-user-flow)

---

## Target User Flow

### MVP Flow (Report-First)

1. User selects `REDCap2` source plugin.
2. User enters:
   - REDCap URL
   - REDCap API token
   - Report ID (manual input or dropdown if API listing exists)
3. User configures export options in an intermediate "Export Settings" panel:
   - format (`csv`/`json`/`xml`)
   - delimiter for CSV (`,` or tab)
   - raw/label options
4. Compare step shows generated virtual files.
5. User selects files and syncs to Dataverse.

### Advanced Flow (Record Mode)

1. User chooses "Record export mode" instead of report mode.
2. User sets optional filters:
   - fields
   - forms
   - events
   - records
   - filter logic
   - date range
   - record type (`flat`/`eav`)
3. Plugin generates data + metadata files according to config.
4. Compare and sync as usual.

[↑ Back to Top](#redcap2-plugin-design-and-implementation-plan) | [→ Syncable File Model](#syncable-file-model)

---

## Syncable File Model

`redcap2` should expose **generated virtual files** through `Query()` and `Streams()`.

Suggested naming (report mode):

1. `redcap2/report-<id>/data.csv`
2. `redcap2/report-<id>/schema/redcap_metadata.csv`
3. `redcap2/report-<id>/schema/instruments.csv`
4. `redcap2/report-<id>/schema/events.csv` (longitudinal only)
5. `redcap2/report-<id>/schema/form_event_mapping.csv` (longitudinal only)
6. `redcap2/report-<id>/manifest/export-config.json`
7. `redcap2/report-<id>/manifest/provenance.json`

Suggested naming (record mode):

1. `redcap2/records/<scope>/records.flat.csv` or `records.eav.csv`
2. same schema + manifest sidecars as above

Design requirements:

1. Deterministic path/ID based on mode + options.
2. Stable hashing for change detection.
3. Each generated file can be independently selected in the tree.

[↑ Back to Top](#redcap2-plugin-design-and-implementation-plan) | [→ Export Controls](#export-controls)

---

## Export Controls

### Core Controls (MVP)

1. `mode`: `report` or `records`
2. `report_id` (required for report mode)
3. `format_type`: `csv`/`json`/`xml`
4. `csv_delimiter`: comma or tab
5. `raw_or_label`
6. `raw_or_label_headers`
7. `export_checkbox_labels`

### Advanced Record Controls

1. `fields` (variable subset)
2. `forms`
3. `events`
4. `records` (record IDs subset)
5. `filter_logic`
6. `dateRangeBegin`
7. `dateRangeEnd`
8. `record_type`: `flat`/`eav`

### Attachment Controls

1. `include_attachments`: default `false`
2. `attachments_mode`: `reference-only` or `download`
3. `attachments_max_size_mb`

Rationale:

1. For many projects, upload/file fields should remain references in MVP.
2. Full attachment download can be expensive and should be explicit.

[↑ Back to Top](#redcap2-plugin-design-and-implementation-plan) | [→ De-Identification And Encryption](#de-identification-and-encryption)

---

## De-Identification And Encryption

### Policy Model

De-identification should be policy-driven, not ad-hoc.

Suggested policy file (`redcap2-policy.json`):

1. `drop_fields`: remove columns entirely
2. `blank_fields`: keep column but replace all values with empty values
3. `mask_rules`: regex or function-based transforms
4. `pseudonymize_fields`: deterministic irreversible tokenization
5. `encrypt_fields`: reversible encryption

### Methods

1. **Drop**
   - safest for direct identifiers
2. **Blank**
   - preserves schema, no values
3. **Deterministic pseudonymization (non-reversible)**
   - e.g. HMAC-based token with secret key
   - consistent per value, not reversible
4. **Reversible encryption**
   - only if strictly required
   - requires key management, key rotation, audit policy, and strict access controls

Important:

1. "Anonymized and reversible" is not anonymous in strict privacy sense.
2. If reversibility is needed, call it pseudonymization/encryption and treat it as sensitive.

### Recommended Defaults

1. Default to `blank` or `drop` for known identifiers.
2. Make reversible encryption opt-in and disabled by default.
3. Store no raw keys in job payloads or logs.

[↑ Back to Top](#redcap2-plugin-design-and-implementation-plan) | [→ Metadata Outputs](#metadata-outputs)

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

[↑ Back to Top](#redcap2-plugin-design-and-implementation-plan) | [→ Architecture In rdm-integration](#architecture-in-rdm-integration)

---

## Architecture In rdm-integration

### Backend (this repo)

Add:

1. `image/app/plugin/impl/redcap2/common.go`
2. `image/app/plugin/impl/redcap2/options.go`
3. `image/app/plugin/impl/redcap2/query.go`
4. `image/app/plugin/impl/redcap2/streams.go`
5. `image/app/plugin/impl/redcap2/metadata.go` (optional initially)
6. `image/app/plugin/impl/redcap2/deidentify.go`
7. `image/app/plugin/impl/redcap2/exporters/` (for DDI-CDI/Croissant/RO-Crate)

Update:

1. `image/app/plugin/registry.go` with `redcap2`
2. `image/app/frontend/default_frontend_config.json` add `redcap2` entry
3. request/option handling if extra params are needed beyond existing fields

### Frontend (separate repo)

Add `redcap2` plugin UX:

1. report selection input/dropdown
2. export settings panel
3. de-identification config panel (later phase)
4. metadata format toggles

Constraint:

Current generic request model is string-heavy (`option`, `repoName`, etc.). For advanced controls, we should add a structured `pluginOptions` payload rather than overloading one string field.

[↑ Back to Top](#redcap2-plugin-design-and-implementation-plan) | [→ Step-By-Step Implementation Plan](#step-by-step-implementation-plan)

---

## Step-By-Step Implementation Plan

### Phase 0: Design Lock

1. Confirm whether report listing endpoint exists on target REDCap instance.
2. Confirm minimum REDCap version and API rights assumptions.
3. Lock MVP scope:
   - report export only
   - csv/json/xml
   - schema sidecars
   - no attachment download
   - no reversible encryption in MVP

### Phase 1: Backend `redcap2` MVP

1. Scaffold `redcap2` plugin package.
2. Implement API client helpers for report export + metadata export.
3. Implement `Query()` to create virtual nodes for generated files.
4. Implement `Streams()` to generate bytes on demand.
5. Implement deterministic hashes in `Query()` for generated files.
6. Add logging, error handling, and timeout strategy for long exports.
7. Register plugin in `registry.go`.

### Phase 2: Frontend MVP Wiring

1. Add `redcap2` entry to frontend config.
2. Add required fields:
   - URL
   - token
   - report ID
   - export format/delimiter
3. Pass settings into compare/stream requests.
4. Verify compare tree and sync workflow end-to-end.

### Phase 3: Record Mode Controls

1. Add record-mode API path.
2. Add fields/forms/events/records/filter/date-range options.
3. Add flat/eav export mode.
4. Add unit tests for each parameter combination.

### Phase 4: De-Identification Engine

1. Add policy schema and validation.
2. Implement field-level transforms (drop/blank/mask/pseudonymize).
3. Add optional reversible encryption with key-provider abstraction.
4. Add audit/provenance output listing transformed fields and method.
5. Add strict safeguards:
   - no key logging
   - no raw-value logging
   - secure defaults

### Phase 5: Metadata Exporters

1. Define normalized metadata model.
2. Implement exporter adapters:
   - DDI-CDI
   - Croissant
   - RO-Crate
3. Expose format toggles in UI.
4. Add schema validation tests for each output type.

### Phase 6: Hardening And Rollout

1. Performance test with large REDCap projects.
2. Security review (keys, logs, PII handling, transport).
3. Add operator documentation and troubleshooting.
4. Run pilot with limited users.
5. Keep `redcap` plugin as stable fallback until `redcap2` is proven.

[↑ Back to Top](#redcap2-plugin-design-and-implementation-plan) | [→ Testing Plan](#testing-plan)

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

[↑ Back to Top](#redcap2-plugin-design-and-implementation-plan) | [→ Open Questions](#open-questions)

---

## Open Questions

1. Can we list reports over API on the target REDCap instance, or must users provide `report_id` manually?
2. Which de-identification policy should be default at KU Leuven:
   - drop identifiers
   - blank identifiers
   - deterministic pseudonymization
3. Are reversible transformations acceptable under institutional policy?
4. Should metadata outputs be generated during sync, after sync, or both?
5. Should attachments be supported in MVP or deferred?

[↑ Back to Top](#redcap2-plugin-design-and-implementation-plan) | [→ References](#references)

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

[↑ Back to Top](#redcap2-plugin-design-and-implementation-plan)
