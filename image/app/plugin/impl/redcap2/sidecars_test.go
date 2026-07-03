// Author: Eryk Kulikowski @ KU Leuven (2026). Apache 2.0 License

package redcap2

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const sidecarTestMetadataCSV = "field_name,form_name,field_type,field_label,select_choices_or_calculations,identifier,text_validation_type_or_show_slider_number,text_validation_min,text_validation_max\n" +
	"record_id,demographics,text,Record ID,,,,,\n" +
	"name,demographics,text,Full Name,,y,,,\n" +
	"age,demographics,text,Age,,,integer,0,120\n" +
	"weight,demographics,text,Weight,,,number,,\n" +
	"sex,demographics,radio,Sex,\"1, Male | 2, Female\",,,,\n" +
	"consent,demographics,yesno,Consent given,,,,,\n" +
	"visit_date,demographics,text,Visit Date,,,date_ymd,2020-01-01,\n"

func sidecarTestModel(t *testing.T, opts pluginOptions, plan transformPlan, dataCSV string) sidecarModel {
	t.Helper()
	dict := parseDictionary([]byte(sidecarTestMetadataCSV))
	files := map[string][]byte{
		"redcap/records/data.csv":          []byte(dataCSV),
		"redcap/records/metadata.csv":      []byte(sidecarTestMetadataCSV),
		"redcap/records/project_info.json": []byte(`{"project_id":1,"project_title":"Demo"}`),
		"redcap/records/manifest.json":     []byte(`{"plugin":"redcap2"}`),
	}
	return buildSidecarModel(opts, plan, dict, "redcap/records", files, "redcap/records/data.csv", "14.5.5", float64(1), "Demo")
}

