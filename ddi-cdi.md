# DDI-CDI Metadata Generation

## Table of Contents

### 📚 Getting Started
- [Overview](#overview)
- [Quick Start](#dataverse-external-tool-quick-start)
- [What is DDI-CDI?](#what-is-ddi-cdi)
- [What Does This Feature Do?](#what-does-this-feature-do)

### 🔧 How It Works
- [The Processing Pipeline](#how-it-works-the-processing-pipeline)
  - [Step 1: Job Submission](#step-1-job-submission)
  - [Step 2: Data Access and Preparation](#step-2-data-access-and-preparation)
  - [Step 3: File Analysis](#step-3-file-analysis)
  - [Step 4: Metadata Enrichment](#step-4-metadata-enrichment)
  - [Step 5: CDI Generation](#step-5-cdi-generation)
  - [Step 6: Presentation](#step-6-presentation)

### 📁 File Formats
- [Supported File Formats](#supported-file-formats)
  - [CSV and TSV Files](#csv-and-tsv-files)
  - [Statistical Data Files](#statistical-data-files)
  - [File Filtering](#file-filtering)

### 👤 User Guide
- [Using the Feature](#using-the-feature)
  - [Accessing the Generator](#accessing-the-ddi-cdi-generator)
  - [Step-by-Step Workflow](#step-by-step-workflow)
  - [Understanding the Results](#understanding-the-results)
  - [The Interactive Viewer (CDI Viewer)](#the-interactive-viewer-cdi-viewer)
  - [Console Output](#console-output)
  - [Caching and Performance](#caching-and-performance)
  - [Common Scenarios and Tips](#common-scenarios-and-tips)

### 🛠️ Technical Reference
- [Technical Details](#technical-details)
  - [Architecture](#architecture)
  - [The cdi_generator_jsonld.py Script](#the-cdi_generator_jsonldpy-script)
  - [Testing](#testing)
- [Performance and Scalability](#performance-and-scalability)
- [Limitations and Future Enhancements](#limitations-and-future-enhancements)
- [Metadata Standards and Compliance](#metadata-standards-and-compliance)

### 📖 Additional Resources
- [Getting Help and Contributing](#getting-help-and-contributing)
- [Credits and Acknowledgments](#credits-and-acknowledgments)
- [Appendix: cdi_generator_jsonld.py Deep Dive](#appendix-cdi_generator_jsonldpy-deep-dive)

---

## Overview

This document describes the DDI-CDI (Data Documentation Initiative - Cross-Domain Integration) metadata generation feature for Dataverse datasets. This feature automatically creates rich, standardized metadata descriptions of your tabular data files, making your research data more discoverable, reusable, and interoperable across different systems and domains.

**Note**: This feature complements Dataverse's native [tabular data ingest](https://guides.dataverse.org/en/latest/user/tabulardataingest/index.html) by leveraging the DDI metadata it generates and extending it to the DDI-CDI format. For files already ingested by Dataverse, the feature uses the existing metadata; for other formats or non-ingested files, it provides comprehensive analysis.

## Dataverse External Tool Quick Start

Run `make up` from the repository root to start the full demo stack (Docker with the Compose plugin and GNU Make are required). Once the containers are ready, sign in via Keycloak using the default `admin / admin` credentials to access Dataverse. After the initial setup, Dataverse is empty: create a dataset and upload a few supported files (CSV/TSV/TAB or statistical syntax such as `.sps`, `.sas`, `.dct`) so you can try the tool. Supporting services are exposed on loopback-friendly hostnames (`keycloak.localhost`, `localstack.localhost`, `minio.localhost`), so no `/etc/hosts` adjustments are needed.

The `make up` flow verifies whether the Dataverse container has already been bootstrapped. On the first run it executes `dataverse/setup.sh`, which registers all shipped external tools—including **Generate DDI-CDI** - via `conf/dataverse/external-tools/03-rdm-integration-ddi-cdi.json`. As a result, the dataset page already exposes the DDI-CDI button and launches the frontend with the dataset PID (and API token when available) pre-populated.

If you ever need to re-register the tool manually (for example after deleting it), run the following inside the Dataverse container:

```bash
. /scripts/setup-tools
superAdmin datafile 'admin/externalTools' '/conf/external-tools/03-rdm-integration-ddi-cdi.json'
```

Refer back to [README.md](README.md) for broader environment setup details, credentials, and troubleshooting tips beyond this quick start. Once your dataset exists and contains supported files, the dataset page will show the "Generate DDI-CDI" external tool button, which launches the frontend pre-populated with the dataset PID.

### What is DDI-CDI?

DDI-CDI is an international standard for describing research data. It provides a common vocabulary and structure for documenting datasets, making it easier to:

- **Share data** across different research platforms and repositories
- **Preserve data** with complete documentation for long-term archiving
- **Discover data** through standardized metadata that search engines can understand
- **Integrate data** from multiple sources in cross-domain research projects
- **Validate data** against documented schemas and quality rules

### What Does This Feature Do?

When you have tabular data files (CSV, TSV, or statistical format files like SPSS, SAS, or Stata) in your Dataverse dataset, this feature:

1. **Analyzes your data files** automatically, examining the structure and content
2. **Infers metadata** about each column (variable type, role, statistics)
3. **Enriches descriptions** using any existing DDI metadata from Dataverse
4. **Generates standardized metadata** in JSON-LD format following DDI-CDI 1.0 specifications
5. **Presents results** through an interactive viewer interface (cdi-viewer) for review and editing

All of this happens in the background, requiring minimal effort from you as a researcher.

---

## How It Works: The Processing Pipeline

**Navigation**: [↑ Table of Contents](#table-of-contents) | [→ Supported File Formats](#supported-file-formats) | [→ User Guide](#using-the-feature) | [→ Technical Details](#technical-details)

The DDI-CDI generation follows a multi-step pipeline that transforms your data files into rich metadata descriptions:

### Step 1: Job Submission

When you request DDI-CDI generation for your dataset:

- You select which files in your dataset to process
- The system creates a background job to handle the processing
- You can optionally choose to receive email notifications on success (via checkbox)
- You can continue working or close your browser - the job runs independently
- Email notifications are sent containing:
  - **Always sent on failure**: Error details and a link to retry
  - **Sent on success (if opted in)**: Success confirmation with a direct link to the DDI-CDI page
  - The dataset persistent ID for reference
  - A direct link to view/edit the generated metadata

### Step 2: Data Access and Preparation

The system securely accesses your data:

- Authenticates using your Dataverse credentials
- Mounts the dataset storage (read-only for safety)
- Creates a temporary workspace for processing
- Workspace root defaults to `/dsdata/<job-key>` inside the container, with subfolders:
  - `/dsdata/<job-key>/s3` for the read-only `s3fs` mount of the dataset bucket
  - `/dsdata/<job-key>/linked` containing symlinks for each Dataverse file (mirroring IDs)
  - `/dsdata/<job-key>/work` used for per-file scratch files (DDI fragments, CSV temp output)
- The base path is configurable via `options.workspaceRoot` in `backend_config.json`; all helpers resolve paths relative to this location to support non-root execution.
- Retrieves existing metadata from Dataverse (dataset information and any DDI documentation)

### Step 3: File Analysis

For each tabular data file, the system:

- **Detects the file format** (CSV, TSV, or statistical formats)
- **Checks for Dataverse ingest**:
  - If the file was ingested by Dataverse (SPSS `.sav`, Stata `.dta`, R, Excel, or CSV)
  - Uses the tab-separated version created by Dataverse ([ingest process details](https://guides.dataverse.org/en/latest/user/tabulardataingest/ingestprocess.html))
  - Retrieves DDI metadata from the `/metadata/ddi` API endpoint
- **For non-ingested statistical syntax files** (SPSS `.sps`, SAS `.sas`, Stata `.dct`/`.do`):
  - Uses the Berkeley xconvert tool to extract structure and metadata
  - Converts syntax definitions to DDI XML format
  - **Note**: Currently requires a matching data file (same stem name) to function; see [SYNTAX_FILE_PROCESSING.md](SYNTAX_FILE_PROCESSING.md) for planned standalone syntax file support
- **For all tabular files**:
  - Streams through the data row-by-row (memory-efficient for large files)
  - Infers data types for each column (integer, decimal, boolean, date/time, text)
  - Determines variable roles (identifier, measure, dimension, attribute)
  - Calculates approximate statistics using probabilistic data structures
  - Packages the gathered context into a manifest entry that describes how the generator should read the file in Step 5

### Step 4: Metadata Enrichment

The system combines multiple metadata sources:

- **Inferred metadata** from data profiling (types, roles, statistics)
- **Dataverse metadata** (dataset title, DOI, file identifiers)
- **DDI metadata from Dataverse ingest** (for ingested files: SPSS `.sav`, Stata `.dta`, R, Excel, CSV)
  - Variable labels and descriptions
  - Category definitions with value labels
  - Summary statistics (mean, min, max, standard deviation)
  - Retrieved via the `/metadata/ddi` API endpoint
- **DDI metadata from xconvert** (for syntax files: SPSS `.sps`, SAS `.sas`, Stata `.dct`/`.do`)
  - Extracted from syntax definitions
  - Converted to DDI XML format

This multi-source approach ensures the richest possible documentation, leveraging both Dataverse's native capabilities and specialized tools.

### Step 5: CDI Generation

The Go backend now assembles a manifest (JSON) that captures dataset context alongside every selected file (physical paths, discovered metadata, ingest/xconvert fragments, and processing options). It then invokes [`cdi_generator_jsonld.py`](image/cdi_generator_jsonld.py) once via `--manifest <manifest-path>`, letting the Python layer iterate through each entry, emit DDI-CDI output, and surface any warnings back to the job log.

Within this manifest-driven run, the generator:

- Constructs a DDI-CDI 1.0 compliant JSON-LD document
- Uses the official DDI-CDI context: `https://ddi-cdi.github.io/m2t-ng/DDI-CDI_1-0/encoding/json-ld/ddi-cdi.jsonld`
- Describes the dataset structure using WideDataSet, WideDataStructure, and related types
- Documents each variable with InstanceVariable, RepresentedVariable, and component types
- Records provenance information (processing timestamp, tools used)
- Writes optional per-run summary JSON when `--summary` is enabled (used by the backend to capture profiling metadata)
- Outputs the result in JSON-LD format with MIME type `application/ld+json`

### Step 6: Presentation

The generated metadata is:

- Cached for quick retrieval
- Displayed as a **JSON-LD preview** in the interface
- Available to open in the **cdi-viewer** (new browser window) for editing and saving to Dataverse
- Validated against official DDI-CDI SHACL shapes from ddi-cdi.github.io
- Made available for download as a `.jsonld` file

---

## Supported File Formats

**Navigation**: [↑ Table of Contents](#table-of-contents) | [← Processing Pipeline](#how-it-works-the-processing-pipeline) | [→ User Guide](#using-the-feature) | [→ Technical Details](#technical-details)

### CSV and TSV Files

Standard comma-separated or tab-separated value files are fully supported:

- Automatic delimiter detection
- Character encoding detection (handles UTF-8, ASCII, ISO-8859, etc.)
- Header row detection
- Handles missing values and various formatting conventions
- Memory-efficient streaming for files of any size

### Statistical Data Files

The feature supports statistical data files through two complementary approaches:

#### Dataverse Native Ingest

Dataverse has built-in support for ingesting several statistical formats (see [Dataverse Tabular Data Ingest Guide](https://guides.dataverse.org/en/latest/user/tabulardataingest/index.html)). When you upload these files, Dataverse automatically:

- Converts the raw data to plain-text tab-separated format (`.tab` files)
- Extracts variable metadata (labels, types, categories) to the database
- Generates DDI Codebook 2.5 metadata accessible via the `/metadata/ddi` API
- Preserves the original uploaded file with an `.orig` extension

**Supported by Dataverse ingest** ([full list](https://guides.dataverse.org/en/latest/user/tabulardataingest/supportedformats.html)):

| File Format | Versions Supported |
|-------------|-------------------|
| **SPSS** (`.por`, `.sav`) | Versions 7-22 |
| **Stata** (`.dta`) | Versions 4-17 |
| **R** (`.RData`) | Up to version 3 |
| **Excel** (`.xlsx`) | XLSX only (XLS not supported) |
| **CSV** | Limited support |

**File size limits**: Administrators can configure the maximum size for tabular ingest using the `TabularIngestSizeLimit` setting (see [Dataverse Configuration Guide](https://guides.dataverse.org/en/latest/installation/config.html#tabularingestsizelimit)). The default is typically 2GB.

**How files are stored** ([see Dataverse Ingest Process](https://guides.dataverse.org/en/latest/user/tabulardataingest/ingestprocess.html)):
- The `.tab` file (tab-delimited archival format) is stored in the configured file storage location (default: `/usr/local/payara6/glassfish/domains/domain1/files`)
- The original file is preserved with `.orig` extension in the same storage location
- Variable metadata is stored in the relational database
- Both versions appear in the Dataverse UI and are accessible through the API

**Using ingested files with DDI-CDI**: When Dataverse has successfully ingested a file, our DDI-CDI feature:
- Automatically uses the `.tab` file generated by Dataverse (the archival format)
- Retrieves comprehensive DDI metadata via the `/metadata/ddi` API endpoint ([CSV/TSV documentation](https://guides.dataverse.org/en/latest/user/tabulardataingest/csv-tsv.html))
- Enriches the CDI output with variable labels, categories, and statistics from the ingest process
- The ingested `.tab` files appear in the file selection UI alongside other dataset files

#### xconvert Tool Integration

For files not ingested by Dataverse (syntax files, files exceeding size limits, or pre-ingest scenarios), the feature integrates with the **xconvert tool** from UC Berkeley's Survey Documentation and Analysis (SDA) project:

- **SPSS syntax files**: `.sps` (command syntax)
- **SAS syntax files**: `.sas` (data step definitions)
- **Stata dictionary files**: `.dct` (dictionary), `.do` (command files)

The xconvert tool extracts metadata from these syntax definition files and converts them to DDI XML format, which is then used to enrich the CDI output.

Credit: The xconvert tool is developed and maintained by the University of California, Berkeley's Survey Documentation and Analysis (SDA) program. More information: [https://sda.berkeley.edu/ddi/tools/xconvert.html](https://sda.berkeley.edu/ddi/tools/xconvert.html)

### File Filtering

The DDI-CDI feature automatically filters files by their extension to show only compatible formats. When you select a dataset, the system will:

- Query the backend for files matching supported extensions
- Display only files that can be processed for DDI-CDI generation
- Auto-select all compatible files by default (you can deselect any you don't want to process)

**Currently Supported Extensions**:

| Extension | Description | Processing Method |
|-----------|-------------|-------------------|
| `.csv` | Comma-separated values | Direct CSV analysis |
| `.tsv` | Tab-separated values | Direct TSV analysis |
| `.tab` | Dataverse tabular format | Uses Dataverse ingest metadata + direct analysis |
| `.sps` | SPSS syntax files | xconvert tool + DDI extraction |
| `.sas` | SAS data step definitions | xconvert tool + DDI extraction |
| `.dct` | Stata dictionary files | xconvert tool + DDI extraction |
| `.do` | Stata command files | xconvert tool + DDI extraction |

**Note**: The extension list is defined in the backend code ([image/app/common/ddi_cdi.go](image/app/common/ddi_cdi.go)) and files are filtered server-side. Files with other extensions will not appear in the file selection tree.

> ⚠️ **Current Limitation**: The backend filter does NOT yet include `.do` files—this is a known gap. Additionally, syntax files (`.sps`, `.sas`, `.dct`, `.do`) currently cannot be processed standalone; they require a matching data file with the same stem name (e.g., `survey.sps` needs `survey.csv`). See [SYNTAX_FILE_PROCESSING.md](SYNTAX_FILE_PROCESSING.md) for the planned improvements.

#### Adding Support for New Extensions

If you need to add support for additional file formats (e.g., `.xlsx`, `.json`, or other statistical formats), you can modify the supported extensions list in the backend code:

1. Open `image/app/core/ddi_cdi.go`
2. Locate the `GetDdiCdiCompatibleFiles` function
3. Find the `supported` map definition:
   ```go
   supported := map[string]bool{
       "csv": true,
       "tsv": true,
       "tab": true,
       "sps": true,
       "sas": true,
       "dct": true,
   }
   ```
4. Add your new extension to the map (e.g., `"xlsx": true,`)
5. Implement the necessary processing logic in the DDI-CDI generation pipeline to handle the new format
6. Rebuild and redeploy the application

The frontend will automatically recognize files with the new extension once the backend is updated. No frontend code changes are required - the UI dynamically adapts to show only the files returned by the backend filter.

---

## Using the Feature

**Navigation**: [↑ Table of Contents](#table-of-contents) | [← Supported File Formats](#supported-file-formats) | [← Processing Pipeline](#how-it-works-the-processing-pipeline) | [→ Technical Details](#technical-details)

### Accessing the DDI-CDI Generator

To use the DDI-CDI generation feature:

1. Navigate to the RDM Integration tool from your Dataverse instance
2. Access the DDI-CDI component from the navigation menu (Home button returns you to the main interface)
3. Enter your Dataverse API token if required (depends on configuration)
4. Select your target dataset from the dropdown or use the search feature

**Tip**: If you have a direct link with the dataset persistent ID (e.g., from an email notification), the dataset will be automatically selected when you arrive at the page.

### Step-by-Step Workflow

#### 1. Select Your Dataset

- Use the **"Select Dataset"** dropdown to choose your dataset
- Start typing to search for datasets (minimum 3 characters)
- Or paste a persistent ID directly if the field is editable

#### 2. Review and Select Files

Once a dataset is selected:

- The interface displays a **tree table** of all files in your dataset
- **Supported files** (CSV, TSV, and TAB formats) are **automatically selected** and highlighted
- Unsupported files appear in gray with no selection checkbox
- You can manually **deselect** files you don't want to process

**Info Box**: An information banner shows which file types are supported and confirms that supported files are auto-selected.

#### 3. Check for Previous Results

If DDI-CDI metadata was previously generated for this dataset:

- The system **automatically loads** the cached output when you select the dataset
- A success notification displays: "Loaded previously generated DDI-CDI metadata (timestamp)"
- The **cdi-viewer** shows the previously generated metadata for review and editing
- Use the **"Reload viewer"** button to reload from cache

#### 4. Generate DDI-CDI Metadata

To generate new metadata:

- Click the **"Generate DDI-CDI"** button
- A popup dialog appears with important information:
  - **"Generate DDI-CDI Metadata"** dialog
  - Informs you that the job runs **asynchronously**
  - Shows a **checkbox**: "Email me when the generation is completed"
    - If checked: You'll receive emails for both success and failure
    - If unchecked: You'll only receive an email if generation **fails**
  - **You can close the browser window** - processing continues in the background
- Check the email checkbox if you want success notifications (optional)
- Click **"OK"** to confirm and start the job, or **"Cancel"** to return

#### 5. Monitor Progress

After starting generation:

- A **console output area** shows processing status
- Initial message: "DDI-CDI generation started... You will receive an email when it completes. You can close this window."
- The system polls for results in the background
- **You can safely navigate away or close the browser**

#### 6. Review Generated Metadata

When generation completes (either by waiting or returning later):

- The generated DDI-CDI JSON-LD is displayed in a **read-only preview** on the right side
- The preview shows the raw JSON-LD content for quick verification
- Console output shows on the left (or can be hidden)
- A success notification confirms: "DDI-CDI generated successfully!"

#### 7. Download or Open in Viewer

After generation, you have two options:

**Option A: Download**
- Click the **"Download"** button to save the JSON-LD file locally
- File is named `ddi-cdi-[timestamp].jsonld`
- Useful for archiving or sharing outside of Dataverse

**Option B: Open in Viewer (Recommended)**
- Click the **"Open in Viewer"** button to open the **cdi-viewer** in a new browser window
- The system first adds your DDI-CDI file to the dataset with the correct MIME type
- The viewer opens and loads the file directly from Dataverse
- You can then edit and save changes back to the same file

#### 8. Edit and Save to Dataverse (via cdi-viewer)

When you click "Open in Viewer":

1. The generated DDI-CDI metadata is added as a file to your Dataverse dataset
2. The file receives the correct DDI-CDI MIME type automatically
3. The cdi-viewer opens with your file ID and Dataverse URL as parameters
4. The viewer loads the file directly from Dataverse using its API

In the cdi-viewer:

- **Review** the automatically generated metadata in a hierarchical tree view
- **Edit** any fields to add or correct information (click "Enable Editing" button)
- **Validate** changes against official DDI-CDI SHACL shapes
- Click **"Save to Dataverse"** to save your changes back to the file

**File Management**:
- The file is created with the correct DDI-CDI MIME type
- The file is added to your dataset automatically when you click "Open in Viewer"
- When you save in cdi-viewer, changes are saved back to the same file
- The file can be downloaded, versioned, and shared with your dataset

#### 9. Refresh or Start Over

At any time after generation:

- Click **"Reload viewer"** (if displayed) to reload the last cached output from Redis
- This refreshes the preview with the original generated metadata
- Useful if you want to start over with unmodified output

### Understanding the Results

The generated DDI-CDI metadata is presented in **JSON-LD format** (following DDI-CDI 1.0) and includes:

#### Dataset-Level Information
- **Dataset title** and persistent identifier (DOI)
- **Publisher** information (from Dataverse)
- **Description** if available
- **Provenance** metadata (when and how it was generated)

#### File-Level Information
For each processed file:
- **Physical location** URI in the Dataverse storage
- **File format** (CSV, TSV, TAB)
- **Original filename**
- **File identifier** from Dataverse

#### Variable-Level Information
For each column/variable in your tabular data:
- **Variable name** (column header)
- **Data type**: Inferred as integer, decimal, boolean, date/time, or string
- **Variable role**: Classified as identifier, measure, dimension, or attribute
- **Variable labels**: Descriptive labels from DDI metadata (if available)
- **Value labels and categories**: For categorical variables (from SPSS/Stata/SAS metadata)
- **Summary statistics**: 
  - Approximate distinct count (using HyperLogLog algorithm)
  - Mean, minimum, maximum values (where applicable)
  - Missing value counts

#### Metadata Sources

The system enriches the output by combining:
- **Inferred metadata** from data profiling and analysis
- **Dataverse metadata** from the dataset record
- **DDI Codebook metadata** from ingested files (via `/metadata/ddi` API)
- **xconvert metadata** from statistical syntax files

### The Interactive Viewer (CDI Viewer)

The **cdi-viewer** application (developed at KU Leuven, [https://github.com/libis/cdi-viewer](https://github.com/libis/cdi-viewer)) opens in a **new browser window** when you click "Open in Viewer". 

**How it works**: When you click "Open in Viewer", the system first adds the generated DDI-CDI metadata as a file to your Dataverse dataset (with the correct JSON-LD MIME type), then opens the cdi-viewer with standard Dataverse parameters (`fileid` and `siteUrl`). The viewer loads the file directly from Dataverse using its API.

The cdi-viewer provides:

#### Visual Interface Features
- **Hierarchical tree display** of your DDI-CDI metadata
- **Collapsible sections** for dataset, data structures, and variables
- **Search functionality** to find specific elements
- **Color-coded validation** indicators

#### Real-Time Validation
- **SHACL shape validation** against official DDI-CDI shapes from ddi-cdi.github.io
- **Visual indicators** for validation errors or warnings
- **Inline property suggestions** based on DDI-CDI schema

#### Editing Capabilities
- **Toggle edit mode** with the "Enable Editing" button
- **Direct editing** of generated metadata values
- **Add or remove** properties and nodes
- **Changes tracked** and highlighted in the interface

#### Export and Save
- **JSON-LD format**: Standard linked data serialization with DDI-CDI 1.0 context
- **Export button**: Download the current metadata as a `.jsonld` file
- **Save to Dataverse**: Click "Save to Dataverse" to replace the file in Dataverse with your edits
- The saved file can be downloaded, versioned, and shared with your dataset

#### How Viewer Integration Works
When opened from rdm-integration, the viewer receives standard Dataverse parameters:
- **`fileid`**: The Dataverse file ID of the generated DDI-CDI file
- **`siteUrl`**: The Dataverse server URL

The viewer loads the file content directly from Dataverse using these parameters. When you save changes, the viewer replaces the existing file in the dataset. This approach:
- Works seamlessly across different origins (rdm-integration and cdi-viewer)
- Leverages standard Dataverse file APIs
- Ensures proper authentication and authorization
- Uses the correct DDI-CDI MIME type: `application/ld+json;profile="http://www.w3.org/ns/json-ld#flattened http://www.w3.org/ns/json-ld#compacted https://ddialliance.org/specification/ddi-cdi/1.0"`

### Console Output

While generation is running or after completion, the **console output area** (left side of the interface) shows:

- Processing status messages
- File analysis progress
- Python script output from `cdi_generator_jsonld.py`
- Any warnings or non-fatal errors
- Completion confirmation

This output is useful for:
- **Debugging** if issues occur
- **Understanding** what metadata sources were used
- **Verifying** which files were processed

### Caching and Performance

The DDI-CDI feature includes intelligent caching to improve user experience:

#### Automatic Result Caching
- All generated metadata is **cached in Redis** for 24 hours
- Cache key is based on the **dataset persistent ID**
- Results are automatically retrieved when you revisit the page or select the same dataset

#### Auto-Load on Page Visit
- When you select a dataset (or arrive via a direct link), the system checks for cached results
- If found, the metadata is **immediately displayed** without regenerating
- Timestamp notification shows when the metadata was generated
- You can review and edit the cached metadata instantly

#### Manual Refresh
- Use the **"Refresh"** button to reload cached output
- Helpful if you want to:
  - Discard unsaved edits
  - Start over with the original generated metadata
  - Verify the latest cached version

#### Re-generation
- To generate fresh metadata (e.g., after updating files):
  - Click **"Generate DDI-CDI"** to start a new job
  - This replaces the cached output with new results
  - Previous cache is overwritten once the new job completes

**Performance Benefit**: Caching eliminates wait times when returning to previously processed datasets, making metadata review and editing nearly instantaneous.

### Common Scenarios and Tips

#### Scenario 1: Large Dataset with Many Files
**Problem**: Your dataset has hundreds of files, but only a few are tabular data.

**Solution**: 
- The interface automatically selects only supported file types (CSV, TSV, TAB)
- Review the auto-selected files in the tree table
- Deselect any files you don't want to process
- This saves processing time and focuses on relevant files

#### Scenario 2: Returning After Email Notification
**Problem**: You received an email that DDI-CDI generation completed (you opted in for success emails).

**Solution**:
- Click the link in the email - it includes your dataset's persistent ID
- The page loads with your dataset pre-selected
- Cached metadata appears automatically in the preview
- Click "Open in Viewer" to edit, then use cdi-viewer's "Save to Dataverse" button

**Note**: If you didn't opt in for success emails, you won't receive a notification when generation completes successfully. Simply return to the DDI-CDI page and select your dataset - cached results will load automatically.

#### Scenario 3: Files Updated in Dataverse
**Problem**: You've uploaded new data files or updated existing ones in Dataverse.

**Solution**:
- Navigate to the DDI-CDI component and select your dataset
- Review the file selection (new files will appear)
- Click "Generate DDI-CDI" to create fresh metadata
- This replaces the cached output with new results

#### Scenario 4: Need to Start Over with Edits
**Problem**: You've made edits in the cdi-viewer but want to discard them.

**Solution**:
- Click the **"Reload viewer"** button
- This reloads the cached output, discarding unsaved changes
- You're back to the last generated version

#### Scenario 5: Unsupported File Types
**Problem**: Your dataset contains SPSS `.sav` or Stata `.dta` files that aren't being processed.

**Solution**:
- These binary formats require Dataverse's native ingest first
- After ingest, Dataverse creates `.tab` files
- The DDI-CDI feature will process these `.tab` files
- Alternatively, use syntax files (`.sps`, `.sas`, `.dct`) with xconvert

#### Scenario 6: Processing Takes Too Long
**Problem**: Generation seems to be taking a very long time.

**Solution**:
- This is normal for very large files (>1GB)
- Close the browser - processing continues in background
- You'll receive an email when complete
- Return later via the email link or by selecting your dataset again

#### Scenario 7: Error in Generated Metadata
**Problem**: The generated metadata has incorrect or missing information.

**Solution**:
- Click "Open in Viewer" to open the cdi-viewer
- Use the viewer to **edit** the metadata directly (click "Enable Editing")
- Add missing fields or correct errors
- Click "Save to Dataverse" in the viewer to save your corrections
- Consider improving source metadata in Dataverse for better future results

---

## Technical Details

**Navigation**: [↑ Table of Contents](#table-of-contents) | [← User Guide](#using-the-feature) | [← Processing Pipeline](#how-it-works-the-processing-pipeline) | [→ Standards & Compliance](#metadata-standards-and-compliance)

### Architecture

The feature uses a hybrid architecture:

- **Go backend**: Handles job orchestration, authentication, file system access, caching, and adding files to Dataverse
- **Python script**: Performs data profiling and JSON-LD generation (`cdi_generator_jsonld.py`)
- **Redis queue**: Manages background job processing
- **cdi-viewer**: Provides interactive metadata viewing and editing (opens in new browser window, loads files via Dataverse API)
- **Angular frontend**: Displays preview, handles generation requests, and opens cdi-viewer with Dataverse file parameters

### The cdi_generator_jsonld.py Script

The core metadata generation is performed by [`cdi_generator_jsonld.py`](image/cdi_generator_jsonld.py), a Python script designed with contributions in mind:

- **Clean, documented code** with clear function boundaries
- **Standard Python libraries** (rdflib, chardet, datasketch, python-dateutil)
- **JSON-LD output** following DDI-CDI 1.0 specification
- **Official DDI-CDI context** from `https://ddi-cdi.github.io/m2t-ng/DDI-CDI_1-0/encoding/json-ld/ddi-cdi.jsonld`
- **Streaming architecture** for memory efficiency
- **Modular design** making it easy to add features or fix issues

**Contributions welcome!** If you have Python knowledge and want to improve the DDI-CDI generation:

- Add support for new data types or patterns
- Improve type inference accuracy
- Enhance statistical profiling
- Add new metadata enrichments
- Fix bugs or improve performance

View the script here: [image/cdi_generator_jsonld.py](image/cdi_generator_jsonld.py)

### Testing

The feature includes comprehensive test coverage to ensure reliability.

#### Running Python Tests

To run the Python tests:

```bash
# Run all tests (Python + Go)
make test

# Run only Python tests
make test-python

# Run tests directly
cd image
python3 -m venv venv
source venv/bin/activate
pip install -r requirements.txt
python3 test_csv_to_cdi.py
```

#### What the Tests Cover

The test suite includes dozens of tests covering:

1. **CSV Processing Tests**:
   - Type inference (integers, decimals, booleans, dates, strings)
   - Role detection (identifiers, measures, dimensions, attributes)
   - Missing value handling
   - Encoding detection
   - Delimiter detection

2. **Metadata Integration Tests**:
   - Dataset metadata extraction from Dataverse JSON
   - DDI XML parsing and variable enrichment
   - File URI construction
   - Title and description handling

3. **RDF Generation Tests**:
   - Valid JSON-LD syntax generation
   - Required CDI properties presence
   - Correct DDI-CDI 1.0 context reference
   - Variable and dataset linking

4. **xconvert Integration Tests** (12 tests):
   - SPSS to DDI conversion
   - SAS to DDI conversion
   - Stata to DDI conversion
   - Error handling for missing or invalid files
   - Complete workflow: statistical file → DDI → CDI-RDF
  - Fixture validation against `testdata/tmp_ddi8.xml` (mirrors Dataverse `GetDataFileDDI` output)

5. **Error Handling Tests**:
   - Graceful degradation when metadata unavailable
   - Warning messages for partial failures
   - Proper cleanup of temporary files

All tests are automatically skipped if required tools (like xconvert) are not available, making the suite robust across different environments.

---

## Performance and Scalability

**Navigation**: [↑ Table of Contents](#table-of-contents) | [← Technical Details](#technical-details) | [→ Limitations & Future](#limitations-and-future-enhancements)

### Memory Efficiency

The pipeline is designed to handle large datasets:

- **Streaming processing**: CSV files are read row-by-row, not loaded entirely into memory
- **Probabilistic algorithms**: HyperLogLog provides approximate distinct counts with minimal memory
- **Temporary file management**: Work directories are cleaned up after processing

### Parallel Processing

Multiple datasets can be processed simultaneously:

- Each job runs independently on its own goroutine
- Queue-based scheduling prevents resource overload
- Configurable worker count per queue

### Caching

Results are cached for quick retrieval:

- Completed metadata is stored in Redis
- Cache entries are automatically cleaned up after retrieval
- Supports multiple concurrent users

---

## Limitations and Future Enhancements

**Navigation**: [↑ Table of Contents](#table-of-contents) | [← Performance](#performance-and-scalability) | [→ Standards & Compliance](#metadata-standards-and-compliance)

### Current Limitations

- **Dataverse ingest required for binary formats**: Binary statistical files (SPSS `.sav`, Stata `.dta`, R `.RData`, Excel `.xlsx`) must be ingested by Dataverse first. Files exceeding the configured `TabularIngestSizeLimit` (typically 2GB) cannot be processed through native ingest.
- **Fixed-column formats for xconvert**: The xconvert tool supports only fixed-column SAS/Stata syntax definitions, not free-field formats.
- **CSV ingest limitations**: Dataverse's CSV ingest has limited support - the DDI-CDI feature provides more comprehensive analysis for CSV files.
- **Processing time**: Very large files (>1GB) may take several minutes to process, especially if streaming analysis is required.
- **Custom metadata sync**: Manual metadata additions are not yet integrated back into Dataverse.

### Planned Enhancements

Future versions may include:

- **Extended format support**: JSON, XML, and other structured data formats
- **Enhanced Excel processing**: More comprehensive metadata extraction from Excel workbooks
- **Large file optimization**: Improved handling for files exceeding Dataverse ingest limits
- **Real-time progress updates**: Live status during processing with estimated completion times
- **Interactive metadata editing**: Edit and save metadata directly back to Dataverse
- **Batch processing**: Generate DDI-CDI for entire collections or multiple datasets at once
- **Multiple output formats**: Export metadata as JSON-LD, RDF/XML, or other serializations
- **Quality reports**: Automated completeness scores and metadata quality assessments
- **Streaming ingest integration**: Tighter integration with Dataverse ingest API for real-time processing

---

## Metadata Standards and Compliance

**Navigation**: [↑ Table of Contents](#table-of-contents) | [← Limitations & Future](#limitations-and-future-enhancements) | [→ Getting Help & Contributing](#getting-help-and-contributing)

### DDI-CDI Specification

This feature implements the DDI-CDI 1.0 specification:

- **Namespace**: `http://ddialliance.org/Specification/DDI-CDI/1.0/RDF/`
- **JSON-LD Context**: `https://ddi-cdi.github.io/m2t-ng/DDI-CDI_1-0/encoding/json-ld/ddi-cdi.jsonld`
- **SHACL Shapes**: `https://ddi-cdi.github.io/m2t-ng/DDI-CDI_1-0/encoding/shacl/ddi-cdi.shacl.ttl`
- **Documentation**: [https://ddialliance.org/Specification/DDI-CDI/](https://ddialliance.org/Specification/DDI-CDI/)

### DDI Codebook 2.x

For statistical file metadata, the feature uses:

- DDI Codebook 2.x format from xconvert
- Variable-level documentation with `<var>` elements
- Category definitions with value labels
- Summary statistics (mean, min, max, standard deviation)
- **Documentation**: [https://ddialliance.org/Specification/DDI-Codebook/](https://ddialliance.org/Specification/DDI-Codebook/)

### RDF and Semantic Web Standards

Generated metadata follows:

- **JSON-LD**: JavaScript Object Notation for Linked Data
- **RDF**: Resource Description Framework for linked data
- **PROV**: W3C provenance vocabulary for processing history
- **DCTERMS**: Dublin Core terms for dataset descriptions
- **SKOS**: Simple Knowledge Organization System for labels and concepts

---

## Getting Help and Contributing

**Navigation**: [↑ Table of Contents](#table-of-contents) | [← Standards & Compliance](#metadata-standards-and-compliance) | [→ Credits & Acknowledgments](#credits-and-acknowledgments)

### Documentation Resources

- **This document**: Overview and user guide
- **README.md**: Installation and setup instructions
- **API Documentation**: Technical integration details (in README.md)

### Reporting Issues

If you encounter problems:

1. Check that your files are in supported formats
2. Verify you have necessary permissions in Dataverse
3. Review any error messages or warnings
4. Check the application logs for detailed diagnostics

### Contributing

We welcome contributions, especially to the Python metadata generation:

- **Python developers**: Enhance cdi_generator_jsonld.py with new features
- **Data scientists**: Improve statistical profiling algorithms
- **Metadata experts**: Refine DDI-CDI mappings and enrichments
- **Testers**: Add test cases for edge cases and new formats

The Python codebase is designed to be accessible - basic Python knowledge is sufficient to make meaningful contributions.

---

## Credits and Acknowledgments

**Navigation**: [↑ Table of Contents](#table-of-contents) | [← Getting Help & Contributing](#getting-help-and-contributing) | [→ Appendix](#appendix-cdi_generator_jsonldpy-deep-dive)

This feature integrates several open-source tools and standards:

- **cdi-viewer**: Interactive DDI-CDI viewer and editor from KU Leuven ([https://github.com/libis/cdi-viewer](https://github.com/libis/cdi-viewer))
- **xconvert**: Statistical file converter from UC Berkeley SDA project ([https://sda.berkeley.edu/ddi/tools/xconvert.html](https://sda.berkeley.edu/ddi/tools/xconvert.html))
- **DDI Alliance**: International standards body for data documentation
- **Dataverse Project**: Open-source research data repository software

### Dependencies

Python libraries:
- rdflib: RDF graph construction and serialization
- chardet: Character encoding detection
- datasketch: Probabilistic data structures (HyperLogLog)
- python-dateutil: Date and time parsing

---

## License

This project is licensed under the Apache License, Version 2.0. See [LICENSE.txt](LICENSE.txt) for details.

---

## Appendix: cdi_generator_jsonld.py Deep Dive

**Navigation**: [↑ Table of Contents](#table-of-contents) | [← Credits & Acknowledgments](#credits-and-acknowledgments) | [↑ Back to Top](#ddi-cdi-metadata-generation)

This appendix provides a technical, implementation-oriented overview of the Python generator referenced throughout this document: [cdi_generator_jsonld.py](image/cdi_generator_jsonld.py).

### What it is

- A streaming CSV/TSV profiler and DDI‑CDI 1.0 JSON-LD generator.
- Reads one or many tabular files, infers per‑column datatype and role, optionally enriches with Dataverse metadata and DDI fragments, and emits a DDI-CDI compliant JSON-LD document.
- Uses the official DDI-CDI 1.0 context from ddi-cdi.github.io.
- Scales to large files by streaming rows (no full‑file loads).

### Inputs and outputs

- Inputs
  - Manifest JSON describing multiple files and dataset context
- Optional enrichments
  - Dataverse dataset JSON: title, description, creators (+ORCID), subjects, license, issued date, publisher, per‑file URIs.
  - DDI XML fragment (from Dataverse ingest or xconvert): variable labels, categories, summary statistics; stored as rdf:XMLLiteral when well‑formed.
- Outputs
  - JSON-LD file (.jsonld) with DDI-CDI 1.0 context, containing WideDataSet, WideDataStructure, Variables, and provenance.
  - Optional JSON summary with per‑column profiling (approx distinct counts, datatype, role).

### High‑level flow

1. Parse CLI, configure logging.
2. Load manifest and create a single WideDataSet node; add dataset‑level info from Dataverse JSON when available.
3. Iterate files: profile CSV, optionally obtain DDI (native or via xconvert), then add WideDataStructure + LogicalRecord + Variables.
4. Serialize combined graph to JSON-LD with DDI-CDI 1.0 context; optionally write a summary JSON used by the job logs/UI.

### Key components

- DDI-CDI Types and Structure
  - Uses official DDI-CDI 1.0 types:
    - WideDataSet: root container for the dataset
    - WideDataStructure: defines the structure of the tabular data
    - LogicalRecord: represents the logical organization
    - InstanceVariable, RepresentedVariable, SubstantiveValueDomain: variable representation
    - IdentifierComponent, MeasureComponent, DimensionComponent, AttributeComponent: variable roles
- ColumnStats (streaming inference)
  - Determines XSD datatype (integer, decimal, boolean, dateTime, string) and approximate distinct counts (HyperLogLog).
  - Role heuristic:
    - identifier: ≥ ~95% distinct (for ≥ 50 non‑missing rows)
    - measure: numeric but non‑unique
    - dimension: boolean or low‑cardinality text
    - attribute: everything else
- CSV ingestion
  - Encoding via chardet; delimiter via csv.Sniffer; header detection guarded by a “typed cell ratio” so first data rows aren’t misclassified as headers.
- DDI handling and xconvert
  - Parses DDI XML; extracts variable label, categories, and simple statistics.
  - Valid XML is preserved as an `rdf:XMLLiteral` on the PhysicalDataSet `dcterms:source`.
  - Can auto‑run UC Berkeley’s xconvert for `.sps`, `.sas`, `.do`, `.dct` to produce DDI when no fragment is supplied.
- Dataverse metadata extraction
  - Title, description, authors (+ORCID), subjects, license (URI/name), issued date, publisher; per‑file persistent or access URIs.

### JSON-LD output structure

- WideDataSet
  - Root container with `dcterms:title`, `dcterms:description`, `dcterms:creator`, `dcterms:issued`.
  - Links to WideDataStructure via `cdi:WideDataSet_has_WideDataStructure`.
- WideDataStructure
  - Defines data organization with PhysicalSegmentLayout and LogicalRecord.
  - Links to components (IdentifierComponent, MeasureComponent, etc.).
- InstanceVariable
  - Represents each column with `cdi:name`, `cdi:displayLabel`.
  - Links to RepresentedVariable via `cdi:InstanceVariable_representedVariable`.
- RepresentedVariable and SubstantiveValueDomain
  - Captures the semantic representation and data type.
  - `cdi:dataType` set to inferred XSD datatype URI.
- Component Types
  - IdentifierComponent, MeasureComponent, DimensionComponent, AttributeComponent based on role heuristic.
- Provenance
  - Linked via PROV vocabulary with generation timestamp and tool identification.

### Noteworthy behavior: dataset description

- If the dataset has a description from Dataverse metadata, it is included in the WideDataSet.
- File-level information is captured in the WideDataStructure and PhysicalSegmentLayout.
- JSON-LD naturally handles multi-line strings without special quoting.

### CLI at a glance

```bash
python cdi_generator_jsonld.py --manifest <path> --output <path> [--summary-json <path>] [--skip-md5] [--quiet] [--verbose]
```

- `--manifest`: Path to dataset manifest JSON (required)
- `--output`: Output JSON-LD path (default: `dataset.cdi.jsonld`)
- `--summary-json`: Optional path to write column summary as JSON
- `--skip-md5`: Skip MD5 checksum calculation for faster runs
- `--quiet`: Suppress console summary output
- `--verbose`: Enable verbose logging

### Quick tweak tips

- Switch link style (if a different profile is mandated): set `ACTIVE_LINKS = FALLBACK_LINKS`.
- Tune inference: update `ColumnStats.xsd_datatype()` and `ColumnStats.role()`.


