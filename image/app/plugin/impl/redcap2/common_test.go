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
		"rawOrLabel": "other",
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
	if got := form.Get("type"); got != "flat" {
		t.Errorf("type = %q, want flat", got)
	}
	for _, key := range []string{"csvDelimiter", "rawOrLabel", "rawOrLabelHeaders"} {
		if _, ok := form[key]; ok {
			t.Errorf("default options should not send %q", key)
		}
	}
}

func TestApplySharedExportParamsNonDefaults(t *testing.T) {
	opts, _ := parsePluginOptions(`{"recordType":"eav","csvDelimiter":"tab","rawOrLabel":"label","rawOrLabelHeaders":"label"}`)
	form := url.Values{}
	applySharedExportParams(form, opts)
	want := map[string]string{
		"type":              "eav",
		"csvDelimiter":      "tab",
		"rawOrLabel":        "label",
		"rawOrLabelHeaders": "label",
	}
	for key, value := range want {
		if got := form.Get(key); got != value {
			t.Errorf("%s = %q, want %q", key, got, value)
		}
	}
}

func TestApplyRecordOnlyFiltersEmpty(t *testing.T) {
	opts, _ := parsePluginOptions("")
	form := url.Values{}
	applyRecordOnlyFilters(form, opts)
	if len(form) != 0 {
		t.Fatalf("expected no record-only params for default options, got %v", form)
	}
}

func TestApplyRecordOnlyFiltersFull(t *testing.T) {
	opts, _ := parsePluginOptions(`{
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

func TestApplyBlankCSV(t *testing.T) {
	out, header, err := applyBlankCSV([]byte(testDataCSV), ',', map[string]bool{"name": true, "email": true})
	if err != nil {
		t.Fatalf("applyBlankCSV returned error: %v", err)
	}
	wantHeader := []string{"record_id", "name", "email", "age"}
	if !reflect.DeepEqual(header, wantHeader) {
		t.Errorf("header = %v, want %v", header, wantHeader)
	}
	want := "record_id,name,email,age\n1,,,34\n2,,,29\n"
	if string(out) != want {
		t.Errorf("blanked CSV = %q, want %q", string(out), want)
	}
}

func TestApplyBlankCSVNoMatchingColumns(t *testing.T) {
	out, header, err := applyBlankCSV([]byte(testDataCSV), ',', map[string]bool{"missing": true})
	if err != nil {
		t.Fatalf("applyBlankCSV returned error: %v", err)
	}
	if string(out) != testDataCSV {
		t.Errorf("data changed despite no matching blank columns")
	}
	if len(header) != 4 {
		t.Errorf("header = %v, want 4 columns", header)
	}
}

func TestApplyBlankCSVEmptyInput(t *testing.T) {
	out, header, err := applyBlankCSV(nil, ',', map[string]bool{"name": true})
	if err != nil {
		t.Fatalf("applyBlankCSV returned error: %v", err)
	}
	if len(out) != 0 || header != nil {
		t.Errorf("expected empty passthrough, got out=%q header=%v", out, header)
	}
}

func TestApplyBlankJSON(t *testing.T) {
	out, fields, err := applyBlankJSON([]byte(testDataJSON), map[string]bool{"name": true, "email": true})
	if err != nil {
		t.Fatalf("applyBlankJSON returned error: %v", err)
	}
	wantFields := []string{"age", "email", "name", "record_id"}
	if !reflect.DeepEqual(fields, wantFields) {
		t.Errorf("fields = %v, want %v", fields, wantFields)
	}
	rows := []map[string]string{}
	if err := json.Unmarshal(out, &rows); err != nil {
		t.Fatalf("blanked JSON is invalid: %v", err)
	}
	for i, row := range rows {
		if row["name"] != "" || row["email"] != "" {
			t.Errorf("row %d not blanked: %v", i, row)
		}
		if row["record_id"] == "" || row["age"] == "" {
			t.Errorf("row %d lost non-blanked values: %v", i, row)
		}
	}
}

func TestApplyBlankJSONInvalid(t *testing.T) {
	if _, _, err := applyBlankJSON([]byte("not json"), nil); err == nil {
		t.Fatal("expected error for invalid JSON input")
	}
}

func TestFilterMetadataCSV(t *testing.T) {
	out, err := filterMetadataCSV([]byte(testMetadataCSV), []string{"age", "record_id"})
	if err != nil {
		t.Fatalf("filterMetadataCSV returned error: %v", err)
	}
	want := "field_name,form_name,field_type,identifier\n" +
		"record_id,demographics,text,\n" +
		"age,demographics,text,\n"
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
	opts, _ := parsePluginOptions(`{"exportMode":"report","reportId":"7","variables":[{"name":"email","anonymization":"blank"}]}`)
	data, err := makeManifest(opts, "7", "redcap/report-7/data.csv", "redcap/report-7/metadata.csv",
		"redcap/report-7/project_info.json", "redcap/report-7/events.csv", "redcap/report-7/form_event_mapping.csv",
		"14.5.5", []string{"something failed"})
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
	if manifest["redcap_version"] != "14.5.5" {
		t.Errorf("redcap_version = %v, want 14.5.5", manifest["redcap_version"])
	}
	files := manifest["files"].(map[string]interface{})
	if files["events"] != "redcap/report-7/events.csv" || files["form_event_mapping"] != "redcap/report-7/form_event_mapping.csv" {
		t.Errorf("longitudinal files missing from manifest: %v", files)
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
		"redcap/records/project_info.json", "", "", "", nil)
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
	for _, key := range []string{"variables", "warnings"} {
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