func TestParseChoiceCodes(t *testing.T) {
	got := parseChoiceCodes("1, Male | 2, Female |  3, Other, with comma ")
	want := []choiceCode{
		{Code: "1", Label: "Male"},
		{Code: "2", Label: "Female"},
		{Code: "3", Label: "Other, with comma"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("parseChoiceCodes = %+v, want %+v", got, want)
	}
	if got := parseChoiceCodes(""); len(got) != 0 {
		t.Errorf("empty choices should parse to nothing, got %+v", got)
	}
}

func TestBuildSidecarVariables(t *testing.T) {
	opts, _ := parsePluginOptions(`{"exportMode":"records","generatedAt":"2026-06-12T00:00:00Z"}`)
	plan := testPlan(map[string]string{"name": "blank"})
	model := sidecarTestModel(t, opts, plan,
		"record_id,name,age,sex,consent,visit_date\n1,John,34,1,1,2026-01-01\n")

	byColumn := map[string]sidecarVariable{}
	for _, v := range model.Variables {
		byColumn[v.Column] = v
	}
	if !byColumn["record_id"].IsRecordID {
		t.Error("record_id must be flagged as record ID")
	}
	if byColumn["name"].Transform != "blank" || !byColumn["name"].Identifier {
		t.Errorf("name = %+v, want identifier with blank transform", byColumn["name"])
	}
	if len(byColumn["sex"].Choices) != 2 || byColumn["sex"].Choices[0].Label != "Male" {
		t.Errorf("sex choices = %+v", byColumn["sex"].Choices)
	}
	if byColumn["age"].Validation != "integer" {
		t.Errorf("age validation = %q", byColumn["age"].Validation)
	}
	if byColumn["age"].MinValue != "0" || byColumn["age"].MaxValue != "120" {
		t.Errorf("age min/max = %q/%q, want 0/120", byColumn["age"].MinValue, byColumn["age"].MaxValue)
	}
}

func TestBuildCroissant(t *testing.T) {
	opts, _ := parsePluginOptions(`{"exportMode":"records","generatedAt":"2026-06-12T00:00:00Z"}`)
	plan := testPlan(map[string]string{"name": "pseudonymize"})
	plan.keyFingerprint = "abcdef0123456789"
	model := sidecarTestModel(t, opts, plan,
		"record_id,name,age,weight,sex,consent,visit_date\n1,x,34,70.5,1,1,2026-01-01\n")

	data, err := buildCroissant(model)
	if err != nil {
		t.Fatalf("buildCroissant returned error: %v", err)
	}
	doc := map[string]interface{}{}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("croissant.json is invalid JSON: %v", err)
	}
	if doc["conformsTo"] != "http://mlcommons.org/croissant/1.0" {
		t.Errorf("conformsTo = %v", doc["conformsTo"])
	}
	if doc["@type"] != "sc:Dataset" || doc["name"] != "Demo" {
		t.Errorf("type/name = %v/%v", doc["@type"], doc["name"])
	}
	if doc["@id"] != "#dataset" {
		t.Errorf("@id = %v, want #dataset (anonymous roots are unlinkable)", doc["@id"])
	}
	if !strings.Contains(doc["description"].(string), "abcdef0123456789") {
		t.Error("description should mention the pseudonymization key fingerprint")
	}

	distribution := doc["distribution"].([]interface{})
	if len(distribution) != len(model.Files) {
		t.Errorf("distribution has %d entries, want %d", len(distribution), len(model.Files))
	}
	first := distribution[0].(map[string]interface{})
	if first["@type"] != "cr:FileObject" || first["md5"] == "" {
		t.Errorf("distribution entry = %v", first)
	}

	recordSets := doc["recordSet"].([]interface{})
	fields := recordSets[0].(map[string]interface{})["field"].([]interface{})
	dataTypes := map[string]string{}
	for _, f := range fields {
		field := f.(map[string]interface{})
		dataTypes[field["name"].(string)] = field["dataType"].(string)
	}
	want := map[string]string{
		"age":        "sc:Integer",
		"weight":     "sc:Float",
		"consent":    "sc:Boolean",
		"visit_date": "sc:Date",
		"sex":        "sc:Text",
		"name":       "sc:Text",
	}
	for name, wantType := range want {
		if dataTypes[name] != wantType {
			t.Errorf("dataType(%s) = %q, want %q", name, dataTypes[name], wantType)
		}
	}

	// Variable-level metadata (CDIF-style variableMeasured).
	variableMeasured := doc["variableMeasured"].([]interface{})
	byName := map[string]map[string]interface{}{}
	for _, entry := range variableMeasured {
		pv := entry.(map[string]interface{})
		byName[pv["name"].(string)] = pv
	}
	if len(byName) != len(model.Variables) {
		t.Errorf("variableMeasured has %d entries, want %d", len(byName), len(model.Variables))
	}
	// CDIF 1.1 Discovery mandatory dataset-level properties.
	if doc["identifier"] != "redcap-project-1" {
		t.Errorf("identifier = %v, want redcap-project-1", doc["identifier"])
	}
	if doc["dateModified"] != "2026-06-12T00:00:00Z" || doc["datePublished"] != "2026-06-12T00:00:00Z" {
		t.Errorf("dateModified/datePublished = %v/%v", doc["dateModified"], doc["datePublished"])
	}

	age := byName["age"]
	if age["@type"] != "PropertyValue" || age["minValue"] != float64(0) || age["maxValue"] != float64(120) {
		t.Errorf("age variableMeasured = %v", age)
	}
	sex := byName["sex"]
	if sex["alternateName"] != "Sex" {
		t.Errorf("sex alternateName = %v", sex["alternateName"])
	}
	terms := sex["valueReference"].([]interface{})
	if len(terms) != 2 {
		t.Fatalf("sex valueReference = %v, want 2 DefinedTerms", terms)
	}
	firstTerm := terms[0].(map[string]interface{})
	if firstTerm["@type"] != "DefinedTerm" || firstTerm["termCode"] != "1" || firstTerm["name"] != "Male" {
		t.Errorf("sex code term = %v", firstTerm)
	}
	if _, ok := byName["visit_date"]["minValue"]; ok {
		t.Error("non-numeric validation bounds must not become minValue")
	}
	pseudonymized := byName["name"]
	if !strings.Contains(pseudonymized["description"].(string), "pseudonymize") {
		t.Errorf("transform note missing from description: %v", pseudonymized["description"])
	}
}

func TestBuildCroissantJSONExportHasNoRecordSet(t *testing.T) {
	opts, _ := parsePluginOptions(`{"exportMode":"records","dataFormat":"json"}`)
	model := sidecarTestModel(t, opts, transformPlan{}, `[{"record_id":"1","age":"34"}]`)
	model.DataFormat = "json"

	data, err := buildCroissant(model)
	if err != nil {
		t.Fatalf("buildCroissant returned error: %v", err)
	}
	doc := map[string]interface{}{}
	_ = json.Unmarshal(data, &doc)
	if _, ok := doc["recordSet"]; ok {
		t.Error("JSON exports must not declare a CSV-column record set")
	}
}

