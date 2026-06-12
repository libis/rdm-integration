// Author: Eryk Kulikowski @ KU Leuven (2026). Apache 2.0 License

package redcap2

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Mime types of the generated metadata sidecars.
//
//   - The RO-Crate mime matches Dataverse's own filename-based detection for
//     ro-crate-metadata.json (Dataverse 6.3+) and the contentType the RO-Crate
//     previewer registers for.
//   - The DDI-CDI mime must stay in sync with common.DdiCdiMimeType and the
//     contentType in conf/dataverse/external-tools/04-cdi-previewer.json.
//   - Croissant is JSON-LD; the bare application/ld+json type lets a generic
//     JSON-LD previewer (conf/dataverse/external-tools/06-jsonld-previewer.json)
//     pick it up. There is no Croissant-specific previewer or mime convention
//     (the Croissant 1.0 spec defines no media type).
const (
	roCrateMimeType   = `application/ld+json; profile="http://www.w3.org/ns/json-ld#flattened http://www.w3.org/ns/json-ld#compacted https://w3id.org/ro/crate"`
	ddiCdiMimeType    = `application/ld+json;profile="http://www.w3.org/ns/json-ld#flattened http://www.w3.org/ns/json-ld#compacted https://ddialliance.org/specification/ddi-cdi/1.0"`
	croissantMimeType = "application/ld+json"
)

// ddiDataTypeCV is the DDI controlled vocabulary used for variable data types,
// matching the in-repo DDI-CDI generator (cdi_generator_jsonld.py).
const ddiDataTypeCV = "http://rdf-vocabulary.ddialliance.org/cv/DataType/1.1.2/#"

// choiceCode is one parsed entry of a REDCap select_choices definition.
type choiceCode struct {
	Code  string
	Label string
}

// sidecarVariable describes one physical column of the exported data file,
// enriched with data-dictionary information where the column maps to a field.
type sidecarVariable struct {
	Column     string // physical column name (or JSON key)
	Field      string // dictionary field name ("" for pseudo-columns)
	Label      string
	FieldType  string // REDCap field type ("" for pseudo-columns)
	Validation string
	MinValue   string // text_validation_min from the dictionary
	MaxValue   string // text_validation_max from the dictionary
	Identifier bool
	IsRecordID bool
	Transform  string // applied anonymization mode ("" if none)
	Choices    []choiceCode
}

// sidecarFile describes one generated file of the export bundle.
type sidecarFile struct {
	Name           string // file name within the bundle folder
	Description    string
	EncodingFormat string
	MD5            string
	Size           int64
}

// sidecarModel is the normalized metadata model all three exporters render.
type sidecarModel struct {
	ProjectID      interface{}
	ProjectTitle   string
	RedcapVersion  string
	GeneratedAt    string
	ExportMode     string
	ReportID       string
	DataFileName   string
	DataFormat     string // csv | json
	Delimiter      string // "," or "\t" (csv only)
	IsEAV          bool
	Files          []sidecarFile
	Variables      []sidecarVariable
	KeyFingerprint string
}

// parseChoiceCodes parses a REDCap select_choices definition
// ("1, Male | 2, Female") into code/label pairs.
func parseChoiceCodes(raw string) []choiceCode {
	res := []choiceCode{}
	for _, part := range strings.Split(raw, "|") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		code := part
		label := ""
		if i := strings.Index(part, ","); i >= 0 {
			code = strings.TrimSpace(part[:i])
			label = strings.TrimSpace(part[i+1:])
		}
		if code == "" {
			continue
		}
		res = append(res, choiceCode{Code: code, Label: label})
	}
	return res
}

// variableChoices returns the parsed code list for choice-type fields only
// (the same dictionary column holds calculations for calc fields).
func variableChoices(dict dictionary, field string) []choiceCode {
	switch dict.fieldType[field] {
	case "radio", "dropdown", "checkbox":
		return parseChoiceCodes(dict.choices[field])
	}
	return nil
}

