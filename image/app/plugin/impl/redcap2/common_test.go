// Author: Eryk Kulikowski @ KU Leuven (2026). Apache 2.0 License

package redcap2

import (
	"context"
	"encoding/json"
	"integration/app/plugin/types"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

func TestParsePluginOptionsDefaults(t *testing.T) {
	for _, raw := range []string{"", "   "} {
		opts, err := parsePluginOptions(raw)
		if err != nil {
			t.Fatalf("parsePluginOptions(%q) returned error: %v", raw, err)
		}
		want := pluginOptions{
			ExportMode:        "report",
			DataFormat:        "csv",
			RecordType:        "flat",
			CsvDelimiter:      ",",
			RawOrLabel:        "raw",
			RawOrLabelHeaders: "raw",
			GeneratedAt:       "missing-generated-at",
		}
		if !reflect.DeepEqual(opts, want) {
			t.Fatalf("parsePluginOptions(%q) = %+v, want %+v", raw, opts, want)
		}
	}
}

func TestParsePluginOptionsInvalidJSON(t *testing.T) {
	if _, err := parsePluginOptions("{not json"); err == nil {
		t.Fatal("expected error for invalid pluginOptions JSON")
	}
}

func TestParsePluginOptionsNormalization(t *testing.T) {
	opts, err := parsePluginOptions(`{
		"exportMode": " Records ",
		"dataFormat": "JSON",
		"recordType": "EAV",
		"csvDelimiter": " TSV ",
		"rawOrLabel": "Label",
		"rawOrLabelHeaders": "LABEL",
		"reportId": " 7 ",
		"fields": [" age", "age", "", "name "],
		"variables": [
			{"name": " email ", "anonymization": "BLANK"},
			{"name": "age", "anonymization": "whatever"}
		]
	}`)
	if err != nil {
		t.Fatalf("parsePluginOptions returned error: %v", err)
	}
	if opts.ExportMode != "records" {
		t.Errorf("ExportMode = %q, want records", opts.ExportMode)
	}
	if opts.DataFormat != "json" {
		t.Errorf("DataFormat = %q, want json", opts.DataFormat)
	}
	if opts.RecordType != "eav" {
		t.Errorf("RecordType = %q, want eav", opts.RecordType)
	}
	if opts.CsvDelimiter != "\t" {
		t.Errorf("CsvDelimiter = %q, want tab", opts.CsvDelimiter)
	}
	if opts.RawOrLabel != "label" || opts.RawOrLabelHeaders != "label" {
		t.Errorf("RawOrLabel = %q, RawOrLabelHeaders = %q, want label/label", opts.RawOrLabel, opts.RawOrLabelHeaders)
	}
	if opts.ReportID != "7" {
		t.Errorf("ReportID = %q, want 7", opts.ReportID)
	}
	if !reflect.DeepEqual(opts.Fields, []string{"age", "name"}) {
		t.Errorf("Fields = %v, want [age name]", opts.Fields)
	}
	wantVars := []variableOption{
		{Name: "email", Anonymization: "blank"},
		{Name: "age", Anonymization: "none"},
	}
	if !reflect.DeepEqual(opts.Variables, wantVars) {
		t.Errorf("Variables = %v, want %v", opts.Variables, wantVars)
	}
	if opts.GeneratedAt != "missing-generated-at" {
		t.Errorf("GeneratedAt = %q, want missing-generated-at", opts.GeneratedAt)
	}
}

func TestParsePluginOptionsUnknownValuesFallBackToDefaults(t *testing.T) {
	opts, err := parsePluginOptions(`{
		"exportMode": "weird",
		"dataFormat": "xml",
		"recordType": "wide",
		"csvDelimiter": ";",
		"rawOrLabel": "both",
		"rawOrLabelHeaders": "other"
	}`)
	if err != nil {
		t.Fatalf("parsePluginOptions returned error: %v", err)
	}
	if opts.ExportMode != "report" || opts.DataFormat != "csv" || opts.RecordType != "flat" ||
		opts.CsvDelimiter != "," || opts.RawOrLabel != "raw" || opts.RawOrLabelHeaders != "raw" {
		t.Fatalf("unknown values not normalized to defaults: %+v", opts)
	}
}

func TestNormalizeStringSlice(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{name: "nil", in: nil, want: nil},
		{name: "all_empty", in: []string{"", "  "}, want: nil},
		{name: "trim_and_dedup", in: []string{" a", "a", "b ", "", "b"}, want: []string{"a", "b"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := normalizeStringSlice(tt.in); !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("normalizeStringSlice(%v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestGetAPIURL(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "https://redcap.example.org", want: "https://redcap.example.org/api/"},
		{in: "https://redcap.example.org/", want: "https://redcap.example.org/api/"},
		{in: "https://redcap.example.org/api", want: "https://redcap.example.org/api/"},
		{in: "https://redcap.example.org/api/", want: "https://redcap.example.org/api/"},
		{in: "  https://redcap.example.org ", want: "https://redcap.example.org/api/"},
	}
	for _, tt := range tests {
		if got := getAPIURL(tt.in); got != tt.want {
			t.Errorf("getAPIURL(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestSanitizeReportID(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{in: "", want: "unknown"},
		{in: "42", want: "42"},
		{in: "My Report/7", want: "My_Report_7"},
		{in: "a.b-c_D9", want: "a.b-c_D9"},
	}
	for _, tt := range tests {
		if got := sanitizeReportID(tt.in); got != tt.want {
			t.Errorf("sanitizeReportID(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestBlankFields(t *testing.T) {
	opts := pluginOptions{Variables: []variableOption{
		{Name: "email", Anonymization: "blank"},
		{Name: "age", Anonymization: "none"},
		{Name: "", Anonymization: "blank"},
	}}
	got := blankFields(opts)
	if !reflect.DeepEqual(got, map[string]bool{"email": true}) {
		t.Fatalf("blankFields = %v, want only email", got)
	}
}

func TestApplySharedExportParamsDefaults(t *testing.T) {
	opts, _ := parsePluginOptions("")
	form := url.Values{}
	applySharedExportParams(form, opts)
	if len(form) != 0 {
		t.Fatalf("default options should send no shared params (type is records-only), got %v", form)
	}
}

func TestApplySharedExportParamsNonDefaults(t *testing.T) {
	opts, _ := parsePluginOptions(`{"csvDelimiter":"tab","rawOrLabel":"label","rawOrLabelHeaders":"label"}`)
	form := url.Values{}
	applySharedExportParams(form, opts)
	want := map[string]string{
		"csvDelimiter":      "tab",
		"rawOrLabel":        "label",
		"rawOrLabelHeaders": "label",
	}
	for key, value := range want {
		if got := form.Get(key); got != value {
			t.Errorf("%s = %q, want %q", key, got, value)
		}
	}
	if _, ok := form["type"]; ok {
		t.Error("type must not be a shared parameter (content=report has no type)")
	}
}

func TestApplySharedExportParamsJSONSuppressesCSVParams(t *testing.T) {
	opts, _ := parsePluginOptions(`{"dataFormat":"json","csvDelimiter":"tab","rawOrLabelHeaders":"label"}`)
	form := url.Values{}
	applySharedExportParams(form, opts)
	for _, key := range []string{"csvDelimiter", "rawOrLabelHeaders"} {
		if _, ok := form[key]; ok {
			t.Errorf("%s must not be sent for JSON exports", key)
		}
	}
}

func TestApplySharedExportParamsEAVSuppressesHeaderLabels(t *testing.T) {
	opts, _ := parsePluginOptions(`{"exportMode":"records","recordType":"eav","rawOrLabelHeaders":"label"}`)
	form := url.Values{}
	applySharedExportParams(form, opts)
	if _, ok := form["rawOrLabelHeaders"]; ok {
		t.Error("rawOrLabelHeaders must not be sent for EAV exports (flat CSV only)")
	}
}

func TestApplyRecordOnlyFiltersDefaults(t *testing.T) {
	opts, _ := parsePluginOptions("")
	form := url.Values{}
	applyRecordOnlyFilters(form, opts)
	if got := form.Get("type"); got != "flat" {
		t.Errorf("type = %q, want flat", got)
	}
	if len(form) != 1 {
		t.Fatalf("expected only type for default options, got %v", form)
	}
}

func TestApplyRecordOnlyFiltersFull(t *testing.T) {
	opts, _ := parsePluginOptions(`{
		"recordType": "eav",
		"fields": ["age", "name", "age"],
		"forms": ["demographics"],
		"events": ["baseline_arm_1"],
		"records": ["1", "2"],
		"filterLogic": "[age] > 30",
		"dateRangeBegin": "2026-01-02",
		"dateRangeEnd": "2026-01-31",
		"exportSurveyFields": true,
		"exportDataAccessGroups": true
	}`)
	form := url.Values{}
	applyRecordOnlyFilters(form, opts)
	want := map[string]string{
		"type":                   "eav",
		"fields":                 "age,name",
		"forms":                  "demographics",
		"events":                 "baseline_arm_1",
		"records":                "1,2",
		"filterLogic":            "[age] > 30",
		"dateRangeBegin":         "2026-01-02 00:00:00",
		"dateRangeEnd":           "2026-01-31 23:59:59",
		"exportSurveyFields":     "true",
		"exportDataAccessGroups": "true",
	}
	for key, value := range want {
		if got := form.Get(key); got != value {
			t.Errorf("%s = %q, want %q", key, got, value)
		}
	}
}

func TestApplyRecordOnlyFiltersKeepsExplicitTimes(t *testing.T) {
	opts, _ := parsePluginOptions(`{"dateRangeBegin":"2026-01-02 10:30:00","dateRangeEnd":"2026-01-31 12:00:00"}`)
	form := url.Values{}
	applyRecordOnlyFilters(form, opts)
	if got := form.Get("dateRangeBegin"); got != "2026-01-02 10:30:00" {
		t.Errorf("dateRangeBegin = %q, want explicit time preserved", got)
	}
	if got := form.Get("dateRangeEnd"); got != "2026-01-31 12:00:00" {
		t.Errorf("dateRangeEnd = %q, want explicit time preserved", got)
	}
}

func TestParseDictionary(t *testing.T) {
	dict := parseDictionary([]byte(testMetadataCSV))
	if !reflect.DeepEqual(dict.fieldOrder, []string{"record_id", "name", "email", "age"}) {
		t.Errorf("fieldOrder = %v", dict.fieldOrder)
	}
	if dict.fieldType["name"] != "text" {
		t.Errorf("fieldType[name] = %q, want text", dict.fieldType["name"])
	}
	if !reflect.DeepEqual(dict.labelFields["Email Address"], []string{"email"}) {
		t.Errorf("labelFields[Email Address] = %v, want [email]", dict.labelFields["Email Address"])
	}
}

func TestDictionaryFileUploadFields(t *testing.T) {
	metadata := "field_name,field_type,field_label\n" +
		"record_id,text,Record ID\n" +
		"consent_scan,file,Consent Scan\n" +
		"mri_image,file,MRI Image\n"
	dict := parseDictionary([]byte(metadata))
	if got := dict.fileUploadFields(); !reflect.DeepEqual(got, []string{"consent_scan", "mri_image"}) {
		t.Fatalf("fileUploadFields = %v", got)
	}
}

func TestBaseFieldName(t *testing.T) {
	tests := map[string]string{
		"phones___1": "phones",
		"phones":     "phones",
		"a___b___c":  "a",
		"___x":       "___x", // no base before the separator
	}
	for in, want := range tests {
		if got := baseFieldName(in); got != want {
			t.Errorf("baseFieldName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestResolveHeaderFields(t *testing.T) {
	dict := parseDictionary([]byte(testMetadataCSV))
	if got := resolveHeaderFields("phones___2", false, dict); !reflect.DeepEqual(got, []string{"phones"}) {
		t.Errorf("raw checkbox header = %v, want [phones]", got)
	}
	if got := resolveHeaderFields("Email Address", true, dict); !reflect.DeepEqual(got, []string{"email"}) {
		t.Errorf("label header = %v, want [email]", got)
	}
	if got := resolveHeaderFields("Full Name (choice=Other)", true, dict); !reflect.DeepEqual(got, []string{"name"}) {
		t.Errorf("checkbox label header = %v, want [name]", got)
	}
	if got := resolveHeaderFields("redcap_event_name", true, dict); !reflect.DeepEqual(got, []string{"redcap_event_name"}) {
		t.Errorf("unknown header should resolve to itself, got %v", got)
	}
}

func TestBlankFlatCSV(t *testing.T) {
	dict := parseDictionary([]byte(testMetadataCSV))
	out, exported, audit, err := blankFlatCSV([]byte(testDataCSV), ',', map[string]bool{"name": true, "email": true}, false, dict)
	if err != nil {
		t.Fatalf("blankFlatCSV returned error: %v", err)
	}
	want := "record_id,name,email,age\n1,,,34\n2,,,29\n"
	if string(out) != want {
		t.Errorf("blanked CSV = %q, want %q", string(out), want)
	}
	if !reflect.DeepEqual(exported, []string{"record_id", "name", "email", "age"}) {
		t.Errorf("exported = %v", exported)
	}
	for _, entry := range audit {
		if entry.Matched != 1 {
			t.Errorf("audit %s matched = %d, want 1", entry.Field, entry.Matched)
		}
	}
}

func TestBlankFlatCSVCheckboxExpansion(t *testing.T) {
	dict := parseDictionary([]byte("field_name,field_type,field_label\nrecord_id,text,Record ID\nphones,checkbox,Phone Types\n"))
	data := "record_id,phones___1,phones___2\n1,555-1234,555-5678\n"
	out, exported, audit, err := blankFlatCSV([]byte(data), ',', map[string]bool{"phones": true}, false, dict)
	if err != nil {
		t.Fatalf("blankFlatCSV returned error: %v", err)
	}
	want := "record_id,phones___1,phones___2\n1,,\n"
	if string(out) != want {
		t.Errorf("blanked CSV = %q, want %q", string(out), want)
	}
	if !reflect.DeepEqual(exported, []string{"record_id", "phones"}) {
		t.Errorf("exported = %v, want base names", exported)
	}
	if len(audit) != 1 || audit[0].Matched != 2 {
		t.Errorf("audit = %+v, want phones matched=2", audit)
	}
}

func TestBlankFlatCSVLabelHeaders(t *testing.T) {
	dict := parseDictionary([]byte(testMetadataCSV))
	data := "Record ID,Full Name,Email Address,Age\n1,John,john@example.org,34\n"
	out, exported, audit, err := blankFlatCSV([]byte(data), ',', map[string]bool{"email": true}, true, dict)
	if err != nil {
		t.Fatalf("blankFlatCSV returned error: %v", err)
	}
	want := "Record ID,Full Name,Email Address,Age\n1,John,,34\n"
	if string(out) != want {
		t.Errorf("blanked CSV = %q, want %q", string(out), want)
	}
	if !reflect.DeepEqual(exported, []string{"record_id", "name", "email", "age"}) {
		t.Errorf("exported = %v, want translated field names", exported)
	}
	if len(audit) != 1 || audit[0].Matched != 1 {
		t.Errorf("audit = %+v", audit)
	}
}

func TestBlankFlatCSVZeroMatchAudit(t *testing.T) {
	dict := parseDictionary([]byte(testMetadataCSV))
	out, _, audit, err := blankFlatCSV([]byte(testDataCSV), ',', map[string]bool{"missing": true}, false, dict)
	if err != nil {
		t.Fatalf("blankFlatCSV returned error: %v", err)
	}
	if string(out) != testDataCSV {
		t.Error("data changed despite no matching blank columns")
	}
	if len(audit) != 1 || audit[0].Matched != 0 || audit[0].Note == "" {
		t.Errorf("zero-match audit missing note: %+v", audit)
	}
}

func TestBlankEAVCSV(t *testing.T) {
	dict := parseDictionary([]byte(testMetadataCSV))
	data := "record,redcap_event_name,field_name,value\n" +
		"1,baseline_arm_1,name,John\n" +
		"1,baseline_arm_1,email,john@example.org\n" +
		"2,baseline_arm_1,email,jane@example.org\n" +
		"2,baseline_arm_1,age,29\n"
	out, exported, audit, err := blankEAVCSV([]byte(data), ',', map[string]bool{"email": true}, dict)
	if err != nil {
		t.Fatalf("blankEAVCSV returned error: %v", err)
	}
	want := "record,redcap_event_name,field_name,value\n" +
		"1,baseline_arm_1,name,John\n" +
		"1,baseline_arm_1,email,\n" +
		"2,baseline_arm_1,email,\n" +
		"2,baseline_arm_1,age,29\n"
	if string(out) != want {
		t.Errorf("blanked EAV CSV = %q, want %q", string(out), want)
	}
	if !reflect.DeepEqual(exported, []string{"record_id", "name", "email", "age"}) {
		t.Errorf("exported = %v (record_id must be seeded)", exported)
	}
	if len(audit) != 1 || audit[0].Matched != 2 {
		t.Errorf("audit = %+v, want email matched=2 rows", audit)
	}
}

func TestBlankEAVJSON(t *testing.T) {
	dict := parseDictionary([]byte(testMetadataCSV))
	data := `[{"record":"1","field_name":"name","value":"John"},{"record":"1","field_name":"email","value":"john@example.org"}]`
	out, exported, audit, err := blankEAVJSON([]byte(data), map[string]bool{"email": true}, dict)
	if err != nil {
		t.Fatalf("blankEAVJSON returned error: %v", err)
	}
	rows := []map[string]string{}
	if err := json.Unmarshal(out, &rows); err != nil {
		t.Fatalf("blanked EAV JSON invalid: %v", err)
	}
	if rows[1]["value"] != "" || rows[0]["value"] != "John" {
		t.Errorf("unexpected EAV JSON blanking: %v", rows)
	}
	if !reflect.DeepEqual(exported, []string{"record_id", "name", "email"}) {
		t.Errorf("exported = %v", exported)
	}
	if len(audit) != 1 || audit[0].Matched != 1 {
		t.Errorf("audit = %+v", audit)
	}
}

func TestBlankFlatJSONCheckboxExpansion(t *testing.T) {
	data := `[{"record_id":"1","phones___1":"555-1234","phones___2":"555-5678","age":"34"}]`
	out, exported, audit, err := blankFlatJSON([]byte(data), map[string]bool{"phones": true})
	if err != nil {
		t.Fatalf("blankFlatJSON returned error: %v", err)
	}
	rows := []map[string]string{}
	if err := json.Unmarshal(out, &rows); err != nil {
		t.Fatalf("blanked JSON invalid: %v", err)
	}
	if rows[0]["phones___1"] != "" || rows[0]["phones___2"] != "" || rows[0]["age"] != "34" {
		t.Errorf("unexpected JSON blanking: %v", rows)
	}
	if !reflect.DeepEqual(exported, []string{"age", "phones", "record_id"}) {
		t.Errorf("exported = %v, want sorted base names", exported)
	}
	if len(audit) != 1 || audit[0].Matched != 2 {
		t.Errorf("audit = %+v, want phones matched=2", audit)
	}
}

func TestBlankFlatJSONInvalid(t *testing.T) {
	if _, _, _, err := blankFlatJSON([]byte("not json"), nil); err == nil {
		t.Fatal("expected error for invalid JSON input")
	}
}

func TestFilterMetadataCSV(t *testing.T) {
	out, err := filterMetadataCSV([]byte(testMetadataCSV), []string{"age", "record_id"})
	if err != nil {
		t.Fatalf("filterMetadataCSV returned error: %v", err)
	}
	want := "field_name,form_name,field_type,field_label,identifier\n" +
		"record_id,demographics,text,Record ID,\n" +
		"age,demographics,text,Age,\n"
	if string(out) != want {
		t.Errorf("filtered metadata = %q, want %q", string(out), want)
	}
}

func TestFilterMetadataCSVPassthrough(t *testing.T) {
	out, err := filterMetadataCSV([]byte(testMetadataCSV), nil)
	if err != nil || string(out) != testMetadataCSV {
		t.Errorf("expected passthrough without fields, got %q (err %v)", string(out), err)
	}
	noFieldName := "a,b\n1,2\n"
	out, err = filterMetadataCSV([]byte(noFieldName), []string{"x"})
	if err != nil || string(out) != noFieldName {
		t.Errorf("expected passthrough without field_name column, got %q (err %v)", string(out), err)
	}
}

func TestDetectLongitudinal(t *testing.T) {
	tests := []struct {
		name    string
		payload string
		want    bool
	}{
		{name: "object_string_one", payload: `{"is_longitudinal":"1"}`, want: true},
		{name: "object_bool", payload: `{"is_longitudinal":true}`, want: true},
		{name: "object_yes", payload: `{"is_longitudinal":"yes"}`, want: true},
		{name: "object_number", payload: `{"is_longitudinal":1}`, want: true},
		{name: "object_zero", payload: `{"is_longitudinal":"0"}`, want: false},
		{name: "object_missing", payload: `{"project_id":1}`, want: false},
		{name: "array_form", payload: `[{"is_longitudinal":"y"}]`, want: true},
		{name: "alternate_key", payload: `{"is_longitudinal_project":"true"}`, want: true},
		{name: "invalid", payload: `not json`, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := detectLongitudinal([]byte(tt.payload)); got != tt.want {
				t.Fatalf("detectLongitudinal(%q) = %v, want %v", tt.payload, got, tt.want)
			}
		})
	}
}

func TestProjectIdentity(t *testing.T) {
	id, title := projectIdentity([]byte(`{"project_id":42,"project_title":"Demo Study"}`))
	if title != "Demo Study" {
		t.Errorf("title = %q", title)
	}
	if num, ok := id.(float64); !ok || num != 42 {
		t.Errorf("id = %v (%T), want 42", id, id)
	}
	id, title = projectIdentity([]byte(`[{"project_id":"7","project_title":"Array Form"}]`))
	if id != "7" || title != "Array Form" {
		t.Errorf("array form: id=%v title=%q", id, title)
	}
	if id, title = projectIdentity([]byte(`not json`)); id != nil || title != "" {
		t.Errorf("invalid payload should yield empty identity, got %v %q", id, title)
	}
}

func TestDeduplicatedSelectItems(t *testing.T) {
	got := deduplicatedSelectItems([]string{" b", "a", "b", ""})
	want := []types.SelectItem{
		{Label: "a", Value: "a"},
		{Label: "b", Value: "b"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("deduplicatedSelectItems = %v, want %v", got, want)
	}
}

func TestDeduplicatedSelectItemsWithIdentifiers(t *testing.T) {
	got := deduplicatedSelectItemsWithIdentifiers(
		[]string{"email", "age", "email", " name "},
		map[string]bool{"email": true, "name": true},
	)
	want := []types.SelectItem{
		{Label: "age", Value: "age"},
		{Label: "email", Value: "email", Selected: true},
		{Label: "name", Value: "name", Selected: true},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("deduplicatedSelectItemsWithIdentifiers = %v, want %v", got, want)
	}
}

func TestMakeManifestReportMode(t *testing.T) {
	opts, _ := parsePluginOptions(`{"exportMode":"report","reportId":"7","recordType":"eav","variables":[{"name":"email","anonymization":"blank"}]}`)
	extras := manifestExtras{
		Audit:            []anonymizationAudit{{Field: "email", Mode: "blank", Matched: 1}},
		FileUploadFields: []string{"consent_scan"},
		ProjectID:        float64(1),
		ProjectTitle:     "Demo",
	}
	data, err := makeManifest(opts, "7", "redcap/report-7/data.csv", "redcap/report-7/metadata.csv",
		"redcap/report-7/project_info.json", "redcap/report-7/events.csv", "redcap/report-7/form_event_mapping.csv",
		"14.5.5", []string{"something failed"}, extras)
	if err != nil {
		t.Fatalf("makeManifest returned error: %v", err)
	}
	manifest := map[string]interface{}{}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("manifest is invalid JSON: %v", err)
	}
	if manifest["plugin"] != "redcap2" || manifest["export_mode"] != "report" {
		t.Errorf("unexpected plugin/export_mode: %v / %v", manifest["plugin"], manifest["export_mode"])
	}
	if manifest["report_id"] != "7" {
		t.Errorf("report_id = %v, want 7", manifest["report_id"])
	}
	export := manifest["export"].(map[string]interface{})
	if export["record_type"] != "flat" {
		t.Errorf("report-mode record_type = %v, want forced flat (no type param)", export["record_type"])
	}
	files := manifest["files"].(map[string]interface{})
	if files["events"] != "redcap/report-7/events.csv" || files["form_event_mapping"] != "redcap/report-7/form_event_mapping.csv" {
		t.Errorf("longitudinal files missing from manifest: %v", files)
	}
	project := manifest["project"].(map[string]interface{})
	if project["title"] != "Demo" {
		t.Errorf("project = %v", project)
	}
	attachments := manifest["attachments"].(map[string]interface{})
	if attachments["exported"] != false {
		t.Errorf("attachments.exported = %v, want false", attachments["exported"])
	}
	if _, ok := manifest["anonymization_audit"]; !ok {
		t.Error("anonymization_audit missing from manifest")
	}
	if _, ok := manifest["variables"]; !ok {
		t.Error("variables missing from manifest")
	}
	if _, ok := manifest["warnings"]; !ok {
		t.Error("warnings missing from manifest")
	}
}

func TestMakeManifestRecordsMode(t *testing.T) {
	opts, _ := parsePluginOptions(`{"exportMode":"records"}`)
	data, err := makeManifest(opts, "", "redcap/records/data.csv", "redcap/records/metadata.csv",
		"redcap/records/project_info.json", "", "", "", nil, manifestExtras{})
	if err != nil {
		t.Fatalf("makeManifest returned error: %v", err)
	}
	manifest := map[string]interface{}{}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatalf("manifest is invalid JSON: %v", err)
	}
	if _, ok := manifest["report_id"]; ok {
		t.Error("records-mode manifest should not contain report_id")
	}
	for _, key := range []string{"variables", "warnings", "attachments", "anonymization_audit", "project", "dictionary_fields_not_exported"} {
		if _, ok := manifest[key]; ok {
			t.Errorf("empty %s should be omitted from manifest", key)
		}
	}
	files := manifest["files"].(map[string]interface{})
	for _, key := range []string{"events", "form_event_mapping"} {
		if _, ok := files[key]; ok {
			t.Errorf("non-longitudinal manifest should not list %s", key)
		}
	}
}

func TestMakeManifestZeroMatchAuditAddsWarning(t *testing.T) {
	opts, _ := parsePluginOptions(`{"exportMode":"records"}`)
	extras := manifestExtras{Audit: []anonymizationAudit{{Field: "ghost", Mode: "blank", Matched: 0, Note: "field not present in export"}}}
	data, err := makeManifest(opts, "", "d", "m", "p", "", "", "", nil, extras)
	if err != nil {
		t.Fatalf("makeManifest returned error: %v", err)
	}
	manifest := map[string]interface{}{}
	_ = json.Unmarshal(data, &manifest)
	warnings, ok := manifest["warnings"].([]interface{})
	if !ok || len(warnings) == 0 || !strings.Contains(warnings[0].(string), "ghost") {
		t.Fatalf("expected zero-match warning, got %v", manifest["warnings"])
	}
}

func TestBundleCacheKeyStability(t *testing.T) {
	base, _ := parsePluginOptions(`{"exportMode":"report","reportId":"7","generatedAt":"2026-01-01T00:00:00Z"}`)
	sameButLater := base
	sameButLater.GeneratedAt = "2026-06-11T00:00:00Z"
	if bundleCacheKey("https://r", "tok", base) != bundleCacheKey("https://r", "tok", sameButLater) {
		t.Error("generatedAt should not change the cache key")
	}

	otherReport := base
	otherReport.ReportID = "8"
	if bundleCacheKey("https://r", "tok", base) == bundleCacheKey("https://r", "tok", otherReport) {
		t.Error("different report id should change the cache key")
	}

	otherMode := base
	otherMode.ExportMode = "records"
	if bundleCacheKey("https://r", "tok", base) == bundleCacheKey("https://r", "tok", otherMode) {
		t.Error("different export mode should change the cache key")
	}

	surveyFields := base
	surveyFields.ExportSurveyFields = true
	if bundleCacheKey("https://r", "tok", base) == bundleCacheKey("https://r", "tok", surveyFields) {
		t.Error("exportSurveyFields should change the cache key")
	}

	if bundleCacheKey("https://r", "tok", base) == bundleCacheKey("https://r", "other", base) {
		t.Error("different token should change the cache key")
	}
}

func TestRedcapRequestErrorBody(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ERROR: You do not have permissions to use the API"))
	}))
	defer server.Close()

	form := baseForm("tok", "record", "csv")
	_, err := redcapRequest(context.Background(), server.URL, form)
	if err == nil || !strings.Contains(err.Error(), "redcap error") {
		t.Fatalf("expected redcap error for ERROR body, got %v", err)
	}
}

func TestRedcapRequestHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "forbidden", http.StatusForbidden)
	}))
	defer server.Close()

	form := baseForm("tok", "record", "csv")
	_, err := redcapRequest(context.Background(), server.URL, form)
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Fatalf("expected status error, got %v", err)
	}
}