func TestBuildROCrate(t *testing.T) {
	opts, _ := parsePluginOptions(`{"exportMode":"records","generatedAt":"2026-06-12T00:00:00Z"}`)
	model := sidecarTestModel(t, opts, transformPlan{},
		"record_id,age\n1,34\n")

	data, err := buildROCrate(model)
	if err != nil {
		t.Fatalf("buildROCrate returned error: %v", err)
	}
	doc := map[string]interface{}{}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("ro-crate-metadata.json is invalid JSON: %v", err)
	}
	if doc["@context"] != "https://w3id.org/ro/crate/1.2/context" {
		t.Errorf("@context = %v", doc["@context"])
	}

	byID := map[string]map[string]interface{}{}
	for _, entry := range doc["@graph"].([]interface{}) {
		node := entry.(map[string]interface{})
		byID[node["@id"].(string)] = node
	}

	descriptor := byID["ro-crate-metadata.json"]
	if descriptor == nil || descriptor["conformsTo"].(map[string]interface{})["@id"] != "https://w3id.org/ro/crate/1.2" {
		t.Errorf("metadata descriptor = %v", descriptor)
	}
	root := byID["./"]
	if root == nil || root["name"] != "Demo" || root["datePublished"] == "" {
		t.Errorf("root dataset = %v", root)
	}
	if root["dateModified"] != "2026-06-12T00:00:00Z" || root["identifier"] != "redcap-project-1" {
		t.Errorf("root dateModified/identifier = %v/%v", root["dateModified"], root["identifier"])
	}
	hasPart := root["hasPart"].([]interface{})
	if len(hasPart) != len(model.Files) {
		t.Errorf("hasPart has %d entries, want %d", len(hasPart), len(model.Files))
	}
	if byID["data.csv"] == nil || byID["data.csv"]["encodingFormat"] != "text/csv" {
		t.Errorf("data.csv file entity = %v", byID["data.csv"])
	}
	action := byID["#export-action"]
	if action == nil || action["@type"] != "CreateAction" ||
		action["instrument"].(map[string]interface{})["@id"] != "#rdm-integration-redcap2" {
		t.Errorf("provenance action = %v", action)
	}
	if byID["#rdm-integration-redcap2"] == nil || byID["#redcap"] == nil {
		t.Error("software application entities missing")
	}

	// Variables are flattened PropertyValue entities referenced from the root.
	variableRefs := root["variableMeasured"].([]interface{})
	if len(variableRefs) != len(model.Variables) {
		t.Errorf("variableMeasured refs = %d, want %d", len(variableRefs), len(model.Variables))
	}
	recordID := byID["#variable/record_id"]
	if recordID == nil || recordID["@type"] != "PropertyValue" || recordID["name"] != "record_id" {
		t.Errorf("record_id variable entity = %v", recordID)
	}
}

func TestBuildROCrateFlattensCodeLists(t *testing.T) {
	opts, _ := parsePluginOptions(`{"exportMode":"records","generatedAt":"2026-06-12T00:00:00Z"}`)
	model := sidecarTestModel(t, opts, transformPlan{},
		"record_id,sex\n1,1\n")

	data, err := buildROCrate(model)
	if err != nil {
		t.Fatalf("buildROCrate returned error: %v", err)
	}
	doc := map[string]interface{}{}
	_ = json.Unmarshal(data, &doc)
	byID := map[string]map[string]interface{}{}
	for _, entry := range doc["@graph"].([]interface{}) {
		node := entry.(map[string]interface{})
		byID[node["@id"].(string)] = node
	}

	sex := byID["#variable/sex"]
	if sex == nil {
		t.Fatal("missing #variable/sex entity")
	}
	termRefs := sex["valueReference"].([]interface{})
	if len(termRefs) != 2 {
		t.Fatalf("sex valueReference = %v", termRefs)
	}
	// RO-Crate JSON-LD must stay flattened: code terms are their own entities.
	firstRef := termRefs[0].(map[string]interface{})
	if len(firstRef) != 1 || firstRef["@id"] != "#variable/sex/code/1" {
		t.Errorf("term reference must be an @id ref, got %v", firstRef)
	}
	term := byID["#variable/sex/code/1"]
	if term == nil || term["@type"] != "DefinedTerm" || term["termCode"] != "1" || term["name"] != "Male" {
		t.Errorf("code term entity = %v", term)
	}
}