// dataFileColumns extracts the physical column names (or JSON keys) of the
// processed data file.
func dataFileColumns(data []byte, opts pluginOptions) []string {
	if opts.DataFormat == "csv" {
		rows, err := parseCSV(data, reportDelimiter(opts))
		if err != nil || len(rows) == 0 {
			return nil
		}
		return rows[0]
	}
	rows := make([]map[string]interface{}, 0)
	if err := json.Unmarshal(data, &rows); err != nil {
		return nil
	}
	seen := map[string]bool{}
	keys := []string{}
	for _, row := range rows {
		for k := range row {
			if !seen[k] {
				seen[k] = true
				keys = append(keys, k)
			}
		}
	}
	sort.Strings(keys)
	return keys
}

// buildSidecarVariables maps physical columns to dictionary-enriched variable
// descriptions. Columns that do not resolve to a dictionary field (record,
// redcap_event_name, ...) are kept as pseudo-columns.
func buildSidecarVariables(columns []string, opts pluginOptions, plan transformPlan, dict dictionary) []sidecarVariable {
	labelHeaders := headersAreLabels(opts)
	recordField := recordIDField(dict)
	vars := make([]sidecarVariable, 0, len(columns))
	for _, col := range columns {
		v := sidecarVariable{Column: col}
		for _, candidate := range resolveHeaderFields(col, labelHeaders, dict) {
			base := baseFieldName(candidate)
			if _, ok := dict.fieldType[base]; ok {
				v.Field = base
				break
			}
		}
		if v.Field != "" {
			v.Label = dict.fieldLabel[v.Field]
			v.FieldType = dict.fieldType[v.Field]
			v.Validation = dict.validation[v.Field]
			v.MinValue = dict.validationMin[v.Field]
			v.MaxValue = dict.validationMax[v.Field]
			v.Identifier = dict.identifier[v.Field]
			v.IsRecordID = v.Field == recordField
			v.Choices = variableChoices(dict, v.Field)
			v.Transform = plan.modes[v.Field]
			if t := plan.modes[baseFieldName(col)]; t != "" {
				v.Transform = t
			}
		} else if strings.EqualFold(strings.TrimSpace(col), "record") {
			// The EAV linking column carries record-ID values.
			v.IsRecordID = true
			v.Transform = plan.modes[recordField]
		}
		vars = append(vars, v)
	}
	return vars
}

// buildSidecarModel assembles the normalized model from the generated bundle
// context. files maps bundle paths to contents; only files under basePath are
// described.
func buildSidecarModel(opts pluginOptions, plan transformPlan, dict dictionary, basePath string, files map[string][]byte, dataPath, redcapVersion string, projectID interface{}, projectTitle string) sidecarModel {
	model := sidecarModel{
		ProjectID:      projectID,
		ProjectTitle:   projectTitle,
		RedcapVersion:  redcapVersion,
		GeneratedAt:    opts.GeneratedAt,
		ExportMode:     opts.ExportMode,
		ReportID:       opts.ReportID,
		DataFormat:     opts.DataFormat,
		Delimiter:      opts.CsvDelimiter,
		IsEAV:          isEAV(opts),
		KeyFingerprint: plan.keyFingerprint,
	}

	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	for _, path := range paths {
		name := strings.TrimPrefix(path, basePath+"/")
		model.Files = append(model.Files, sidecarFile{
			Name:           name,
			Description:    bundleFileDescription(name),
			EncodingFormat: bundleFileEncodingFormat(name, opts),
			MD5:            md5Hex(files[path]),
			Size:           int64(len(files[path])),
		})
	}

	model.DataFileName = strings.TrimPrefix(dataPath, basePath+"/")
	columns := dataFileColumns(files[dataPath], opts)
	model.Variables = buildSidecarVariables(columns, opts, plan, dict)
	return model
}

func bundleFileDescription(name string) string {
	switch name {
	case "data.csv", "data.json":
		return "Exported REDCap records"
	case "metadata.csv":
		return "REDCap data dictionary (exported fields)"
	case "project_info.json":
		return "REDCap project information"
	case "events.csv":
		return "REDCap events (longitudinal projects)"
	case "form_event_mapping.csv":
		return "REDCap form-event mapping (longitudinal projects)"
	case "manifest.json":
		return "Export manifest (parameters, anonymization audit, provenance)"
	case "project_metadata.xml":
		return "CDISC ODM project metadata (metadata only, no data)"
	}
	return ""
}

