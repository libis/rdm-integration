// Author: Eryk Kulikowski @ KU Leuven (2026). Apache 2.0 License

package redcap2

import (
	"context"
	"encoding/json"
	"integration/app/plugin/types"
	"io"
	"testing"
)

func queryAndRead(t *testing.T, f *fakeRedcap, pluginOpts, path string) ([]byte, map[string]interface{}) {
	t.Helper()
	nodes, err := Query(context.Background(), types.CompareRequest{Url: f.url(), Token: "tok", PluginOptions: pluginOpts}, nil)
	if err != nil {
		t.Fatalf("Query returned error: %v", err)
	}
	streams, err := Streams(context.Background(), nodes, types.StreamParams{Url: f.url(), Token: "tok", PluginOptions: pluginOpts})
	if err != nil {
		t.Fatalf("Streams returned error: %v", err)
	}
	read := func(p string) []byte {
		stream, ok := streams.Streams[p]
		if !ok {
			t.Fatalf("no stream for %s (have %v)", p, nodes)
		}
		reader, err := stream.Open()
		if err != nil {
			t.Fatalf("open %s: %v", p, err)
		}
		data, err := io.ReadAll(reader)
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		return data
	}
	var manifest map[string]interface{}
	base, _ := splitPath(path)
	if err := json.Unmarshal(read(base+"/manifest.json"), &manifest); err != nil {
		t.Fatalf("manifest invalid: %v", err)
	}
	return read(path), manifest
}

// EAV exports must blank by field_name row, not by header, and metadata.csv
// must keep the fields seen in the EAV rows plus the record-ID field.
func TestEndToEndEAVBlanking(t *testing.T) {
	f := newFakeRedcap()
	f.eavCSV = "record,redcap_event_name,field_name,value\n" +
		"1,baseline_arm_1,name,John\n" +
		"1,baseline_arm_1,email,john@example.org\n" +
		"2,baseline_arm_1,email,jane@example.org\n" +
		"2,baseline_arm_1,age,29\n"
	defer f.close()

	pluginOpts := `{
		"exportMode": "records",
		"recordType": "eav",
		"variables": [{"name": "email", "anonymization": "blank"}]
	}`
	data, manifest := queryAndRead(t, f, pluginOpts, "redcap/records/data.csv")

	want := "record,redcap_event_name,field_name,value\n" +
		"1,baseline_arm_1,name,John\n" +
		"1,baseline_arm_1,email,\n" +
		"2,baseline_arm_1,email,\n" +
		"2,baseline_arm_1,age,29\n"
	if string(data) != want {
		t.Errorf("EAV data.csv = %q, want %q", string(data), want)
	}

	audit := manifest["anonymization_audit"].([]interface{})
	entry := audit[0].(map[string]interface{})
	if entry["field"] != "email" || entry["matched"] != float64(2) {
		t.Errorf("audit = %v, want email matched=2", audit)
	}
	if form := f.lastForm("record"); form.Get("type") != "eav" {
		t.Errorf("record export type = %q, want eav", form.Get("type"))
	}
}

// Label-header exports must translate headers through the dictionary so that
// blanking by field name still applies, and metadata.csv keeps all fields.
func TestEndToEndLabelHeaderBlanking(t *testing.T) {
	f := newFakeRedcap()
	f.labelCSV = "Record ID,Full Name,Email Address,Age\n1,John,john@example.org,34\n2,Jane,jane@example.org,29\n"
	defer f.close()

	pluginOpts := `{
		"exportMode": "records",
		"rawOrLabelHeaders": "label",
		"variables": [
			{"name": "name", "anonymization": "blank"},
			{"name": "email", "anonymization": "blank"}
		]
	}`
	data, manifest := queryAndRead(t, f, pluginOpts, "redcap/records/data.csv")

	want := "Record ID,Full Name,Email Address,Age\n1,,,34\n2,,,29\n"
	if string(data) != want {
		t.Errorf("label-header data.csv = %q, want %q", string(data), want)
	}
	if _, ok := manifest["warnings"]; ok {
		t.Errorf("no warnings expected for fully matched blanking, got %v", manifest["warnings"])
	}
	if form := f.lastForm("record"); form.Get("rawOrLabelHeaders") != "label" {
		t.Errorf("rawOrLabelHeaders = %q, want label", form.Get("rawOrLabelHeaders"))
	}
}