func TestBuildDDICDI(t *testing.T) {
	opts, _ := parsePluginOptions(`{"exportMode":"records","generatedAt":"2026-06-12T00:00:00Z"}`)
	model := sidecarTestModel(t, opts, transformPlan{},
		"record_id,age,sex\n1,34,1\n")

	data, err := buildDDICDI(model)
	if err != nil {
		t.Fatalf("buildDDICDI returned error: %v", err)
	}
	doc := map[string]interface{}{}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("ddi-cdi.jsonld is invalid JSON: %v", err)
	}
	if doc["@context"] != ddiCdiContext {
		t.Errorf("@context = %v", doc["@context"])
	}

	byType := map[string][]map[string]interface{}{}
	byID := map[string]map[string]interface{}{}
	for _, entry := range doc["@graph"].([]interface{}) {
		node := entry.(map[string]interface{})
		if typeName, ok := node["@type"].(string); ok {
			byType[typeName] = append(byType[typeName], node)
		}
		if id, ok := node["@id"].(string); ok {
			byID[id] = node
		}
	}

	if len(byType["WideDataSet"]) != 1 || len(byType["WideDataStructure"]) != 1 || len(byType["LogicalRecord"]) != 1 {
		t.Fatalf("missing structural nodes: %v", byType)
	}
	if len(byType["InstanceVariable"]) != 3 {
		t.Errorf("InstanceVariables = %d, want 3", len(byType["InstanceVariable"]))
	}
	if len(byType["IdentifierComponent"]) != 1 {
		t.Errorf("IdentifierComponents = %d, want 1 (record_id)", len(byType["IdentifierComponent"]))
	}
	if len(byType["PrimaryKey"]) != 1 || len(byType["PrimaryKeyComponent"]) != 1 {
		t.Error("primary key nodes missing")
	}
	// The SHACL shapes require the primary key to be reachable from the
	// structure and the component to use the full association term.
	if byID["#datastructure"]["has_PrimaryKey"] != "#primaryKey" {
		t.Errorf("datastructure has_PrimaryKey = %v", byID["#datastructure"]["has_PrimaryKey"])
	}
	if byID["#primaryKeyComponent"]["correspondsTo_DataStructureComponent"] == nil {
		t.Errorf("primaryKeyComponent = %v", byID["#primaryKeyComponent"])
	}
	// sex has a code list with two codes; per DDI-CDI each Code uses a
	// Notation (the value as it appears in the data) and denotes a Category
	// (the label).
	if len(byType["CodeList"]) != 1 || len(byType["Code"]) != 2 ||
		len(byType["Notation"]) != 2 || len(byType["Category"]) != 2 {
		t.Errorf("CodeList/Code/Notation/Category = %d/%d/%d/%d, want 1/2/2/2",
			len(byType["CodeList"]), len(byType["Code"]), len(byType["Notation"]), len(byType["Category"]))
	}
	code := byID["#sex_1_Code"]
	if code == nil || code["denotes"] != "#sex_1_Category" || code["uses_Notation"] != "#sex_1_Notation" {
		t.Fatalf("code node = %v", code)
	}
	if _, ok := code["identifier"]; ok {
		t.Error("Code must not carry a literal identifier (the value lives in the Notation)")
	}
	notation := byID["#sex_1_Notation"]
	content := notation["content"].(map[string]interface{})
	if content["@type"] != "TypedString" || content["content"] != "1" {
		t.Errorf("notation content = %v, want TypedString with the data value", content)
	}
	category := byID["#sex_1_Category"]
	categoryName := category["name"].(map[string]interface{})
	if categoryName["@type"] != "ObjectName" || categoryName["name"] != "Male" {
		t.Errorf("category name = %v", categoryName)
	}
	codeList := byID["#sex_CodeList"]
	if codeList["allowsDuplicates"] != false {
		t.Errorf("codeList allowsDuplicates = %v, want false (required by SHACL)", codeList["allowsDuplicates"])
	}
	codeListName := codeList["name"].(map[string]interface{})
	if codeListName["@type"] != "ObjectName" {
		t.Errorf("codeList name = %v, want ObjectName object", codeList["name"])
	}
	// CSV exports describe the physical layout
	layout := byID["#physicalSegmentLayout"]
	if layout == nil || layout["isDelimited"] != true || layout["delimiter"] != "," {
		t.Errorf("physical layout = %v", layout)
	}
	if len(byType["ValueMapping"]) != 3 || len(byType["ValueMappingPosition"]) != 3 {
		t.Errorf("value mappings = %d/%d, want 3/3", len(byType["ValueMapping"]), len(byType["ValueMappingPosition"]))
	}
	// age is numeric -> Integer datatype in the DDI CV
	domain := byID["#age_Substantive_Value_Domain"]
	entry := domain["recommendedDataType"].(map[string]interface{})
	if entry["entryValue"] != "Integer" {
		t.Errorf("age datatype = %v", entry["entryValue"])
	}
}