func bundleFileEncodingFormat(name string, opts pluginOptions) string {
	switch {
	case name == "data.csv" && opts.CsvDelimiter == "\t":
		return "text/tab-separated-values"
	case strings.HasSuffix(name, ".csv"):
		return "text/csv"
	case strings.HasSuffix(name, ".json"):
		return "application/json"
	case strings.HasSuffix(name, ".xml"):
		return "text/xml"
	}
	return "application/octet-stream"
}

// publishedDate returns the export timestamp, or false when only the
// missing-timestamp sentinel is available (datePublished must be ISO 8601).
func (m sidecarModel) publishedDate() (string, bool) {
	if m.GeneratedAt == "" || m.GeneratedAt == "missing-generated-at" {
		return "", false
	}
	return m.GeneratedAt, true
}

// datasetName returns a human-readable dataset name for the sidecars.
func (m sidecarModel) datasetName() string {
	if m.ProjectTitle != "" {
		return m.ProjectTitle
	}
	if m.ExportMode == "report" {
		return "REDCap report " + m.ReportID
	}
	return "REDCap records export"
}

func (m sidecarModel) datasetDescription() string {
	scope := "all records"
	if m.ExportMode == "report" {
		scope = fmt.Sprintf("report %s", m.ReportID)
	}
	desc := fmt.Sprintf("Export of %s from REDCap project %q", scope, m.datasetName())
	if m.RedcapVersion != "" {
		desc += fmt.Sprintf(" (REDCap %s)", m.RedcapVersion)
	}
	desc += ", generated by the rdm-integration redcap2 plugin."
	if m.KeyFingerprint != "" {
		desc += " Some variables are pseudonymized with HMAC-SHA256 (key fingerprint " + m.KeyFingerprint + ")."
	}
	return desc
}

// variableDescription combines the dictionary label and transform note.
func variableDescription(v sidecarVariable) string {
	parts := []string{}
	if v.Label != "" && v.Label != v.Column {
		parts = append(parts, v.Label)
	}
	if v.Transform != "" {
		parts = append(parts, fmt.Sprintf("anonymization applied: %s", v.Transform))
	}
	return strings.Join(parts, " — ")
}

// numericBound parses a REDCap validation min/max into a JSON number.
// Non-numeric bounds (e.g. date limits) are skipped: schema.org
// minValue/maxValue expect numbers.
func numericBound(raw string) (float64, bool) {
	f, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	return f, err == nil
}

// propertyValueBase renders the shared part of a schema.org PropertyValue
// for one variable, following the CDIF 1.1 Discovery profile shape for
// variableMeasured (name + description required, alternateName, min/max).
// Code lists are attached by the caller (inline DefinedTerms for Croissant,
// flattened references for RO-Crate).
func propertyValueBase(v sidecarVariable) map[string]interface{} {
	pv := map[string]interface{}{
		"@type": "PropertyValue",
		"name":  v.Column,
	}
	if desc := variableDescription(v); desc != "" {
		pv["description"] = desc
	}
	if v.Label != "" && v.Label != v.Column {
		pv["alternateName"] = v.Label
	}
	if f, ok := numericBound(v.MinValue); ok {
		pv["minValue"] = f
	}
	if f, ok := numericBound(v.MaxValue); ok {
		pv["maxValue"] = f
	}
	return pv
}

// definedTermFor renders one code-list entry as a schema.org DefinedTerm
// (termCode = the value in the data, name = the human label).
func definedTermFor(c choiceCode) map[string]interface{} {
	term := map[string]interface{}{
		"@type":    "DefinedTerm",
		"termCode": c.Code,
	}
	if c.Label != "" {
		term["name"] = c.Label
	}
	return term
}

// --- Croissant 1.0 ---