// Checkbox fields expand to field___code columns; a blank rule for the base
// field must blank every expansion, and the manifest must document attachments
// (file-upload fields) and dictionary fields missing from the export.
func TestEndToEndCheckboxAndAttachmentManifest(t *testing.T) {
	f := newFakeRedcap()
	f.metadataCSV = "field_name,form_name,field_type,field_label,identifier\n" +
		"record_id,demographics,text,Record ID,\n" +
		"phones,demographics,checkbox,Phone Types,y\n" +
		"consent_scan,demographics,file,Consent Scan,\n"
	f.dataCSV = "record_id,phones___1,phones___2\n1,555-1234,555-5678\n"
	defer f.close()

	pluginOpts := `{
		"exportMode": "records",
		"variables": [{"name": "phones", "anonymization": "blank"}]
	}`
	data, manifest := queryAndRead(t, f, pluginOpts, "redcap/records/data.csv")

	want := "record_id,phones___1,phones___2\n1,,\n"
	if string(data) != want {
		t.Errorf("checkbox data.csv = %q, want %q", string(data), want)
	}

	audit := manifest["anonymization_audit"].([]interface{})
	entry := audit[0].(map[string]interface{})
	if entry["field"] != "phones" || entry["matched"] != float64(2) {
		t.Errorf("audit = %v, want phones matched=2", audit)
	}

	attachments := manifest["attachments"].(map[string]interface{})
	fields := attachments["file_upload_fields"].([]interface{})
	if len(fields) != 1 || fields[0] != "consent_scan" || attachments["exported"] != false {
		t.Errorf("attachments = %v, want consent_scan not exported", attachments)
	}

	notExported := manifest["dictionary_fields_not_exported"].([]interface{})
	if len(notExported) != 1 || notExported[0] != "consent_scan" {
		t.Errorf("dictionary_fields_not_exported = %v, want [consent_scan]", notExported)
	}
}

// Zero-match blank rules must surface as manifest warnings, never silently.
func TestEndToEndZeroMatchBlankWarning(t *testing.T) {
	f := newFakeRedcap()
	defer f.close()

	pluginOpts := `{
		"exportMode": "report",
		"reportId": "7",
		"variables": [{"name": "not_in_report", "anonymization": "blank"}]
	}`
	_, manifest := queryAndRead(t, f, pluginOpts, "redcap/report-7/data.csv")

	warnings, ok := manifest["warnings"].([]interface{})
	if !ok || len(warnings) == 0 {
		t.Fatalf("expected zero-match warning in manifest, got %v", manifest["warnings"])
	}
}

// Bundles above the cache cap must be rebuilt instead of cached.
func TestOversizedBundleIsNotCached(t *testing.T) {
	originalCap := maxCacheableBundleBytes
	maxCacheableBundleBytes = 1 // everything is oversized
	defer func() { maxCacheableBundleBytes = originalCap }()

	f := newFakeRedcap()
	defer f.close()

	pluginOpts := `{"exportMode":"report","reportId":"7"}`
	for i := 0; i < 2; i++ {
		if _, err := Query(context.Background(), types.CompareRequest{Url: f.url(), Token: "tok", PluginOptions: pluginOpts}, nil); err != nil {
			t.Fatalf("Query %d returned error: %v", i, err)
		}
	}
	if got := f.calls("report"); got != 2 {
		t.Fatalf("report called %d times, want 2 (oversized bundle must not be cached)", got)
	}
}
