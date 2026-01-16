# DDI-CDI Update Plan: Migration to cdi-viewer

## ✅ IMPLEMENTATION COMPLETED

This migration has been successfully implemented. Below is the summary of changes made.

---

## Changes Summary

### 1. Python Generator (NEW: cdi_generator_jsonld.py)
- **Created**: `image/cdi_generator_jsonld.py` - New JSON-LD generator
- **Output format**: JSON-LD with proper DDI-CDI 1.0 context
- **Context URL**: `https://ddi-cdi.github.io/m2t-ng/DDI-CDI_1-0/encoding/json-ld/ddi-cdi.jsonld`
- **Namespace**: `http://ddialliance.org/Specification/DDI-CDI/1.0/RDF/` (correct, without www)
- **File extension**: `.jsonld`

### 2. Go Backend Updates
- **Modified**: `image/app/core/ddi_cdi.go`
  - Changed output file extension from `.ttl` to `.jsonld`
  - Changed script path to `cdi_generator_jsonld.py`
- **Modified**: `image/app/server/http_server.go`
  - Removed SHACL endpoint `/api/frontend/shacl`
- **Deleted**: `image/app/frontend/shacl.go` - No longer needed
- **Deleted**: `image/app/frontend/default_shacl_shapes.ttl` - Using official shapes via cdi-viewer

### 3. Angular Frontend Updates (rdm-integration-frontend)
- **Modified**: `src/app/ddi-cdi/ddi-cdi.component.ts`
  - Replaced SHACL form with cdi-viewer iframe
  - Added postMessage communication with iframe
  - Removed n3.js imports (no more Turtle parsing)
  - Removed 200+ line SHACL template
  - Updated file extension from `.ttl` to `.jsonld`
- **Modified**: `src/app/ddi-cdi/ddi-cdi.component.html`
  - Replaced `<shacl-form>` element with `<iframe>` pointing to cdi-viewer
  - Updated dialog text to mention JSON-LD instead of Turtle
- **Modified**: `src/app/data.service.ts`
  - Removed `getShaclTemplate()` method
  - Removed SHACL URL constant
- **Deleted**: `src/app/shacl-form-patch.ts` - No longer needed

### 4. CDI Viewer Updates (cdi-viewer)
- **Created**: `src/jsonld-editor/postmessage-handler.js`
  - Handles `loadJsonLd`, `getJsonLd`, `getChanges`, `setEditMode` messages
  - Enables iframe embedding with postMessage API
  - Automatic handshake with parent window
- **Modified**: `src/index.js`
  - Added import for postmessage-handler.js

---

## Architecture After Migration

```
┌────────────────────────────────────────────────────────────────────┐
│                    rdm-integration-frontend                        │
│  ┌─────────────────────────────────────────────────────────────┐   │
│  │ DDI-CDI Component                                           │   │
│  │ ┌─────────────────────────────────────────────────────────┐ │   │
│  │ │ <iframe src="https://libis.github.io/cdi-viewer/">      │ │   │
│  │ │   ↕ postMessage(loadJsonLd / getJsonLd)                 │ │   │
│  │ └─────────────────────────────────────────────────────────┘ │   │
│  └─────────────────────────────────────────────────────────────┘   │
└────────────────────────────────────────────────────────────────────┘
                              ↓
                    JSON-LD generation request
                              ↓
┌────────────────────────────────────────────────────────────────────┐
│                       rdm-integration backend                      │
│                                                                    │
│  cdi_generator_jsonld.py → WideDataSet.jsonld                      │
│                                                                    │
│  Output: application/ld+json with DDI-CDI 1.0 context              │
└────────────────────────────────────────────────────────────────────┘
```

---

## Original Planning Document

This document outlines the plan to update the DDI-CDI generation in rdm-integration to:
1. **Replace the ULB Darmstadt SHACL form** with the **cdi-viewer** we built at the workshop
2. **Output JSON-LD** instead of Turtle (correct DDI-CDI format)
3. **Use official DDI-CDI SHACL shapes** from `ddi-cdi.github.io` (already integrated in cdi-viewer)
4. **Use correct MIME type** `application/ld+json` for DDI-CDI files
5. **Simplify the architecture** by removing custom SHACL shapes hosting

---

## Table of Contents