// croissantContext is the canonical Croissant 1.0 @context.
var croissantContext = map[string]interface{}{
	"@language":          "en",
	"@vocab":             "https://schema.org/",
	"citeAs":             "cr:citeAs",
	"column":             "cr:column",
	"conformsTo":         "dct:conformsTo",
	"cr":                 "http://mlcommons.org/croissant/",
	"rai":                "http://mlcommons.org/croissant/RAI/",
	"data":               map[string]interface{}{"@id": "cr:data", "@type": "@json"},
	"dataType":           map[string]interface{}{"@id": "cr:dataType", "@type": "@vocab"},
	"dct":                "http://purl.org/dc/terms/",
	"equivalentProperty": "cr:equivalentProperty",
	"examples":           map[string]interface{}{"@id": "cr:examples", "@type": "@json"},
	"extract":            "cr:extract",
	"field":              "cr:field",
	"fileProperty":       "cr:fileProperty",
	"fileObject":         "cr:fileObject",
	"fileSet":            "cr:fileSet",
	"format":             "cr:format",
	"includes":           "cr:includes",
	"isLiveDataset":      "cr:isLiveDataset",
	"jsonPath":           "cr:jsonPath",
	"key":                "cr:key",
	"md5":                "cr:md5",
	"parentField":        "cr:parentField",
	"path":               "cr:path",
	"recordSet":          "cr:recordSet",
	"references":         "cr:references",
	"regex":              "cr:regex",
	"repeated":           "cr:repeated",
	"replace":            "cr:replace",
	"samplingRate":       "cr:samplingRate",
	"sc":                 "https://schema.org/",
	"separator":          "cr:separator",
	"source":             "cr:source",
	"subField":           "cr:subField",
	"transform":          "cr:transform",
}

func croissantDataType(v sidecarVariable) string {
	switch v.FieldType {
	case "yesno", "truefalse":
		return "sc:Boolean"
	}
	switch {
	case v.Validation == "integer":
		return "sc:Integer"
	case v.Validation == "number" || strings.HasPrefix(v.Validation, "number_"):
		return "sc:Float"
	case strings.HasPrefix(v.Validation, "date_"):
		return "sc:Date"
	case strings.HasPrefix(v.Validation, "datetime_"):
		return "sc:Date"
	}
	return "sc:Text"
}

// buildCroissant renders the Croissant 1.0 metadata file. The record set is
// only emitted for CSV exports: Croissant column extraction is defined for
// delimited files.
func buildCroissant(m sidecarModel) ([]byte, error) {
	distribution := make([]interface{}, 0, len(m.Files))
	for _, f := range m.Files {
		distribution = append(distribution, map[string]interface{}{
			"@type":          "cr:FileObject",
			"@id":            f.Name,
			"name":           f.Name,
			"description":    f.Description,
			"contentUrl":     f.Name,
			"encodingFormat": f.EncodingFormat,
			"md5":            f.MD5,
		})
	}

	doc := map[string]interface{}{
		"@context":     croissantContext,
		"@type":        "sc:Dataset",
		"conformsTo":   "http://mlcommons.org/croissant/1.0",
		"name":         m.datasetName(),
		"description":  m.datasetDescription(),
		"version":      "1.0.0",
		"distribution": distribution,
	}
	if date, ok := m.publishedDate(); ok {
		doc["datePublished"] = date
	}

	// Variable-level metadata as schema.org variableMeasured, following the
	// CDIF 1.1 Discovery profile shape (Croissant's @vocab is schema.org, so
	// the terms expand to the right IRIs; mlcroissant accepts them). Code
	// lists are inline DefinedTerms via valueReference.
	if len(m.Variables) > 0 {
		variableMeasured := make([]interface{}, 0, len(m.Variables))
		for _, v := range m.Variables {
			pv := propertyValueBase(v)
			pv["@id"] = "variable/" + v.Column
			if len(v.Choices) > 0 {
				terms := make([]interface{}, 0, len(v.Choices))
				for _, c := range v.Choices {
					terms = append(terms, definedTermFor(c))
				}
				pv["valueReference"] = terms
			}
			variableMeasured = append(variableMeasured, pv)
		}
		doc["variableMeasured"] = variableMeasured
	}

	if m.DataFormat == "csv" && len(m.Variables) > 0 {
		fields := make([]interface{}, 0, len(m.Variables))
		for _, v := range m.Variables {
			field := map[string]interface{}{
				"@type":    "cr:Field",
				"@id":      "records/" + v.Column,
				"name":     v.Column,
				"dataType": croissantDataType(v),
				"source": map[string]interface{}{
					"fileObject": map[string]interface{}{"@id": m.DataFileName},
					"extract":    map[string]interface{}{"column": v.Column},
				},
			}
			if desc := variableDescription(v); desc != "" {
				field["description"] = desc
			}
			fields = append(fields, field)
		}
		doc["recordSet"] = []interface{}{
			map[string]interface{}{
				"@type": "cr:RecordSet",
				"@id":   "records",
				"name":  "records",
				"field": fields,
			},
		}
	}

	return json.MarshalIndent(doc, "", "  ")
}

