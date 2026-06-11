# REDCap Integration (redcap2 plugin) — User Guide

This guide explains how to export data from REDCap into a Dataverse dataset
with the `redcap2` plugin, including de-identification (anonymization),
pseudonymization key management, and the metadata files that are generated
with every export.

Design and implementation details live in [redcap.md](redcap.md). This
document is for end users.

## Table of Contents

1. [Overview](#overview)
2. [Prerequisites](#prerequisites)
3. [Connecting to REDCap](#connecting-to-redcap)
4. [Export settings](#export-settings)
5. [Variable anonymization](#variable-anonymization)
6. [Pseudonymization keys](#pseudonymization-keys)
7. [Generated files](#generated-files)
8. [Metadata sidecars and previewers](#metadata-sidecars-and-previewers)
9. [New dataset metadata prefill](#new-dataset-metadata-prefill)
10. [The export manifest](#the-export-manifest)
11. [Configuration (operators)](#configuration-operators)
12. [Limitations and good practice](#limitations-and-good-practice)

## Overview

The plugin exports REDCap data through the REDCap API and uploads the
resulting files to a Dataverse dataset using the regular compare → sync
workflow:

1. Select **REDCap** as the repository type on the connect page and enter your
   REDCap URL and API token.
2. Configure the export on the **REDCap export settings** page (report or
   records mode, format, filters, anonymization).
3. Press **Continue to compare**: the export is generated server-side and shown
   as a list of files compared against the dataset content.
4. Deselect any files you do not want, then synchronize.

Nothing is uploaded until you confirm in the compare step.

## Prerequisites

- A REDCap API token for the project (REDCap → API page). The token's **Data
  Export Rights** are enforced by REDCap itself and are your first line of
  de-identification:
  - *Full Data Set*: everything your role can see is exported.
  - *De-Identified*: REDCap removes identifier-tagged fields, free-text fields
    and dates, and hashes the record ID — before the data reaches this tool.
  - *Remove All Identifier Fields*: identifier-tagged fields are removed.
- Permission to publish the data in the destination dataset. Anonymization in
  this tool is a convenience layer, not a substitute for your institution's
  data protection assessment.

## Connecting to REDCap

On the connect page choose the REDCap repository type, fill in the REDCap
server URL and your API token, choose the destination dataset (or "New
Dataset"), and continue. You will be redirected to the REDCap export settings
page.

## Export settings

Two export modes:

- **Report**: export a saved report by its ID (find it in REDCap under *My
  Reports & Exports*). The report definition controls fields, records, and
  filters. Reports are always exported flat.
- **All records**: export project records directly, with optional filters:
  fields, forms, events, record IDs, filter logic (e.g. `[age] > 30`), and a
  date range. Records mode also offers:
  - **Record type**: *Flat* (one row per record) or *EAV* (one row per value:
    `record, [event,] field_name, value`).
  - **Include survey fields**: adds `redcap_survey_identifier` and timestamp
    columns. The survey identifier can directly identify respondents — leave
    this off unless you need it.
  - **Include Data Access Groups**: adds the DAG column (only honored by
    REDCap if the project has DAGs and your API user is not in one).

Shared options:

- **Data format**: CSV or JSON.
- **CSV delimiter**: comma or tab.
- **Raw / Label**: export stored values (`raw`) or their human-readable labels
  (`label`).
- **Header labels** (flat CSV only): column headers as variable names or
  field labels.

## Variable anonymization

The **Variable anonymization** panel lists the variables of the selected
report (after *Load variables*) or of the whole project (records mode).
Variables that REDCap tags as identifiers are pre-set to *Blank*. Free-text
fields (notes, unvalidated text) carry a warning icon: they can contain
identifying information even when not tagged.

Per variable you can choose:

| Mode | Effect |
|------|--------|
| None | Exported unchanged. |
| Blank | Values are emptied; the column/rows remain. |
| Drop | The variable is removed entirely — from the data and from the exported data dictionary. |
| Pseudonymize | Values are replaced by irreversible HMAC-SHA256 codes (hex). The same value with the same key always yields the same code, so linkage across exports is preserved. |

Details worth knowing:

- A rule on a checkbox field (e.g. `phones`) covers all its expansion columns
  (`phones___1`, `phones___2`, ...); a rule on a single expansion column
  covers only that column.
- In EAV exports, a transform on the record-ID field is also applied to the
  `record` linking column. Dropping the record-ID field is not possible in
  EAV (it would either break the structure or silently keep the identifiers) —
  use pseudonymize or blank.
- Every rule is audited in the manifest with the number of columns/rows it
  touched; a rule that matched nothing produces an explicit warning.

## Pseudonymization keys

When at least one variable is set to *Pseudonymize*, a key field appears.

- **You manage the key, not the server.** Generate one with:

  ```bash
  openssl rand -base64 32
  ```

  and paste the base64 string into the key field. (32 random bytes is the
  recommended size; the minimum accepted is 16 bytes.)
- **Store the key safely** (e.g. in your institution's password manager). The
  same key reproduces the same pseudonyms in future exports — that is what
  makes longitudinal updates linkable. Without the key, new exports cannot be
  linked to old ones. With the key, anyone holding the original values can
  re-compute the mapping, so treat the key as confidential.
- The key itself never appears in the generated files or logs. The manifest
  records only a *fingerprint* (a hash of the key), so you can verify later
  which key was used.
- Pseudonymization is irreversible: there is no decryption. This is by
  design (institutional decision; reversible encryption is out of scope).

## Generated files

Every export produces one folder (`redcap/report-<id>/` or `redcap/records/`)
containing:

| File | Content |
|------|---------|
| `data.csv` / `data.json` | The records, with anonymization applied. |
| `metadata.csv` | The REDCap data dictionary, filtered to exported fields (dropped variables are excluded). |
| `project_info.json` | REDCap project information. |
| `events.csv`, `form_event_mapping.csv` | Longitudinal projects only. |
| `project_metadata.xml` | CDISC ODM project metadata (metadata only, no data). |
| `croissant.json` | Croissant 1.0 dataset description (ML-ready metadata). |
| `ro-crate-metadata.json` | RO-Crate 1.2 crate with provenance of the export. |
| `ddi-cdi.jsonld` | DDI-CDI 1.0 variable-level description. |
| `manifest.json` | Export parameters, anonymization audit, provenance. |

All files are generated; none are mandatory uploads. Deselect what you do not
need in the compare step.

Per-record file attachments (REDCap *file upload* fields) are **not**
exported; the manifest documents which fields hold attachments.

## Metadata sidecars and previewers

The three metadata sidecars are generated from the same normalized model, so
they always agree with each other and with the anonymized data (e.g. dropped
variables are absent everywhere, pseudonymized variables are marked):

- `ro-crate-metadata.json` is uploaded with the RO-Crate mime type that
  Dataverse (6.3+) also detects by filename; the standard **RO-Crate
  previewer** picks it up.
- `ddi-cdi.jsonld` is uploaded with the DDI-CDI profile mime type registered
  by the **CDI previewer** (`conf/dataverse/external-tools/04-cdi-previewer.json`),
  which validates against the official DDI-CDI 1.0 SHACL shapes.
- `croissant.json` is uploaded as `application/ld+json`; the generic
  **JSON-LD previewer** (`conf/dataverse/external-tools/12-jsonld-previewer.json`,
  same viewer as the CDI previewer) displays it. There is no
  Croissant-specific previewer in the Dataverse ecosystem yet. The Croissant
  CDIF profile ("Semantic Croissant") is still draft-stage; the file targets
  plain Croissant 1.0 and can be validated with
  `pip install mlcroissant && mlcroissant validate --jsonld croissant.json`.

## New dataset metadata prefill

When you create a **new** dataset as the destination, the metadata-copy step
offers values mapped from the REDCap project:

- Title ← project title
- Description ← project notes (+ "purpose, specify" text)
- Author ← principal investigator
- Grant number ← project grant number
- Other ID ← IRB number and a `urn:redcap:...:project:<id>` reference

You select which of these to copy; nothing is applied automatically.

## The export manifest

`manifest.json` is the audit record of the export. It contains:

- the export mode and all export parameters (with one privacy exception
  below), REDCap version, project id/title, and generation timestamp;
- the **anonymization audit**: per rule, the mode and how many columns/rows it
  actually touched, with explicit warnings for rules that matched nothing;
- the pseudonymization method and key fingerprint (never the key);
- attachments documentation (file-upload fields that were not exported);
- `dictionary_fields_not_exported`: dictionary fields missing from an
  unfiltered records export — this reveals server-side stripping by your
  token's export rights.

Privacy exception: if you filtered by specific record IDs and the record-ID
field is anonymized, the manifest redacts the record-ID filter (and likewise
filter logic that references anonymized fields) — otherwise the manifest would
leak the very values the transforms removed.

## Configuration (operators)

- `options.redcapHttpTimeout` in the backend config (`BACKEND_CONFIG_FILE`):
  a Go duration string (e.g. `"15m"`) bounding each REDCap API request.
  Default: `5m`. Raise it if exports of very large projects time out — the
  export itself is fast (hundreds of MB/s for processing); the timeout covers
  the REDCap server generating and sending the data.
- The JSON-LD previewer for `croissant.json` requires registering
  `conf/dataverse/external-tools/12-jsonld-previewer.json` in Dataverse (the
  DDI-CDI and RO-Crate previewers use the existing registrations).

## Limitations and good practice

- **Free text can contain anything.** Blanking identifier-tagged fields does
  not clean names or phone numbers typed into notes fields. The variables
  table flags such fields; review them before exporting.
- **Labels can leak too.** When exporting labels (`Raw / Label = Label`),
  choice labels are data from the dictionary, not record values — but check
  custom labels for embedded identifying text.
- **Token rights are the foundation.** Prefer a token with *De-Identified*
  export rights when you do not need identifiers at all; the client-side
  transforms then act as a second layer.
- **Same settings, same bytes.** Exports are deterministic: unchanged data
  with unchanged settings produces identical files, so re-running a sync only
  uploads what actually changed.
- Attachments, reversible encryption, and a Croissant-CDIF profile are
  intentionally out of scope for now; see [redcap.md](redcap.md) for the
  decision log.
