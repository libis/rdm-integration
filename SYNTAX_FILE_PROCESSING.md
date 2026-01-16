# Syntax File Processing for DDI-CDI Generation

> ⚠️ **Document Status**: This is a **design specification** for planned improvements to syntax file handling. The [Current Implementation](#current-implementation-limitations) section describes what exists today; the rest describes the target architecture.

## Table of Contents

- [Overview](#overview)
- [Current Implementation Limitations](#current-implementation-limitations)
- [Understanding Syntax Files](#understanding-syntax-files)
  - [What Are Syntax Files?](#what-are-syntax-files)
  - [SPSS Syntax (.sps)](#spss-syntax-sps)
  - [SAS Data Step (.sas)](#sas-data-step-sas)
  - [Stata Dictionary (.dct, .do)](#stata-dictionary-dct-do)
  - [Inline vs External Data](#inline-vs-external-data)
- [Processing Strategy](#processing-strategy)
  - [Design Principles](#design-principles)
  - [File Relationship Detection](#file-relationship-detection)
  - [Processing Scenarios](#processing-scenarios)
  - [Deduplication Logic](#deduplication-logic)
- [Implementation Details](#implementation-details)
  - [Parsing Data File References](#parsing-data-file-references)
  - [Detecting Inline Data](#detecting-inline-data)
  - [File Matching Algorithm](#file-matching-algorithm)
- [Expected DDI-CDI Output Structure](#expected-ddi-cdi-output-structure)
  - [Overall Structure](#overall-structure)
  - [WideDataSet](#widedataset)
  - [WideDataStructure](#widedatastructure)
  - [Variables](#variables)
  - [Provenance](#provenance)
- [SHACL Validation](#shacl-validation)
  - [Required Properties](#required-properties)
  - [Cardinality Constraints](#cardinality-constraints)
  - [Common Validation Issues](#common-validation-issues)
- [Test Cases](#test-cases)
- [Future Enhancements](#future-enhancements)

---

## Overview

This document describes how the DDI-CDI generator **should** handle statistical syntax files (`.sps`, `.sas`, `.dct`, `.do`) and their relationship to data files. The goal is to provide a **best-effort** approach that:

1. Extracts rich metadata from syntax files (variable labels, categories, value labels)
2. Combines this with data profiling when data files are available
3. Handles all edge cases gracefully
4. Avoids duplicate processing of the same data
5. Supports human-in-the-loop refinement via the CDI Viewer

---

## Current Implementation Limitations

> **This section describes what currently exists as of January 2026.**

The current implementation has several gaps between the design goals and actual behavior:

### What Works Today

1. **CSV/TSV/TAB files**: Fully functional—direct profiling with type inference, statistics, and DDI-CDI output
2. **Dataverse-ingested files**: If Dataverse has ingested a file (`.sav`, `.dta`, etc.), DDI metadata is retrieved via `/metadata/ddi` API and merged with profiling
3. **xconvert invocation**: The `run_xconvert()` function can call the Berkeley xconvert tool to convert syntax files to DDI XML

### What Does NOT Work Today

1. **Syntax files cannot be processed standalone**: The current code path assumes every file in the manifest is a CSV/TSV and tries to open syntax files with `stream_profile_csv()`, which fails

2. **Same-stem matching is backwards**: `detect_and_run_xconvert(csv_path, work_dir)` looks for a syntax file matching a data file's stem (e.g., given `survey.csv`, looks for `survey.sps`). This doesn't help when users select syntax files directly.

3. **Backend filter missing `.do`**: The Go backend ([image/app/common/ddi_cdi.go](image/app/common/ddi_cdi.go)) defines supported extensions but omits `.do`:
   ```go
   supported := map[string]bool{
       "csv": true,
       "tsv": true,
       "tab": true,
       "sps": true,
       "sas": true,
       "dct": true,
       // Missing: "do": true
   }
   ```

4. **No file relationship detection**: The system doesn't parse syntax files to find referenced data files or detect inline data blocks

5. **No deduplication**: If user selects both `survey.sps` and `survey.dat`, both would be processed independently (if syntax processing worked), creating duplicate entries

### Required Changes

To implement the design in this document:

| Component | File | Change Needed |
|-----------|------|---------------|
| Backend filter | `image/app/common/ddi_cdi.go` | Add `"do": true` to supported extensions |
| Manifest builder | `image/app/core/ddi_cdi.go` | Detect syntax files, parse for references, set appropriate flags |
| Python generator | `image/cdi_generator_jsonld.py` | Handle syntax files specially—don't try to CSV-parse them |
| Python generator | `image/cdi_generator_jsonld.py` | Implement `parse_spss_syntax()`, `parse_sas_syntax()`, `parse_stata_syntax()` |
| Python generator | `image/cdi_generator_jsonld.py` | Build file relationship map before processing |

---

## Understanding Syntax Files

### What Are Syntax Files?

Syntax files are **metadata definition files** used in statistical software packages. They describe:

- **Variable names** and positions
- **Variable labels** (human-readable descriptions)
- **Data types** and formats
- **Value labels** (code → meaning mappings, e.g., `1 = "Male"`, `2 = "Female"`)
- **Missing value definitions**
- **Data file references** (where the actual data lives)

**Key insight**: Syntax files contain metadata, not data. The actual data values live in separate files (`.dat`, `.csv`, `.txt`, or embedded inline).

### SPSS Syntax (.sps)

SPSS syntax files use `DATA LIST` to define variable positions and can reference external files:

```spss
* External data file reference
DATA LIST FILE='survey.dat' FIXED RECORDS=1
  /1 ID 1-4 AGE 5-6 GENDER 7 INCOME 8-12.

* Or with inline data
DATA LIST FREE / ID AGE GENDER INCOME.
BEGIN DATA
1 25 1 35000
2 30 2 42000
END DATA.

VARIABLE LABELS
  ID 'Respondent ID'
  AGE 'Age in years'
  GENDER 'Gender of respondent'
  INCOME 'Annual household income'.

VALUE LABELS
  GENDER 1 'Male' 2 'Female' /
  INCOME 1 'Under 25K' 2 '25K-50K' 3 '50K-75K' 4 'Over 75K'.

MISSING VALUES AGE INCOME (-99).

SAVE OUTFILE='survey.sav'.
```

**Key patterns to detect:**
- `FILE='filename'` or `FILE=filename` - external data file
- `INFILE='filename'` - alternative syntax
- `BEGIN DATA ... END DATA` - inline data
- `SAVE OUTFILE='filename'` - output file (not input)

### SAS Data Step (.sas)

SAS uses `INFILE` statements to reference external data:

```sas
* External data file
data survey;
  infile 'survey.dat' lrecl=80;
  input ID 1-4 AGE 5-6 GENDER 7 INCOME 8-12;
  label ID='Respondent ID'
        AGE='Age in years'
        GENDER='Gender of respondent'
        INCOME='Annual household income';
run;

* Or with inline data (datalines/cards)
data survey;
  input ID AGE GENDER $ INCOME;
  datalines;
1 25 Male 35000
2 30 Female 42000
;
run;

proc format;
  value genderfmt 1='Male' 2='Female';
  value incomefmt 1='Under 25K' 2='25K-50K' 3='50K-75K' 4='Over 75K';
run;
```

**Key patterns to detect:**
- `infile 'filename'` or `infile "filename"` - external data file
- `datalines;` or `cards;` followed by data until `;` - inline data
- `set 'filename'` - reading from SAS dataset (different case)

### Stata Dictionary (.dct, .do)

Stata uses dictionary files (`.dct`) that explicitly reference data files:

```stata
* Dictionary file (.dct)
infile dictionary using survey.dat {
  _column(1)  int    id       %4f  "Respondent ID"
  _column(5)  byte   age      %2f  "Age in years"
  _column(7)  byte   gender   %1f  "Gender"
  _column(8)  long   income   %5f  "Annual income"
}

* Or in a .do file
clear
infile id age gender income using "survey.dat"
label variable id "Respondent ID"
label variable age "Age in years"
label define genderlbl 1 "Male" 2 "Female"
label values gender genderlbl

* Inline data
clear
input id age gender income
1 25 1 35000
2 30 2 42000
end
```

**Key patterns to detect:**
- `using filename` or `using "filename"` - external data file
- `input ... end` block - inline data
- `infile dictionary using filename` - dictionary with data reference

### Inline vs External Data

| Type | Description | Data Profiling Possible? |
|------|-------------|-------------------------|
| **Inline data** | Data embedded in syntax file (`BEGIN DATA`, `datalines;`, `input...end`) | No (xconvert extracts structure only) |
| **External reference** | Syntax points to separate file (`FILE=`, `infile`, `using`) | Yes, if file exists and is processable |
| **No data** | Syntax defines structure only, no data reference | No |

---

## Processing Strategy

### Design Principles

1. **Best effort**: Process what we can, warn about what we can't
2. **No duplicates**: Each data file should appear once in output
3. **Syntax files are authoritative for metadata**: When available, prefer labels/categories from syntax over inference
4. **Data files provide statistics**: When available, profile for types, cardinality, distributions
5. **Human in the loop**: CDI Viewer allows corrections; don't aim for perfection
6. **Folder-aware**: Same filename in different folders are different files
7. **Hash-aware**: Duplicate files (same content) may exist; handle gracefully

### File Relationship Detection

When processing begins, build a relationship map:

```
┌─────────────────────────────────────────────────────────────────────┐
│                    PHASE 1: ANALYZE ALL FILES                       │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  For each syntax file (.sps, .sas, .dct, .do):                     │
│    1. Run xconvert → get DDI XML with variable metadata            │
│    2. Parse syntax to find data file reference (if any)            │
│    3. Detect if syntax has inline data                             │
│    4. Record: {                                                     │
│         syntax_file: "folder/survey.sps",                          │
│         ddi_xml: <path to generated DDI>,                          │
│         has_inline_data: false,                                    │
│         references_file: "survey.dat",  // or null                 │
│         referenced_file_found: true,    // did we find it?         │
│         referenced_file_path: "folder/survey.dat"                  │
│       }                                                             │
│                                                                     │
│  Build sets:                                                        │
│    - syntax_files: all .sps, .sas, .dct, .do files                 │
│    - data_files: all .csv, .tsv, .tab, .dat, .txt files           │
│    - referenced_by_syntax: data files pointed to by syntax         │
│    - orphan_data_files: data_files - referenced_by_syntax          │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

### Processing Scenarios

```
┌─────────────────────────────────────────────────────────────────────┐
│                    PHASE 2: PROCESS FILES                           │
├─────────────────────────────────────────────────────────────────────┤
│                                                                     │
│  SCENARIO A: Syntax file with inline data                          │
│  ─────────────────────────────────────────                         │
│  Input:  survey.sps (contains BEGIN DATA...END DATA)               │
│  Output: DDI metadata only (variables, labels, categories)         │
│  Note:   No data profiling (can't extract stats from syntax)       │
│                                                                     │
│  SCENARIO B: Syntax file → data file (found, processable)          │
│  ─────────────────────────────────────────────────────────         │
│  Input:  survey.sps (references survey.dat) + survey.dat exists    │
│  Output: DDI metadata + data profiling statistics                  │
│  Note:   Best case - rich metadata AND statistics                  │
│                                                                     │
│  SCENARIO C: Syntax file → data file (found, NOT processable)      │
│  ─────────────────────────────────────────────────────────────     │
│  Input:  survey.sps → survey.dat (binary/unsupported format)       │
│  Output: DDI metadata only + warning                               │
│  Note:   Can't profile binary .dat files                           │
│                                                                     │
│  SCENARIO D: Syntax file → data file (NOT found)                   │
│  ─────────────────────────────────────────────────────             │
│  Input:  survey.sps (references survey.dat) but file missing       │
│  Output: DDI metadata only + warning                               │
│  Note:   Common when syntax uploaded without data                  │
│                                                                     │
│  SCENARIO E: Data file not referenced by any syntax                │
│  ───────────────────────────────────────────────────               │
│  Input:  results.csv (no syntax file points to it)                 │
│  Output: Inferred metadata + data profiling statistics             │
│  Note:   Standard CSV processing, no labels/categories             │
│                                                                     │
│  SCENARIO F: Data file referenced by syntax (already processed)    │
│  ───────────────────────────────────────────────────────────────   │
│  Input:  survey.dat (already processed with survey.sps)            │
│  Output: SKIP - already included in Scenario B output              │
│  Note:   Prevents duplicate entries in DDI-CDI                     │
│                                                                     │
│  SCENARIO G: Syntax file selected, data file NOT selected          │
│  ───────────────────────────────────────────────────────           │
│  Input:  User selected survey.sps but not survey.dat               │
│  Action: Auto-include survey.dat if it exists in dataset           │
│  Note:   Best effort - include referenced files automatically      │
│                                                                     │
└─────────────────────────────────────────────────────────────────────┘
```

### Deduplication Logic

The key challenge is avoiding duplicates when:
- User selects both `survey.sps` AND `survey.dat`
- Multiple syntax files reference the same data file
- Same filename exists in different folders

**Algorithm:**

```python
def process_files(selected_files, all_dataset_files):
    # Phase 1: Analyze syntax files
    syntax_analysis = {}
    referenced_data_files = set()
    
    for f in selected_files:
        if is_syntax_file(f):
            analysis = analyze_syntax_file(f)
            syntax_analysis[f.path] = analysis
            
            if analysis.references_file:
                # Try to find the referenced file in dataset
                ref_path = resolve_reference(f, analysis.references_file, all_dataset_files)
                if ref_path:
                    referenced_data_files.add(ref_path)
                    analysis.resolved_data_path = ref_path
    
    # Phase 2: Process files
    processed = set()
    results = []
    
    # First: Process syntax files (with their data files if available)
    for syntax_path, analysis in syntax_analysis.items():
        ddi_metadata = analysis.ddi_xml
        data_stats = None
        
        if analysis.resolved_data_path and is_processable(analysis.resolved_data_path):
            data_stats = profile_data_file(analysis.resolved_data_path)
            processed.add(analysis.resolved_data_path)
        
        results.append(combine_metadata(syntax_path, ddi_metadata, data_stats))
        processed.add(syntax_path)
    
    # Second: Process orphan data files (not referenced by any syntax)
    for f in selected_files:
        if is_data_file(f) and f.path not in processed:
            if f.path not in referenced_data_files:
                # Standalone data file
                results.append(profile_data_file(f.path))
            # else: skip - will be/was processed with its syntax file
    
    return results
```

---

## Implementation Details

### Parsing Data File References

Regular expressions for extracting data file references from syntax:

```python
import re
from pathlib import Path
from typing import Optional, NamedTuple

class SyntaxAnalysis(NamedTuple):
    has_inline_data: bool
    referenced_file: Optional[str]
    output_file: Optional[str]  # SAVE OUTFILE, etc. - not input

def parse_spss_syntax(content: str) -> SyntaxAnalysis:
    """Parse SPSS .sps file for data references."""
    content_upper = content.upper()
    
    # Check for inline data
    has_inline = bool(re.search(r'BEGIN\s+DATA', content_upper))
    
    # Find FILE= references (input files)
    # Matches: FILE='name', FILE="name", FILE=name
    file_match = re.search(
        r'\bFILE\s*=\s*[\'"]?([^\s\'"]+)[\'"]?',
        content,
        re.IGNORECASE
    )
    
    # Also check INFILE (alternative)
    if not file_match:
        file_match = re.search(
            r'\bINFILE\s*=\s*[\'"]?([^\s\'"]+)[\'"]?',
            content,
            re.IGNORECASE
        )
    
    referenced = file_match.group(1) if file_match else None
    
    # Find output files (to exclude from input detection)
    outfile_match = re.search(
        r'\bOUTFILE\s*=\s*[\'"]?([^\s\'"]+)[\'"]?',
        content,
        re.IGNORECASE
    )
    output = outfile_match.group(1) if outfile_match else None
    
    return SyntaxAnalysis(has_inline, referenced, output)


def parse_sas_syntax(content: str) -> SyntaxAnalysis:
    """Parse SAS .sas file for data references."""
    content_lower = content.lower()
    
    # Check for inline data (datalines or cards)
    has_inline = bool(re.search(r'\b(datalines|cards)\s*;', content_lower))
    
    # Find infile references
    # Matches: infile 'name', infile "name", infile name
    file_match = re.search(
        r'\binfile\s+[\'"]?([^\s\'";\)]+)[\'"]?',
        content,
        re.IGNORECASE
    )
    
    referenced = file_match.group(1) if file_match else None
    
    return SyntaxAnalysis(has_inline, referenced, None)


def parse_stata_syntax(content: str) -> SyntaxAnalysis:
    """Parse Stata .dct or .do file for data references."""
    content_lower = content.lower()
    
    # Check for inline data (input ... end block)
    has_inline = bool(re.search(r'\binput\b.*\bend\b', content_lower, re.DOTALL))
    
    # Find using references
    # Matches: using filename, using "filename", using 'filename'
    file_match = re.search(
        r'\busing\s+[\'"]?([^\s\'"}\)]+)[\'"]?',
        content,
        re.IGNORECASE
    )
    
    referenced = file_match.group(1) if file_match else None
    
    return SyntaxAnalysis(has_inline, referenced, None)


def analyze_syntax_file(file_path: Path) -> SyntaxAnalysis:
    """Analyze any supported syntax file."""
    content = file_path.read_text(errors='replace')
    ext = file_path.suffix.lower()
    
    if ext == '.sps':
        return parse_spss_syntax(content)
    elif ext == '.sas':
        return parse_sas_syntax(content)
    elif ext in ('.dct', '.do'):
        return parse_stata_syntax(content)
    else:
        return SyntaxAnalysis(False, None, None)
```

### Detecting Inline Data

Inline data markers by format:

| Format | Start Marker | End Marker |
|--------|-------------|------------|
| SPSS | `BEGIN DATA` | `END DATA.` |
| SAS | `datalines;` or `cards;` | `;` (on own line) |
| Stata | `input varlist` | `end` |

### File Matching Algorithm

When a syntax file references `survey.dat`, we need to find it in the dataset:

```python
def resolve_reference(syntax_file: Path, referenced_name: str, 
                      all_files: List[Path]) -> Optional[Path]:
    """
    Find the data file referenced by a syntax file.
    
    Search order:
    1. Same directory as syntax file
    2. Any directory with matching filename
    3. Fuzzy match (case-insensitive, with/without extension)
    """
    ref_name = Path(referenced_name).name  # Strip any path components
    syntax_dir = syntax_file.parent
    
    # 1. Check same directory first
    same_dir_match = syntax_dir / ref_name
    if same_dir_match in all_files:
        return same_dir_match
    
    # 2. Check all files for exact name match
    for f in all_files:
        if f.name == ref_name:
            return f
    
    # 3. Case-insensitive match
    ref_lower = ref_name.lower()
    for f in all_files:
        if f.name.lower() == ref_lower:
            return f
    
    # 4. Match without extension (e.g., 'survey' matches 'survey.dat', 'survey.csv')
    ref_stem = Path(ref_name).stem.lower()
    for f in all_files:
        if f.stem.lower() == ref_stem:
            return f
    
    return None  # Not found
```

---

## Expected DDI-CDI Output Structure

### Overall Structure

The generated JSON-LD must follow DDI-CDI 1.0 specification. Here's the expected structure:

```json
{
  "@context": "https://ddi-cdi.github.io/m2t-ng/DDI-CDI_1-0/encoding/json-ld/ddi-cdi.jsonld",
  "@graph": [
    { "@type": "WideDataSet", ... },
    { "@type": "WideDataStructure", ... },
    { "@type": "LogicalRecord", ... },
    { "@type": "InstanceVariable", ... },
    { "@type": "RepresentedVariable", ... },
    { "@type": "SubstantiveValueDomain", ... },
    { "@type": "IdentifierComponent|MeasureComponent|DimensionComponent|AttributeComponent", ... },
    { "@type": "PhysicalDataSet", ... },
    { "@type": "PhysicalSegmentLayout", ... },
    { "@type": "prov:Activity", ... }
  ]
}
```

### WideDataSet

The root container representing the dataset:

```json
{
  "@id": "#dataset",
  "@type": "WideDataSet",
  "cdi:WideDataSet_has_WideDataStructure": [
    { "@id": "#structure_file1" },
    { "@id": "#structure_file2" }
  ],
  "dcterms:title": "Dataset Title from Dataverse",
  "dcterms:description": "Dataset description if available",
  "dcterms:identifier": "doi:10.5072/FK2/ABCDEF",
  "dcterms:creator": ["Author Name"],
  "dcterms:issued": "2025-01-15"
}
```

### WideDataStructure

One per processed file, defines the structure:

```json
{
  "@id": "#structure_survey",
  "@type": "WideDataStructure",
  "cdi:WideDataStructure_has_LogicalRecord": { "@id": "#record_survey" },
  "cdi:WideDataStructure_has_IdentifierComponent": [
    { "@id": "#comp_id" }
  ],
  "cdi:WideDataStructure_has_MeasureComponent": [
    { "@id": "#comp_age" },
    { "@id": "#comp_income" }
  ],
  "cdi:WideDataStructure_has_DimensionComponent": [
    { "@id": "#comp_gender" }
  ],
  "cdi:WideDataStructure_has_AttributeComponent": [],
  "dcterms:title": "survey.csv"
}
```

### Variables

Each column/variable in the data:

```json
{
  "@id": "#survey_id",
  "@type": "InstanceVariable",
  "cdi:name": "id",
  "cdi:displayLabel": "Respondent ID",
  "cdi:InstanceVariable_representedVariable": { "@id": "#survey_id_repr" }
},
{
  "@id": "#survey_id_repr",
  "@type": "RepresentedVariable",
  "cdi:RepresentedVariable_takesSubstantiveValuesFrom": { "@id": "#survey_id_domain" }
},
{
  "@id": "#survey_id_domain",
  "@type": "SubstantiveValueDomain",
  "cdi:dataType": { "@id": "http://www.w3.org/2001/XMLSchema#integer" }
}
```

### CodeLists for Categorical Variables

When DDI metadata includes value labels (categories), a `CodeList` is generated:

```json
{
  "@id": "#survey_gender_CodeList",
  "@type": "CodeList",
  "name": "Gender codes",
  "has_Code": ["#survey_gender_Code_1", "#survey_gender_Code_2"]
},
{
  "@id": "#survey_gender_Code_1",
  "@type": "Code",
  "identifier": "1",
  "name": "Male"
},
{
  "@id": "#survey_gender_Code_2",
  "@type": "Code",
  "identifier": "2",
  "name": "Female"
}
```

The `SubstantiveValueDomain` links to the CodeList:

```json
{
  "@id": "#survey_gender_domain",
  "@type": "SubstantiveValueDomain",
  "recommendedDataType": "...",
  "takesValuesFrom": "#survey_gender_CodeList"
}
```

### Summary Statistics

When DDI metadata includes summary statistics (mean, min, max, etc.), `CategoryStatistic` nodes are generated:

```json
{
  "@id": "#survey_age_stat_mean",
  "@type": "CategoryStatistic",
  "appliesTo": "#survey_age",
  "typeOfCategoryStatistic": "mean",
  "statistic": {
    "@type": "Statistic",
    "content": 42.5
  }
},
{
  "@id": "#survey_age_stat_min",
  "@type": "CategoryStatistic",
  "appliesTo": "#survey_age",
  "typeOfCategoryStatistic": "min",
  "statistic": {
    "@type": "Statistic",
    "content": 18
  }
}
```

**Variable ID Format (IMPORTANT):**

To ensure uniqueness across files with same column names, variable IDs MUST include a file prefix:

```
#<file_stem>_<variable_name>
```

Examples:
- `#survey_id` (not `#id`)
- `#survey_age` (not `#age`)
- `#results_id` (different file, different ID)

### Provenance

Track how metadata was generated:

```json
{
  "@id": "#generation_activity",
  "@type": "prov:Activity",
  "prov:startedAtTime": "2025-01-16T10:30:00Z",
  "prov:wasAssociatedWith": {
    "@type": "prov:SoftwareAgent",
    "rdfs:label": "cdi_generator_jsonld.py"
  },
  "prov:generated": { "@id": "#dataset" }
}
```

---

## SHACL Validation

The generated DDI-CDI is validated against official SHACL shapes from:
`https://ddi-cdi.github.io/m2t-ng/DDI-CDI_1-0/encoding/shacl/ddi-cdi.shacl.ttl`

### Required Properties

| Type | Required Properties |
|------|---------------------|
| `WideDataSet` | Must have at least one `WideDataStructure` |
| `WideDataStructure` | Must have `LogicalRecord` |
| `InstanceVariable` | Must have `name` |
| `RepresentedVariable` | Should link to `SubstantiveValueDomain` |

### Cardinality Constraints

- `WideDataSet_has_WideDataStructure`: 1..* (at least one structure)
- `InstanceVariable_representedVariable`: 0..1 (optional but recommended)
- `name`: exactly 1 per variable

### Common Validation Issues

1. **Duplicate IDs**: Variables from different files with same name get same ID
   - **Fix**: Prefix IDs with file stem

2. **Missing LogicalRecord**: WideDataStructure without record
   - **Fix**: Always create LogicalRecord

3. **Invalid dataType URI**: Using string instead of URI for XSD types
   - **Fix**: Use `{ "@id": "xsd:integer" }` not `"xsd:integer"`

4. **Orphan nodes**: Objects not connected to graph
   - **Fix**: Ensure all objects linked via relationships

---

## Test Cases

> **Note**: Existing test syntax files in `image/testdata/` all contain **inline data** (no external file references). Additional test files with external references need to be created.

### Existing Test Data

The repository includes test syntax files at [image/testdata/](image/testdata/):

| File | Type | Data Source | Notes |
|------|------|-------------|-------|
| `simple_data.sps` | SPSS | Inline (`BEGIN DATA...END DATA`) | Saves to `simple_data.sav` |
| `simple_data.sas` | SAS | Inline (`datalines;`) | Basic SAS data step |
| `simple_data.dct` | Stata | Inline (`input...end`) | Dictionary format |

**Key observation**: These files do NOT reference external data files—they all use inline data definitions. This matches Scenario A below. To test Scenarios B-G, we need additional test files that reference external `.dat`/`.csv` files.

### Test Case 1: Syntax with Inline Data

**Input:**
```
simple_data.sps (contains BEGIN DATA...END DATA)
```

**Expected Output:**
- WideDataStructure for `simple_data.sps`
- Variables with labels from VARIABLE LABELS
- Value labels from VALUE LABELS
- No statistics (no data profiling)
- Warning: "Using inline data definitions only"

### Test Case 2: Syntax References External File (Found)

**Input:**
```
survey.sps (contains FILE='survey.dat')
survey.dat (CSV format)
```

**Expected Output:**
- WideDataStructure for `survey`
- Variables with labels from .sps
- Statistics from profiling survey.dat
- No duplicate entry for survey.dat

### Test Case 3: Syntax References External File (Not Found)

**Input:**
```
survey.sps (contains FILE='missing.dat')
```

**Expected Output:**
- WideDataStructure for `survey.sps`
- Variables with labels only (no stats)
- Warning: "Referenced file 'missing.dat' not found"

### Test Case 4: Multiple Syntax Files, Same Data

**Input:**
```
spss_version.sps → data.dat
sas_version.sas → data.dat
data.dat
```

**Expected Output:**
- Process data.dat ONCE with first syntax file
- Skip duplicate processing
- Warning about multiple syntax files for same data

### Test Case 5: Orphan Data File

**Input:**
```
results.csv (no syntax references it)
```

**Expected Output:**
- WideDataStructure for `results.csv`
- Variables with inferred labels (column names)
- Statistics from profiling

### Test Case 6: Same Filename Different Folders

**Input:**
```
folder_a/data.csv
folder_b/data.csv
folder_a/data.sps → data.csv
```

**Expected Output:**
- `folder_a/data.csv` processed with syntax (rich metadata)
- `folder_b/data.csv` processed standalone (inferred metadata)
- Unique IDs: `#folder_a_data_col1`, `#folder_b_data_col1`

### Test Case 7: User Selects Syntax Only

**Input:**
```
User selects: survey.sps
Dataset contains: survey.sps, survey.dat, other.csv
```

**Expected Behavior:**
- Auto-include `survey.dat` (referenced by selected syntax)
- Process together
- Do NOT include `other.csv`

---

## Future Enhancements

> See [Current Implementation Limitations](#current-implementation-limitations) for the immediate fixes needed.

### Short-term (Required for Basic Functionality)

1. **Add `.do` to backend filter**: Single-line fix in `image/app/common/ddi_cdi.go`

2. **Handle syntax files in Python generator**: Check file extension before calling `stream_profile_csv()`; for syntax files, only run xconvert and extract DDI metadata

3. **Create test files with external references**: Add `survey.sps` + `survey.dat` pair to `image/testdata/`

### Medium-term (Full Design Implementation)

4. **Rich metadata output** ✅ IMPLEMENTED: Include categories and statistics in JSON-LD output:
   - Generate `CodeList` + `Code` nodes for categorical variables with value labels
   - Generate `CategoryStatistic` nodes for summary statistics (mean, min, max, etc.)
   - Link variables to CodeLists via `takesValuesFrom` on `SubstantiveValueDomain`

5. **Dataset-level rich metadata** ✅ IMPLEMENTED: Extract and output comprehensive dataset metadata:
   - `CatalogDetails` node with title, subtitle, summary/description
   - `AgentInRole` nodes for creators (authors), contributors, publisher
   - Author identifiers (ORCID) and affiliations preserved
   - `CombinedDate` for publication date
   - `AccessInformation` with license name and URI
   - `ProvenanceInformation` with funding agencies and grant numbers
   - `relatedResource` for related publications
   - Language and subject classification
   - Link from `WideDataSet` via `catalogDetails` property

6. **Parse syntax files for data references**: Implement `parse_spss_syntax()`, `parse_sas_syntax()`, `parse_stata_syntax()` functions

7. **Build file relationship map**: Before processing, analyze all syntax files to find references

8. **Auto-include referenced data files**: If user selects `survey.sps` but not `survey.dat`, include it automatically

9. **Deduplication logic**: Skip data files that were already processed via their syntax file

### Long-term (Nice to Have)

10. **Parse xconvert DDI output for data file reference**: The DDI XML from xconvert may include file information

11. **Support for binary .dat files**: Attempt to detect format and parse if possible

12. **Multiple syntax files per data file**: Merge metadata from multiple syntax files

13. **Syntax file validation**: Warn about syntax errors before processing

14. **Interactive file pairing**: UI to manually pair syntax with data files

15. **Preserve syntax file in output**: Store syntax as provenance for reproducibility

---

## Dataset-Level Metadata Mapping

The following table shows how Dataverse citation metadata maps to DDI-CDI `CatalogDetails`:

| Dataverse Field | DDI-CDI Property | DDI-CDI Type |
|-----------------|------------------|--------------|
| `title` | `CatalogDetails.title` | `InternationalString` |
| `subtitle` | `CatalogDetails.subTitle` | `InternationalString` |
| `alternativeTitle` | `CatalogDetails.alternativeTitle` | `InternationalString` |
| `dsDescription` | `CatalogDetails.summary` | `InternationalString` |
| `author` | `CatalogDetails.creator` | `AgentInRole` → `Individual` |
| `authorAffiliation` | `Individual.individualName.affiliation` | via `BibliographicName` |
| `authorIdentifier` (ORCID) | `Individual.identifier.uri` | `Identifier` |
| `contributor` | `CatalogDetails.contributor` | `AgentInRole` → `Individual` |
| `distributorName` / `publisher` | `CatalogDetails.publisher` | `AgentInRole` → `Organization` |
| `distributionDate` / `dateOfDeposit` | `CatalogDetails.date` | `CombinedDate` |
| `language` | `CatalogDetails.languageOfObject` | `xsd:language` |
| `license.name` | `AccessInformation.license.name` | `InternationalString` |
| `license.uri` | `LicenseInformation.uri` | `xsd:anyURI` |
| `termsOfUse` | `AccessInformation.rights` | `InternationalString` |
| `grantNumberAgency` | `FundingInformation.funder` | via `ProvenanceInformation` |
| `grantNumberValue` | `FundingInformation.grantIdentifier` | string |
| `publication` (citation) | `CatalogDetails.relatedResource` | `Reference` with description |
| `publication` (URL) | `CatalogDetails.relatedResource` | `Reference` with uri |
| `subject` | `CatalogDetails.typeOfResource` | `ControlledVocabularyEntry` |

### Example CatalogDetails Output

```json
{
  "@id": "#catalogDetails",
  "@type": "CatalogDetails",
  "title": {
    "@type": "InternationalString",
    "languageSpecificString": {
      "@type": "LanguageString",
      "content": "Survey of Consumer Finances 2022"
    }
  },
  "summary": {
    "@type": "InternationalString",
    "languageSpecificString": {
      "@type": "LanguageString",
      "content": "A comprehensive survey of household finances in the United States."
    }
  },
  "creator": [
    {
      "@type": "AgentInRole",
      "agent": {
        "@id": "#author_0",
        "@type": "Individual",
        "individualName": {
          "@type": "BibliographicName",
          "languageSpecificString": {
            "@type": "LanguageString",
            "content": "Smith, John"
          },
          "affiliation": "Federal Reserve Board"
        },
        "identifier": {
          "@type": "Identifier",
          "uri": "https://orcid.org/0000-0001-2345-6789"
        }
      }
    }
  ],
  "publisher": {
    "@type": "AgentInRole",
    "agent": {
      "@id": "#publisher",
      "@type": "Organization",
      "organizationName": {
        "@type": "OrganizationName",
        "name": {
          "@type": "LanguageString",
          "content": "KU Leuven RDR"
        }
      }
    }
  },
  "date": {
    "@type": "CombinedDate",
    "isoDate": "2023-06-15"
  },
  "access": {
    "@type": "AccessInformation",
    "license": {
      "@type": "LicenseInformation",
      "name": {
        "@type": "InternationalString",
        "languageSpecificString": {
          "@type": "LanguageString",
          "content": "CC BY 4.0"
        }
      },
      "uri": "https://creativecommons.org/licenses/by/4.0/"
    }
  },
  "identifier": {
    "@type": "InternationalIdentifier",
    "identifierContent": "doi:10.70122/FK2/EXAMPLE",
    "isURI": true
  }
}
```

---

## References

- [UC Berkeley xconvert tool](https://sda.berkeley.edu/ddi/tools/xconvert.html)
- [DDI-CDI 1.0 Specification](https://ddialliance.org/Specification/DDI-CDI/)
- [DDI-CDI JSON-LD Context](https://ddi-cdi.github.io/m2t-ng/DDI-CDI_1-0/encoding/json-ld/ddi-cdi.jsonld)
- [DDI-CDI SHACL Shapes](https://ddi-cdi.github.io/m2t-ng/DDI-CDI_1-0/encoding/shacl/ddi-cdi.shacl.ttl)
- [SPSS Syntax Reference](https://www.ibm.com/docs/en/spss-statistics)
- [SAS Language Reference](https://documentation.sas.com/)
- [Stata Dictionary Documentation](https://www.stata.com/manuals/dinfile.pdf)