// --- RO-Crate 1.2 ---

// buildROCrate renders a detached RO-Crate 1.2 metadata file describing the
// bundle folder, with Process Run Crate style provenance (a CreateAction with
// the plugin as instrument).
func buildROCrate(m sidecarModel) ([]byte, error) {
	hasPart := make([]interface{}, 0, len(m.Files))
	results := make([]interface{}, 0, len(m.Files))
	graph := []interface{}{}

	graph = append(graph, map[string]interface{}{
		"@id":         "ro-crate-metadata.json",
		"@type":       "CreativeWork",
		"conformsTo":  map[string]interface{}{"@id": "https://w3id.org/ro/crate/1.2"},
		"about":       map[string]interface{}{"@id": "./"},
		"description": "RO-Crate metadata for a REDCap export generated by the rdm-integration redcap2 plugin",
	})

	for _, f := range m.Files {
		hasPart = append(hasPart, map[string]interface{}{"@id": f.Name})
		results = append(results, map[string]interface{}{"@id": f.Name})
	}

	rootDataset := map[string]interface{}{
		"@id":         "./",
		"@type":       "Dataset",
		"name":        m.datasetName(),
		"description": m.datasetDescription(),
		"hasPart":     hasPart,
		"mentions":    map[string]interface{}{"@id": "#export-action"},
	}
	if date, ok := m.publishedDate(); ok {
		rootDataset["datePublished"] = date
	}
	if m.ProjectID != nil {
		rootDataset["identifier"] = fmt.Sprintf("redcap-project-%v", m.ProjectID)
	}

	// Variable-level metadata as schema.org variableMeasured contextual
	// entities (CDIF 1.1 Discovery profile shape). RO-Crate JSON-LD is
	// flattened: every PropertyValue and DefinedTerm is its own graph entity.
	if len(m.Variables) > 0 {
		variableRefs := make([]interface{}, 0, len(m.Variables))
		for _, v := range m.Variables {
			variableID := "#variable/" + v.Column
			variableRefs = append(variableRefs, map[string]interface{}{"@id": variableID})
			pv := propertyValueBase(v)
			pv["@id"] = variableID
			if len(v.Choices) > 0 {
				termRefs := make([]interface{}, 0, len(v.Choices))
				for _, c := range v.Choices {
					termID := variableID + "/code/" + safeFragment(c.Code)
					termRefs = append(termRefs, map[string]interface{}{"@id": termID})
					term := definedTermFor(c)
					term["@id"] = termID
					graph = append(graph, term)
				}
				pv["valueReference"] = termRefs
			}
			graph = append(graph, pv)
		}
		rootDataset["variableMeasured"] = variableRefs
	}
	graph = append(graph, rootDataset)

	for _, f := range m.Files {
		fileNode := map[string]interface{}{
			"@id":            f.Name,
			"@type":          "File",
			"name":           f.Name,
			"encodingFormat": f.EncodingFormat,
			"contentSize":    fmt.Sprint(f.Size),
			"md5":            f.MD5,
		}
		if f.Description != "" {
			fileNode["description"] = f.Description
		}
		graph = append(graph, fileNode)
	}

	// Process Run Crate provenance: the export run and its instruments.
	action := map[string]interface{}{
		"@id":        "#export-action",
		"@type":      "CreateAction",
		"name":       "REDCap export",
		"instrument": map[string]interface{}{"@id": "#rdm-integration-redcap2"},
		"result":     results,
		"description": fmt.Sprintf(
			"Files generated from the REDCap API (export mode: %s) with client-side anonymization applied as documented in manifest.json",
			m.ExportMode),
	}
	if date, ok := m.publishedDate(); ok {
		action["endTime"] = date
	}
	graph = append(graph, action)
	graph = append(graph, map[string]interface{}{
		"@id":   "#rdm-integration-redcap2",
		"@type": "SoftwareApplication",
		"name":  "rdm-integration redcap2 plugin",
		"url":   "https://github.com/libis/rdm-integration",
	})
	if m.RedcapVersion != "" {
		action["object"] = map[string]interface{}{"@id": "#redcap"}
		graph = append(graph, map[string]interface{}{
			"@id":             "#redcap",
			"@type":           "SoftwareApplication",
			"name":            "REDCap",
			"softwareVersion": m.RedcapVersion,
		})
	}

	doc := map[string]interface{}{
		"@context": "https://w3id.org/ro/crate/1.2/context",
		"@graph":   graph,
	}
	return json.MarshalIndent(doc, "", "  ")
}