func TestBuildDDICDIJSONExportSkipsPhysicalLayout(t *testing.T) {
	opts, _ := parsePluginOptions(`{"exportMode":"records","dataFormat":"json"}`)
	model := sidecarTestModel(t, opts, transformPlan{}, `[{"record_id":"1","age":"34"}]`)
	model.DataFormat = "json"

	data, err := buildDDICDI(model)
	if err != nil {
		t.Fatalf("buildDDICDI returned error: %v", err)
	}
	if strings.Contains(string(data), "PhysicalSegmentLayout") || strings.Contains(string(data), "ValueMapping") {
		t.Error("JSON exports must not describe a delimited physical layout")
	}
}

// TestDumpSidecarsForValidation writes generated sidecars to SIDECAR_DUMP_DIR
// for external validation: pyshacl against the official DDI-CDI 1.0 SHACL
// shapes (the ones bundled with the cdi-viewer previewer) and
// `mlcroissant validate --jsonld`. Skipped unless the env var is set:
//
//	SIDECAR_DUMP_DIR=/tmp/dump go test ./app/plugin/impl/redcap2/ -run TestDumpSidecarsForValidation
func TestDumpSidecarsForValidation(t *testing.T) {
	dir := os.Getenv("SIDECAR_DUMP_DIR")
	if dir == "" {
		t.Skip("SIDECAR_DUMP_DIR not set")
	}
	opts, _ := parsePluginOptions(`{"exportMode":"records","generatedAt":"2026-06-12T00:00:00Z"}`)
	plan := testPlan(map[string]string{"name": "pseudonymize", "email": "drop"})
	plan.keyFingerprint = "abcdef0123456789"
	model := sidecarTestModel(t, opts, plan,
		"record_id,name,age,weight,sex,consent,visit_date\n1,x,34,70.5,1,1,2026-01-01\n")
	files := map[string][]byte{}
	mime := map[string]string{}
	if warnings := addSidecars(model, "redcap/records", files, mime); len(warnings) != 0 {
		t.Fatalf("sidecar warnings: %v", warnings)
	}
	for path, data := range files {
		out := filepath.Join(dir, filepath.Base(path))
		if err := os.WriteFile(out, data, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("wrote %s", out)
	}
}

// End to end: the bundle contains the three sidecars and the ODM file, they
// are valid JSON/XML, dropped variables are absent, and generation is
// deterministic (same input, same bytes).
func TestEndToEndSidecars(t *testing.T) {
	f := newFakeRedcap()
	defer f.close()

	pluginOpts := `{
		"exportMode": "records",
		"variables": [{"name": "email", "anonymization": "drop"}]
	}`
	croissant, manifest := queryAndRead(t, f, pluginOpts, "redcap/records/croissant.json")

	doc := map[string]interface{}{}
	if err := json.Unmarshal(croissant, &doc); err != nil {
		t.Fatalf("croissant.json invalid: %v", err)
	}
	if strings.Contains(string(croissant), `"email"`) {
		t.Error("dropped variable must not appear in croissant.json")
	}

	files := manifest["files"].(map[string]interface{})
	for _, key := range []string{"croissant", "ro_crate", "ddi_cdi", "project_metadata"} {
		if files[key] == nil {
			t.Errorf("manifest files missing %s: %v", key, files)
		}
	}

	roCrate, _ := queryAndRead(t, f, pluginOpts, "redcap/records/ro-crate-metadata.json")
	if err := json.Unmarshal(roCrate, &map[string]interface{}{}); err != nil {
		t.Fatalf("ro-crate-metadata.json invalid: %v", err)
	}
	ddiCdi, _ := queryAndRead(t, f, pluginOpts, "redcap/records/ddi-cdi.jsonld")
	if err := json.Unmarshal(ddiCdi, &map[string]interface{}{}); err != nil {
		t.Fatalf("ddi-cdi.jsonld invalid: %v", err)
	}
	odm, _ := queryAndRead(t, f, pluginOpts, "redcap/records/project_metadata.xml")
	if !strings.Contains(string(odm), "<ODM") {
		t.Errorf("project_metadata.xml = %q, want ODM XML", string(odm))
	}

	// Determinism across separate servers (fresh caches).
	f2 := newFakeRedcap()
	defer f2.close()
	croissant2, _ := queryAndRead(t, f2, pluginOpts, "redcap/records/croissant.json")
	if string(croissant) != string(croissant2) {
		t.Error("croissant.json must be deterministic for identical exports")
	}
}