1. [Current State Analysis](#1-current-state-analysis)
2. [Target State](#2-target-state)
3. [Gap Analysis](#3-gap-analysis)
4. [Implementation Plan](#4-implementation-plan)
5. [File Changes Summary](#5-file-changes-summary)
6. [Testing Strategy](#6-testing-strategy)
7. [Migration Checklist](#7-migration-checklist)

---

## 1. Current State Analysis

### 1.1 Output Format: Turtle (WRONG)

**Current implementation** generates RDF/Turtle format:

```python
# image/cdi_generator.py
graph.serialize(destination=str(out_path), format="turtle")
```

**Files saved as**: `ddi-cdi-{timestamp}.ttl`

**MIME type used**: `text/turtle`

### 1.2 Custom SHACL Shapes (WRONG)

The backend hosts custom SHACL shapes that are **not the official DDI-CDI shapes**:

| File | Lines | Purpose |
|------|-------|---------|
| [image/app/frontend/default_shacl_shapes.ttl](image/app/frontend/default_shacl_shapes.ttl) | 214 | Custom simplified shapes |
| [image/app/frontend/shacl.go](image/app/frontend/shacl.go) | 35 | Serves shapes via `/api/frontend/shacl` |

These custom shapes define:
- `<urn:ddi-cdi:DatasetShape>` - Not official DDI-CDI
- `<urn:ddi-cdi:VariableShape>` - Not official DDI-CDI
- Uses `__TARGET_NODE__` placeholder mechanism

### 1.3 ULB Darmstadt SHACL Form (TO BE REPLACED)

The frontend uses the `@ulb-darmstadt/shacl-form` web component:

```html
<!-- rdm-integration-frontend: ddi-cdi.component.html -->
<shacl-form
  [attr.data-shapes]="shaclShapes"
  [attr.data-values]="generatedDdiCdi"
  [attr.data-values-format]="'text/turtle'"
  data-dense="true"
></shacl-form>
```

Related files:
- [rdm-integration-frontend/src/app/ddi-cdi/ddi-cdi.component.ts](../rdm-integration-frontend/src/app/ddi-cdi/ddi-cdi.component.ts) (1347 lines)
- [rdm-integration-frontend/src/app/ddi-cdi/shacl-form-patch.ts](../rdm-integration-frontend/src/app/ddi-cdi/shacl-form-patch.ts) (42 lines)

### 1.4 RDF Namespace Issues

Current generator uses non-standard predicates and structure:

```python
# image/cdi_generator.py - Current namespace
CDI = Namespace("http://www.ddialliance.org/Specification/DDI-CDI/1.0/RDF/")
# Note: Uses "www." prefix - may not match official shapes
```

**Official namespace** (from cdi-viewer/shapes/ddi-cdi-official.ttl):
```turtle
PREFIX cdi: <http://ddialliance.org/Specification/DDI-CDI/1.0/RDF/>
# Note: NO "www." prefix
```

### 1.5 Current Data Flow

```
┌────────────────┐    ┌─────────────────┐    ┌──────────────────┐
│ Go Backend     │───▶│ cdi_generator.py │───▶│ output.ttl       │
│ (job queue)    │    │ (RDF/Turtle)     │    │ (Turtle format)  │
└────────────────┘    └─────────────────┘    └────────┬─────────┘
                                                       │
                                                       ▼
┌────────────────────────────────────────────────────────────────┐
│ Angular Frontend                                                │
│  ┌───────────────────┐    ┌──────────────────────────────────┐ │
│  │ Fetch SHACL shapes│───▶│ <shacl-form> (Darmstadt)         │ │
│  │ /api/frontend/    │    │ - Renders Turtle                  │ │
│  │ shacl             │    │ - Edits in memory                 │ │
│  └───────────────────┘    │ - Saves as .ttl                   │ │
│                           └──────────────────────────────────┘ │
└────────────────────────────────────────────────────────────────┘
```

---

## 2. Target State

### 2.1 Output Format: JSON-LD (CORRECT)

DDI-CDI standard uses JSON-LD as the primary serialization:

```python
# Target: image/cdi_generator.py
graph.serialize(destination=str(out_path), format="json-ld", context=DDI_CDI_CONTEXT)
```

**Files saved as**: `ddi-cdi-{timestamp}.jsonld`

**MIME type**: `application/ld+json`

### 2.2 Official SHACL Shapes via cdi-viewer

cdi-viewer automatically loads official shapes from:
```
https://ddi-cdi.github.io/m2t-ng/DDI-CDI_1-0/encoding/shacl/ddi-cdi.shacl.ttl
```

Local fallback available at:
```
cdi-viewer/shapes/ddi-cdi-official.ttl (15,348 lines)
```

### 2.3 cdi-viewer Integration

Replace Darmstadt SHACL form with cdi-viewer embedded via iframe:

```html
<iframe 
  [src]="cdiViewerUrl"
  width="100%" 
  height="800px"
  allow="clipboard-write">
</iframe>
```

cdi-viewer capabilities:
- ✅ Official DDI-CDI 1.0 SHACL shapes (auto-loaded)
- ✅ JSON-LD format native support
- ✅ Correct MIME type `application/ld+json`
- ✅ Dataverse integration (save back to dataset)
- ✅ Visual editing with real-time validation
- ✅ Handles complex nested structures
- ✅ Supports loading from Dataverse file API

### 2.4 Target Data Flow

```
┌────────────────┐    ┌─────────────────┐    ┌──────────────────┐
│ Go Backend     │───▶│ cdi_generator.py │───▶│ output.jsonld    │
│ (job queue)    │    │ (JSON-LD)        │    │ (JSON-LD format) │
└────────────────┘    └─────────────────┘    └────────┬─────────┘
                                                       │
                                                       ▼
┌────────────────────────────────────────────────────────────────┐
│ Angular Frontend                                                │
│  ┌───────────────────────────────────────────────────────────┐ │
│  │ <iframe src="cdi-viewer/?embedded=true&data=...">         │ │
│  │   - Official DDI-CDI shapes (auto-loaded)                  │ │
│  │   - JSON-LD editing                                        │ │
│  │   - Save to Dataverse built-in                             │ │
│  └───────────────────────────────────────────────────────────┘ │
└────────────────────────────────────────────────────────────────┘
```

---

## 3. Gap Analysis

| Aspect | Current | Target | Effort |
|--------|---------|--------|--------|
| **Output format** | Turtle (.ttl) | JSON-LD (.jsonld) | Medium |
| **MIME type** | text/turtle | application/ld+json | Low |
| **SHACL shapes** | Custom 214-line shapes | Official 15,348-line shapes | Remove |
| **Viewer/Editor** | Darmstadt shacl-form | cdi-viewer (iframe) | Medium |
| **CDI namespace** | `www.ddialliance.org` | `ddialliance.org` (no www) | Low |
| **JSON-LD context** | None | Official DDI-CDI context URL | Medium |
| **Data structure** | Flat graph with BNodes | Proper @graph with @id refs | High |
| **File extension** | .ttl | .jsonld | Low |

### 3.1 Critical Changes Required

#### 3.1.1 cdi_generator.py - JSON-LD Output

The Python generator must:

1. **Use official DDI-CDI namespace** (without www)
2. **Serialize as JSON-LD** with proper @context
3. **Follow DDI-CDI structure**:
   - `WideDataSet` or `LongDataSet` as root type
   - `WideDataStructure` / `LongDataStructure` for structure
   - `InstanceVariable` + `RepresentedVariable` for variables
   - `SubstantiveValueDomain` for data types
   - Component types: `IdentifierComponent`, `MeasureComponent`, `AttributeComponent`

4. **Use official JSON-LD context**:
   ```json
   {
     "@context": "https://ddi-cdi.github.io/m2t-ng/DDI-CDI_1-0/encoding/json-ld/ddi-cdi.jsonld"
   }
   ```

#### 3.1.2 Frontend - Replace SHACL Form with cdi-viewer

Options for integration:

**Option A: Iframe Embed (Recommended)**
```typescript
// ddi-cdi.component.ts
getCdiViewerUrl(): SafeResourceUrl {
  const baseUrl = 'https://libis.github.io/cdi-viewer/';
  const params = new URLSearchParams({
    siteUrl: this.dataverseUrl,
    embedded: 'true'
  });
  return this.sanitizer.bypassSecurityTrustResourceUrl(`${baseUrl}?${params}`);
}
```

**Option B: External Tool Registration**
Register cdi-viewer as a Dataverse external tool for `.jsonld` files:
```json
{
  "displayName": "DDI-CDI Viewer/Editor",
  "contentType": "application/ld+json",
  "toolUrl": "https://libis.github.io/cdi-viewer/",
  "scope": "file",
  "type": "preview"
}
```

#### 3.1.3 Backend - Remove Custom SHACL Hosting

Files to delete:
- `image/app/frontend/default_shacl_shapes.ttl`
- `image/app/frontend/shacl.go` (or repurpose)

API endpoint to remove:
- `GET /api/frontend/shacl`

---

## 4. Implementation Plan

### Phase 1: Update cdi_generator.py (JSON-LD Output)

**Priority: HIGH** | **Estimated effort: 2-3 days**

#### Task 1.1: Fix DDI-CDI Namespace

```python
# BEFORE
CDI = Namespace("http://www.ddialliance.org/Specification/DDI-CDI/1.0/RDF/")

# AFTER
CDI = Namespace("http://ddialliance.org/Specification/DDI-CDI/1.0/RDF/")
```

#### Task 1.2: Add JSON-LD Context Support

```python
# New constant
DDI_CDI_CONTEXT = "https://ddi-cdi.github.io/m2t-ng/DDI-CDI_1-0/encoding/json-ld/ddi-cdi.jsonld"

# Serialization
def serialize_jsonld(graph: Graph, output_path: Path) -> None:
    """Serialize RDF graph as JSON-LD with DDI-CDI context."""
    jsonld_str = graph.serialize(format="json-ld", context=DDI_CDI_CONTEXT)
    output_path.write_text(jsonld_str, encoding="utf-8")
```

#### Task 1.3: Update Type Mappings

Map current types to official DDI-CDI types:

| Current | Official DDI-CDI |
|---------|-----------------|
| `cdi:DataSet` | `cdi:WideDataSet` |
| `cdi:PhysicalDataSet` | Keep or use `cdi:PhysicalRecordSegment` |
| `cdi:LogicalDataSet` | `cdi:LogicalRecord` |
| `cdi:Variable` | `cdi:InstanceVariable` + `cdi:RepresentedVariable` |
| Role literals | `cdi:IdentifierComponent`, `cdi:MeasureComponent`, `cdi:AttributeComponent` |

#### Task 1.4: Restructure RDF Graph

Follow the SimpleSample.jsonld structure from cdi-viewer:

```python
def build_cdi_graph(dataset_info, files_info):
    """Build DDI-CDI compliant RDF graph."""
    g = Graph()
    g.bind("cdi", CDI)
    
    # Root dataset
    dataset_uri = URIRef(f"#{safe_fragment(dataset_info['title'])}")
    g.add((dataset_uri, RDF.type, CDI.WideDataSet))
    g.add((dataset_uri, CDI.name, Literal(dataset_info['title'])))
    
    # Data structure
    structure_uri = URIRef("#datastructure")
    g.add((structure_uri, RDF.type, CDI.WideDataStructure))
    g.add((dataset_uri, CDI.isStructuredBy, structure_uri))
    
    # Variables with proper type hierarchy
    for var in variables:
        var_uri = URIRef(f"#{safe_fragment(var['name'])}")
        g.add((var_uri, RDF.type, CDI.InstanceVariable))
        g.add((var_uri, RDF.type, CDI.RepresentedVariable))
        g.add((var_uri, CDI.name, Literal(var['name'])))
        
        # Value domain for data type
        domain_uri = URIRef(f"#{safe_fragment(var['name'])}_domain")
        g.add((domain_uri, RDF.type, CDI.SubstantiveValueDomain))
        g.add((domain_uri, CDI.recommendedDataType, 
               URIRef(map_to_ddi_datatype(var['xsd_type']))))
        g.add((var_uri, CDI.takesSubstantiveValuesFrom, domain_uri))
        
        # Component with role
        comp_uri = URIRef(f"#{safe_fragment(var['name'])}_component")
        comp_type = role_to_component_type(var['role'])
        g.add((comp_uri, RDF.type, comp_type))
        g.add((comp_uri, CDI.isDefinedBy, var_uri))
        g.add((structure_uri, CDI.has, comp_uri))
    
    return g
```

#### Task 1.5: Update CLI Arguments

```python
parser.add_argument('--format', choices=['jsonld', 'turtle'], default='jsonld',
                    help='Output format (default: jsonld)')
parser.add_argument('--context', default=DDI_CDI_CONTEXT,
                    help='JSON-LD context URL')
```

### Phase 2: Update Go Backend

**Priority: HIGH** | **Estimated effort: 1 day**

#### Task 2.1: Update File Extension

```go
// core/ddi_cdi.go
// BEFORE
outputPath := filepath.Join(workDir, "output.ttl")

// AFTER  
outputPath := filepath.Join(workDir, "output.jsonld")
```

#### Task 2.2: Update MIME Type

```go
// common/ddi_cdi.go - if serving file directly
// BEFORE
w.Header().Set("Content-Type", "text/turtle")

// AFTER
w.Header().Set("Content-Type", "application/ld+json")
```

#### Task 2.3: Remove SHACL Endpoint

Delete or repurpose:
- `image/app/frontend/shacl.go`
- `image/app/frontend/default_shacl_shapes.ttl`

Remove route from `server/http_server.go`:
```go
// DELETE THIS LINE
srvMux.HandleFunc("/api/frontend/shacl", frontend.GetShaclShapes)
```

### Phase 3: Update Angular Frontend

**Priority: HIGH** | **Estimated effort: 2-3 days**

#### Task 3.1: Replace SHACL Form with cdi-viewer Iframe

```typescript
// ddi-cdi.component.ts

// Remove these imports
// import '@nicovank/shacl-form';
// import './shacl-form-patch';

// Add new property
cdiViewerUrl: SafeResourceUrl | null = null;

// New method to build cdi-viewer URL
buildCdiViewerUrl(jsonldContent: string): void {
  // Option 1: Pass data via postMessage after iframe loads
  // Option 2: Store in temporary endpoint and pass URL
  // Option 3: Base64 encode small payloads
  
  const baseUrl = environment.cdiViewerUrl; // 'https://libis.github.io/cdi-viewer/'
  const params = new URLSearchParams({
    shacl: 'ddi-cdi-official',
    embedded: 'true'
  });
  
  this.cdiViewerUrl = this.sanitizer.bypassSecurityTrustResourceUrl(
    `${baseUrl}?${params}`
  );
}
```

#### Task 3.2: Update Template

```html
<!-- ddi-cdi.component.html -->

<!-- REMOVE THIS -->
<!--
<shacl-form
  [attr.data-shapes]="shaclShapes"
  [attr.data-values]="generatedDdiCdi"
  ...
></shacl-form>
-->

<!-- ADD THIS -->
<div class="cdi-viewer-container" *ngIf="generatedDdiCdi">
  <iframe 
    #cdiViewerFrame
    [src]="cdiViewerUrl"
    class="cdi-viewer-iframe"
    (load)="onCdiViewerLoad()"
    allow="clipboard-write"
    sandbox="allow-scripts allow-same-origin allow-forms allow-popups">
  </iframe>
</div>
```

#### Task 3.3: Implement postMessage Communication

```typescript
// ddi-cdi.component.ts

@ViewChild('cdiViewerFrame') cdiViewerFrame: ElementRef<HTMLIFrameElement>;

onCdiViewerLoad(): void {
  // Send JSON-LD data to cdi-viewer via postMessage
  const iframe = this.cdiViewerFrame.nativeElement;
  iframe.contentWindow?.postMessage({
    type: 'load-jsonld',
    data: this.generatedDdiCdi,
    dataverseUrl: this.dataverseUrl,
    datasetPid: this.selectedDataset?.persistentId
  }, '*');
}

// Listen for save events from cdi-viewer
@HostListener('window:message', ['$event'])
onMessage(event: MessageEvent): void {
  if (event.data?.type === 'save-complete') {
    this.showSuccess('DDI-CDI metadata saved to dataset');
  }
}
```

#### Task 3.4: Add Styles

```css
/* ddi-cdi.component.scss */

.cdi-viewer-container {
  width: 100%;
  height: calc(100vh - 200px);
  min-height: 600px;
  border: 1px solid #ddd;
  border-radius: 4px;
  overflow: hidden;
}

.cdi-viewer-iframe {
  width: 100%;
  height: 100%;
  border: none;
}
```

#### Task 3.5: Remove Obsolete Code

Delete:
- `shacl-form-patch.ts`
- All SHACL form related methods in `ddi-cdi.component.ts`:
  - `setupShaclForm()`
  - `prepareShaclShapes()`
  - `mergeFormChanges()`
  - References to n3.js for Turtle parsing

Remove from `data.service.ts`:
```typescript
// DELETE
getShaclTemplate(): Observable<string> {
  return this.http.get('/api/frontend/shacl', { responseType: 'text' });
}
```

### Phase 4: Update cdi-viewer for Embedded Mode

**Priority: MEDIUM** | **Estimated effort: 1-2 days**

#### Task 4.1: Add postMessage Handler

```javascript
// cdi-viewer/src/jsonld-editor/core.js

window.addEventListener('message', (event) => {
  if (event.data?.type === 'load-jsonld') {
    loadJsonLdFromString(event.data.data);
    
    // Store Dataverse context for saving
    if (event.data.dataverseUrl) {
      window.embeddedDataverseUrl = event.data.dataverseUrl;
      window.embeddedDatasetPid = event.data.datasetPid;
    }
  }
});
```

#### Task 4.2: Add Embedded Mode Detection

```javascript
// cdi-viewer/src/jsonld-editor/state.js

export function isEmbeddedMode() {
  const params = new URLSearchParams(window.location.search);
  return params.get('embedded') === 'true' || window.parent !== window;
}
```

#### Task 4.3: Notify Parent on Save

```javascript
// cdi-viewer/src/jsonld-editor/data-extraction.js

export async function saveToDataverse() {
  // ... existing save logic ...
  
  // Notify parent if embedded
  if (isEmbeddedMode() && window.parent !== window) {
    window.parent.postMessage({
      type: 'save-complete',
      success: true,
      fileId: result.fileId
    }, '*');
  }
}
```

### Phase 5: Update External Tool Registration

**Priority: LOW** | **Estimated effort: 0.5 day**

#### Task 5.1: Register cdi-viewer for JSON-LD Files

Create new external tool config:

```json
// conf/dataverse/external-tools/04-cdi-viewer.json
{
  "displayName": "DDI-CDI Viewer/Editor",
  "description": "View and edit DDI-CDI JSON-LD metadata with official SHACL validation",
  "toolName": "cdiViewer",
  "scope": "file",
  "types": ["explore", "preview"],
  "hasPreviewMode": true,
  "toolUrl": "https://libis.github.io/cdi-viewer/",
  "httpMethod": "GET",
  "contentType": "application/ld+json",
  "toolParameters": {
    "queryParameters": [
      {"fileid": "{fileId}"},
      {"siteUrl": "{siteUrl}"},
      {"key": "{apiToken}"},
      {"datasetid": "{datasetId}"},
      {"datasetversion": "{datasetVersion}"}
    ]
  }
}
```

---

## 5. File Changes Summary

### 5.1 Files to Modify

| File | Changes |
|------|---------|
| `image/cdi_generator.py` | JSON-LD output, fix namespace, update types |
| `image/app/core/ddi_cdi.go` | Update output path (.jsonld) |
| `image/app/common/ddi_cdi.go` | Update MIME type if serving directly |
| `image/app/server/http_server.go` | Remove SHACL route |
| `rdm-integration-frontend/.../ddi-cdi.component.ts` | Replace SHACL form with iframe |
| `rdm-integration-frontend/.../ddi-cdi.component.html` | Update template |
| `rdm-integration-frontend/.../ddi-cdi.component.scss` | Add iframe styles |
| `rdm-integration-frontend/.../data.service.ts` | Remove getShaclTemplate() |
| `cdi-viewer/src/.../core.js` | Add postMessage handler |
| `cdi-viewer/src/.../state.js` | Add embedded mode detection |

### 5.2 Files to Delete

| File | Reason |
|------|--------|
| `image/app/frontend/default_shacl_shapes.ttl` | Replaced by official shapes in cdi-viewer |
| `image/app/frontend/shacl.go` | SHACL endpoint no longer needed |
| `rdm-integration-frontend/.../shacl-form-patch.ts` | SHACL form removed |

### 5.3 Files to Add

| File | Purpose |
|------|---------|
| `conf/dataverse/external-tools/04-cdi-viewer.json` | Register cdi-viewer for .jsonld files |

---

## 6. Testing Strategy

### 6.1 Unit Tests

#### Python (cdi_generator.py)

```python
def test_jsonld_output_format():
    """Verify output is valid JSON-LD with @context."""
    result = generate_cdi(test_csv_path)
    data = json.loads(result)
    assert '@context' in data
    assert data['@context'] == DDI_CDI_CONTEXT
    assert '@graph' in data or '@type' in data

def test_namespace_no_www():
    """Verify namespace uses ddialliance.org without www."""
    result = generate_cdi(test_csv_path)
    assert 'www.ddialliance.org' not in result
    assert 'ddialliance.org/Specification/DDI-CDI' in result

def test_variable_types():
    """Verify variables use InstanceVariable + RepresentedVariable."""
    result = generate_cdi(test_csv_path)
    assert 'InstanceVariable' in result
    assert 'RepresentedVariable' in result
```

#### Angular (ddi-cdi.component.spec.ts)

```typescript
it('should load cdi-viewer in iframe', () => {
  component.generatedDdiCdi = mockJsonLd;
  fixture.detectChanges();
  const iframe = fixture.nativeElement.querySelector('iframe.cdi-viewer-iframe');
  expect(iframe).toBeTruthy();
});

it('should send postMessage on iframe load', () => {
  spyOn(window, 'postMessage');
  component.onCdiViewerLoad();
  expect(window.postMessage).toHaveBeenCalled();
});
```

### 6.2 Integration Tests

1. **End-to-end generation flow**:
   - Upload CSV to Dataverse
   - Trigger DDI-CDI generation
   - Verify output is JSON-LD with correct MIME type
   - Verify cdi-viewer can load and display the output

2. **SHACL validation**:
   - Load generated JSON-LD in cdi-viewer
   - Verify validation against official DDI-CDI shapes passes

3. **Save flow**:
   - Edit metadata in cdi-viewer
   - Save back to Dataverse
   - Verify file is saved with `.jsonld` extension and correct MIME type

### 6.3 Manual Testing Checklist

- [ ] Generate DDI-CDI for CSV file
- [ ] Verify output is `.jsonld` file
- [ ] Open in cdi-viewer standalone
- [ ] Verify no SHACL validation errors
- [ ] Edit a field in cdi-viewer
- [ ] Save back to Dataverse
- [ ] Download and verify JSON-LD structure

---

## 7. Migration Checklist

### Pre-Migration

- [ ] Review DDI-CDI 1.0 specification for type names and structure
- [ ] Study cdi-viewer/examples/cdi/SimpleSample.jsonld as reference
- [ ] Ensure cdi-viewer is deployed and accessible
- [ ] Backup current cdi_generator.py

### Phase 1: Generator (cdi_generator.py)

- [ ] Fix namespace: remove `www.` prefix
- [ ] Add JSON-LD context constant
- [ ] Update serialization to JSON-LD format
- [ ] Map types to official DDI-CDI types
- [ ] Restructure graph to match DDI-CDI model
- [ ] Update CLI arguments
- [ ] Update tests
- [ ] Run test suite: `make test-python`

### Phase 2: Backend (Go)

- [ ] Update output file extension to `.jsonld`
- [ ] Update MIME type to `application/ld+json`
- [ ] Remove `/api/frontend/shacl` endpoint
- [ ] Delete `default_shacl_shapes.ttl`
- [ ] Delete `shacl.go`
- [ ] Update `http_server.go` routes
- [ ] Run tests: `make test`

### Phase 3: Frontend (Angular)

- [ ] Remove shacl-form-patch.ts
- [ ] Remove shacl-form imports
- [ ] Add iframe for cdi-viewer
- [ ] Implement postMessage communication
- [ ] Add iframe styles
- [ ] Remove getShaclTemplate() from data.service.ts
- [ ] Update component tests
- [ ] Run tests: `npm test`

### Phase 4: cdi-viewer Updates

- [ ] Add postMessage handler in core.js
- [ ] Add embedded mode detection
- [ ] Add parent notification on save
- [ ] Test embedded mode
- [ ] Deploy updated cdi-viewer

### Phase 5: External Tool

- [ ] Create 04-cdi-viewer.json
- [ ] Register with Dataverse
- [ ] Test file preview/explore

### Post-Migration

- [ ] Full end-to-end test
- [ ] Update documentation (ddi-cdi.md)
- [ ] Update README if needed
- [ ] Remove old test files referencing Turtle format
- [ ] Announce changes to users

---

## Appendix A: DDI-CDI Type Reference

### Core Types to Use

| Type | Usage |
|------|-------|
| `cdi:WideDataSet` | Root dataset (wide format) |
| `cdi:LongDataSet` | Root dataset (long format) |
| `cdi:WideDataStructure` | Structure for wide dataset |
| `cdi:LongDataStructure` | Structure for long dataset |
| `cdi:LogicalRecord` | Logical record layout |
| `cdi:InstanceVariable` | Variable instance |
| `cdi:RepresentedVariable` | Variable with value domain |
| `cdi:SubstantiveValueDomain` | Data type specification |
| `cdi:IdentifierComponent` | Identifier/key variable |
| `cdi:MeasureComponent` | Measure variable |
| `cdi:AttributeComponent` | Attribute variable |
| `cdi:PrimaryKey` | Primary key definition |
| `cdi:PhysicalSegmentLayout` | Physical file layout |
| `cdi:ValueMapping` | Variable to column mapping |

### DDI Data Type URIs

| XSD Type | DDI CV URI |
|----------|-----------|
| `xsd:string` | `http://rdf-vocabulary.ddialliance.org/cv/DataType/1.1.2/#String` |
| `xsd:integer` | `http://rdf-vocabulary.ddialliance.org/cv/DataType/1.1.2/#Integer` |
| `xsd:decimal` / `xsd:double` | `http://rdf-vocabulary.ddialliance.org/cv/DataType/1.1.2/#Double` |
| `xsd:date` | `http://rdf-vocabulary.ddialliance.org/cv/DataType/1.1.2/#Date` |
| `xsd:dateTime` | `http://rdf-vocabulary.ddialliance.org/cv/DataType/1.1.2/#DateTime` |
| `xsd:boolean` | `http://rdf-vocabulary.ddialliance.org/cv/DataType/1.1.2/#Boolean` |

---

## Appendix B: JSON-LD Output Example

```json
{
  "@context": "https://ddi-cdi.github.io/m2t-ng/DDI-CDI_1-0/encoding/json-ld/ddi-cdi.jsonld",
  "@graph": [
    {
      "@id": "#My_Dataset",
      "@type": "WideDataSet",
      "name": "My Dataset",
      "isStructuredBy": "#datastructure"
    },
    {
      "@id": "#datastructure",
      "@type": "WideDataStructure",
      "has": ["#id_component", "#value_component"]
    },
    {
      "@id": "#id_variable",
      "@type": ["InstanceVariable", "RepresentedVariable"],
      "name": "ID",
      "takesSubstantiveValuesFrom": "#id_domain"
    },
    {
      "@id": "#id_domain",
      "@type": "SubstantiveValueDomain",
      "recommendedDataType": "http://rdf-vocabulary.ddialliance.org/cv/DataType/1.1.2/#String"
    },
    {
      "@id": "#id_component",
      "@type": "IdentifierComponent",
      "isDefinedBy": "#id_variable"
    },
    {
      "@id": "#value_variable",
      "@type": ["InstanceVariable", "RepresentedVariable"],
      "name": "Value",
      "takesSubstantiveValuesFrom": "#value_domain"
    },
    {
      "@id": "#value_domain",
      "@type": "SubstantiveValueDomain",
      "recommendedDataType": "http://rdf-vocabulary.ddialliance.org/cv/DataType/1.1.2/#Double"
    },
    {
      "@id": "#value_component",
      "@type": "MeasureComponent",
      "isDefinedBy": "#value_variable"
    }
  ]
}
```

---

## Appendix C: References

- [DDI-CDI 1.0 Specification](https://ddialliance.org/ddi-cdi)
- [DDI-CDI JSON-LD Context](https://ddi-cdi.github.io/m2t-ng/DDI-CDI_1-0/encoding/json-ld/ddi-cdi.jsonld)
- [DDI-CDI SHACL Shapes](https://ddi-cdi.github.io/m2t-ng/DDI-CDI_1-0/encoding/shacl/ddi-cdi.shacl.ttl)
- [cdi-viewer Repository](https://github.com/libis/cdi-viewer)
- [cdi-viewer Live Demo](https://libis.github.io/cdi-viewer/)
- [JSON-LD Specification](https://json-ld.org/)