// --- DDI-CDI 1.0 ---

// ddiCdiContext is the DDI-CDI 1.0 JSON-LD context published on the DDI
// Alliance documentation site — the released encoding. (The previously used
// ddi-cdi.github.io/m2t-ng URL is a build-tooling Pages artifact and currently
// serves invalid JSON with unresolved merge-conflict markers.) The output
// validates against the official DDI-CDI 1.0 SHACL shapes used by the
// cdi-viewer previewer.
const ddiCdiContext = "https://docs.ddialliance.org/DDI-CDI/1.0/model/encoding/json-ld/ddi-cdi.jsonld"

func ddiCdiDataType(v sidecarVariable) string {
	switch v.FieldType {
	case "yesno", "truefalse":
		return ddiDataTypeCV + "Boolean"
	}
	switch {
	case v.Validation == "integer":
		return ddiDataTypeCV + "Integer"
	case v.Validation == "number" || strings.HasPrefix(v.Validation, "number_"):
		return ddiDataTypeCV + "Double"
	case strings.HasPrefix(v.Validation, "datetime_"):
		return ddiDataTypeCV + "DateTime"
	case strings.HasPrefix(v.Validation, "date_"):
		return ddiDataTypeCV + "Date"
	}
	return ddiDataTypeCV + "String"
}

func ddiCdiComponentType(v sidecarVariable) string {
	switch {
	case v.IsRecordID:
		return "IdentifierComponent"
	case len(v.Choices) > 0 || v.FieldType == "yesno" || v.FieldType == "truefalse":
		return "DimensionComponent"
	case v.Validation == "integer" || v.Validation == "number" || strings.HasPrefix(v.Validation, "number_"):
		return "MeasureComponent"
	}
	return "AttributeComponent"
}

// safeFragment converts a column name into a JSON-LD fragment identifier.
func safeFragment(name string) string {
	var b strings.Builder
	for _, r := range name {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	if b.Len() == 0 {
		return "_"
	}
	return b.String()
}

// buildDDICDI renders a DDI-CDI 1.0 JSON-LD description of the data file,
// mirroring the structure of the in-repo generator: WideDataSet,
// WideDataStructure, LogicalRecord, InstanceVariables with value domains and
// code lists, and (for CSV) a PhysicalSegmentLayout with value mappings.
func buildDDICDI(m sidecarModel) ([]byte, error) {
	graph := []interface{}{}
	componentIDs := []interface{}{}
	variableIDs := []interface{}{}
	valueMappings := []interface{}{}
	valueMappingPositions := []interface{}{}
	primaryKeyComponent := ""

	used := map[string]int{}
	position := 1
	for _, v := range m.Variables {
		frag := safeFragment(v.Column)
		if n, ok := used[frag]; ok {
			used[frag] = n + 1
			frag = fmt.Sprintf("%s_%d", frag, n+1)
		} else {
			used[frag] = 0
		}
		varID := "#" + frag
		domainID := "#" + frag + "_Substantive_Value_Domain"
		componentID := "#" + frag + "_Component"
		mappingID := "#valueMapping_" + frag
		mappingPosID := "#ValueMappingPosition_" + frag

		variableIDs = append(variableIDs, varID)
		componentIDs = append(componentIDs, componentID)

		// Code list from the REDCap choices definition. Per the DDI-CDI 1.0
		// model (and its official SHACL shapes), a Code carries no literal
		// value itself: it uses a Notation whose TypedString content is the
		// value as it appears in the data, and denotes a Category that holds
		// the human-readable label.
		codeListID := ""
		if len(v.Choices) > 0 {
			codeListID = "#" + frag + "_CodeList"
			codeIDs := []interface{}{}
			for _, c := range v.Choices {
				codeFrag := frag + "_" + safeFragment(c.Code)
				codeID := "#" + codeFrag + "_Code"
				categoryID := "#" + codeFrag + "_Category"
				notationID := "#" + codeFrag + "_Notation"
				codeIDs = append(codeIDs, codeID)

				categoryName := c.Label
				if categoryName == "" {
					categoryName = c.Code
				}
				graph = append(graph, map[string]interface{}{
					"@id":   categoryID,
					"@type": "Category",
					"name": map[string]interface{}{
						"@type": "ObjectName",
						"name":  categoryName,
					},
				})
				graph = append(graph, map[string]interface{}{
					"@id":   notationID,
					"@type": "Notation",
					"content": map[string]interface{}{
						"@type":   "TypedString",
						"content": c.Code,
					},
					"represents": categoryID,
				})
				graph = append(graph, map[string]interface{}{
					"@id":           codeID,
					"@type":         "Code",
					"denotes":       categoryID,
					"uses_Notation": notationID,
				})
			}
			label := v.Label
			if label == "" {
				label = v.Column
			}
			graph = append(graph, map[string]interface{}{
				"@id":   codeListID,
				"@type": "CodeList",
				"name": map[string]interface{}{
					"@type": "ObjectName",
					"name":  label + " codes",
				},
				"allowsDuplicates": false,
				"has_Code":         codeIDs,
			})
		}

		dataType := ddiCdiDataType(v)
		domainNode := map[string]interface{}{
			"@id":   domainID,
			"@type": "SubstantiveValueDomain",
			"recommendedDataType": map[string]interface{}{
				"@type":      "ControlledVocabularyEntry",
				"entryValue": strings.TrimPrefix(dataType, ddiDataTypeCV),
				"vocabulary": map[string]interface{}{
					"@type": "Reference",
					"uri":   ddiDataTypeCV,
				},
			},
		}
		if codeListID != "" {
			domainNode["takesValuesFrom"] = codeListID
		}
		graph = append(graph, domainNode)

		name := v.Label
		if name == "" {
			name = v.Column
		}
		varNode := map[string]interface{}{
			"@id":   varID,
			"@type": "InstanceVariable",
			"name": map[string]interface{}{
				"@type": "ObjectName",
				"name":  name,
			},
			"takesSubstantiveValuesFrom_SubstantiveValueDomain": domainID,
		}
		definition := "Column: " + v.Column
		if v.Transform != "" {
			definition += " (anonymization applied: " + v.Transform + ")"
		}
		varNode["definition"] = map[string]interface{}{
			"@type": "InternationalString",
			"languageSpecificString": map[string]interface{}{
				"@type":   "LanguageString",
				"content": definition,
			},
		}
		if m.DataFormat == "csv" {
			varNode["has_ValueMapping"] = mappingID
			valueMappings = append(valueMappings, mappingID)
			valueMappingPositions = append(valueMappingPositions, mappingPosID)
			graph = append(graph, map[string]interface{}{
				"@id":          mappingID,
				"@type":        "ValueMapping",
				"defaultValue": "",
			})
			graph = append(graph, map[string]interface{}{
				"@id":     mappingPosID,
				"@type":   "ValueMappingPosition",
				"indexes": mappingID,
				"value":   position,
			})
			position++
		}
		graph = append(graph, varNode)

		componentType := ddiCdiComponentType(v)
		if componentType == "IdentifierComponent" && primaryKeyComponent == "" {
			primaryKeyComponent = componentID
		}
		graph = append(graph, map[string]interface{}{
			"@id":                             componentID,
			"@type":                           componentType,
			"isDefinedBy_RepresentedVariable": varID,
		})
	}

	datasetID := "#" + safeFragment(m.datasetName())
	graph = append(graph, map[string]interface{}{
		"@id":            datasetID,
		"@type":          "WideDataSet",
		"isStructuredBy": "#datastructure",
	})
	structure := map[string]interface{}{
		"@id":                        "#datastructure",
		"@type":                      "WideDataStructure",
		"has_DataStructureComponent": componentIDs,
	}
	if primaryKeyComponent != "" {
		// The SHACL shapes require the primary key to be reachable from the
		// data structure (DataStructure_has_PrimaryKey).
		structure["has_PrimaryKey"] = "#primaryKey"
	}
	graph = append(graph, structure)
	graph = append(graph, map[string]interface{}{
		"@id":                  "#logicalRecord",
		"@type":                "LogicalRecord",
		"organizes":            datasetID,
		"has_InstanceVariable": variableIDs,
	})
	if primaryKeyComponent != "" {
		graph = append(graph, map[string]interface{}{
			"@id":          "#primaryKey",
			"@type":        "PrimaryKey",
			"isComposedOf": "#primaryKeyComponent",
		})
		graph = append(graph, map[string]interface{}{
			"@id":                                  "#primaryKeyComponent",
			"@type":                                "PrimaryKeyComponent",
			"correspondsTo_DataStructureComponent": primaryKeyComponent,
		})
	}
	if m.DataFormat == "csv" {
		delimiter := ","
		if m.Delimiter == "\t" {
			delimiter = "\\t"
		}
		graph = append(graph, map[string]interface{}{
			"@id":                      "#physicalSegmentLayout",
			"@type":                    "PhysicalSegmentLayout",
			"formats":                  "#logicalRecord",
			"allowsDuplicates":         true,
			"isDelimited":              true,
			"isFixedWidth":             false,
			"hasHeader":                true,
			"headerRowCount":           1,
			"delimiter":                delimiter,
			"has_ValueMapping":         valueMappings,
			"has_ValueMappingPosition": valueMappingPositions,
		})
	}

	doc := map[string]interface{}{
		"@context": ddiCdiContext,
		"@graph":   graph,
	}
	return json.MarshalIndent(doc, "", "  ")
}

// addSidecars generates the three metadata sidecars from the normalized model
// and registers them (with explicit mime types) in the bundle file map.
// Sidecar generation must never fail the export: errors come back as warnings.
func addSidecars(m sidecarModel, basePath string, files map[string][]byte, mime map[string]string) []string {
	warnings := []string{}

	if data, err := buildCroissant(m); err != nil {
		warnings = append(warnings, fmt.Sprintf("croissant generation failed: %v", err))
	} else {
		path := basePath + "/croissant.json"
		files[path] = data
		mime[path] = croissantMimeType
	}

	if data, err := buildROCrate(m); err != nil {
		warnings = append(warnings, fmt.Sprintf("ro-crate generation failed: %v", err))
	} else {
		path := basePath + "/ro-crate-metadata.json"
		files[path] = data
		mime[path] = roCrateMimeType
	}

	if data, err := buildDDICDI(m); err != nil {
		warnings = append(warnings, fmt.Sprintf("ddi-cdi generation failed: %v", err))
	} else {
		path := basePath + "/ddi-cdi.jsonld"
		files[path] = data
		mime[path] = ddiCdiMimeType
	}

	return warnings
}
