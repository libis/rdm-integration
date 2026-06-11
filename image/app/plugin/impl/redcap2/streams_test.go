// Author: Eryk Kulikowski @ KU Leuven (2026). Apache 2.0 License

package redcap2

import (
	"context"
	"encoding/json"
	"integration/app/plugin/types"
	"integration/app/tree"
	"io"
	"strings"
	"testing"
)

func TestStreamsRequiresUrlAndToken(t *testing.T) {
	if _, err := Streams(context.Background(), nil, types.StreamParams{}); err == nil {
		t.Fatal("expected error for missing url and token")
	}
}

func TestStreamsRequiresReportIDInReportMode(t *testing.T) {
	f := newFakeRedcap()
	defer f.close()

	_, err := Streams(context.Background(), nil, types.StreamParams{Url: f.url(), Token: "tok"})
	if err == nil || !strings.Contains(err.Error(), "missing report id") {
		t.Fatalf("expected missing report id error, got %v", err)
	}
}

func readStream(t *testing.T, stream types.Stream) []byte {
	t.Helper()
	reader, err := stream.Open()
	if err != nil {
		t.Fatalf("stream open failed: %v", err)
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("stream read failed: %v", err)
	}
	if err := stream.Close(); err != nil {
		t.Fatalf("stream close failed: %v", err)
	}
	return data
}

func TestStreamsServesQueryBundleFromCache(t *testing.T) {
	f := newFakeRedcap()
	defer f.close()

	pluginOpts := `{
		"exportMode": "report",
		"reportId": "7",
		"generatedAt": "2026-06-11T00:00:00Z",
		"variables": [
			{"name": "name", "anonymization": "blank"},
			{"name": "email", "anonymization": "blank"}
		]
	}`
	nodes, err := Query(context.Background(), types.CompareRequest{Url: f.url(), Token: "tok", PluginOptions: pluginOpts}, nil)
	if err != nil {
		t.Fatalf("Query returned error: %v", err)
	}

	streams, err := Streams(context.Background(), nodes, types.StreamParams{Url: f.url(), Token: "tok", PluginOptions: pluginOpts})
	if err != nil {
		t.Fatalf("Streams returned error: %v", err)
	}

	for path, node := range nodes {
		stream, ok := streams.Streams[path]
		if !ok {
			t.Errorf("no stream for %s", path)
			continue
		}
		data := readStream(t, stream)
		if md5Hex(data) != node.Attributes.RemoteHash {
			t.Errorf("stream bytes for %s do not match Query hash", path)
		}
		if int64(len(data)) != node.Attributes.RemoteFileSize {
			t.Errorf("stream size for %s = %d, want %d", path, len(data), node.Attributes.RemoteFileSize)
		}
	}

	wantData := "record_id,name,email,age\n1,,,34\n2,,,29\n"
	gotData := readStream(t, streams.Streams["redcap/report-7/data.csv"])
	if string(gotData) != wantData {
		t.Errorf("blanked data.csv = %q, want %q", string(gotData), wantData)
	}

	manifestBytes := readStream(t, streams.Streams["redcap/report-7/manifest.json"])
	manifest := map[string]interface{}{}
	if err := json.Unmarshal(manifestBytes, &manifest); err != nil {
		t.Fatalf("manifest is invalid JSON: %v", err)
	}
	if manifest["plugin"] != "redcap2" || manifest["report_id"] != "7" {
		t.Errorf("unexpected manifest identity: %v", manifest)
	}
	if manifest["redcap_version"] != testVersion {
		t.Errorf("redcap_version = %v, want %s", manifest["redcap_version"], testVersion)
	}
	if manifest["generated_at"] != "2026-06-11T00:00:00Z" {
		t.Errorf("generated_at = %v, want propagated value", manifest["generated_at"])
	}
	project, ok := manifest["project"].(map[string]interface{})
	if !ok || project["title"] != "Demo" {
		t.Errorf("manifest project identity = %v, want title Demo", manifest["project"])
	}
	audit, ok := manifest["anonymization_audit"].([]interface{})
	if !ok || len(audit) != 2 {
		t.Errorf("anonymization_audit = %v, want 2 entries (name, email)", manifest["anonymization_audit"])
	}

	// The bundle built during Query must be reused by Streams (single build).
	for _, content := range []string{"report", "metadata", "project", "version"} {
		if got := f.calls(content); got != 1 {
			t.Errorf("content=%s called %d times, want 1 (bundle cache miss?)", content, got)
		}
	}
}

func TestStreamsRecordsModeJSONBlanking(t *testing.T) {
	f := newFakeRedcap()
	defer f.close()

	pluginOpts := `{
		"exportMode": "records",
		"dataFormat": "json",
		"variables": [
			{"name": "name", "anonymization": "blank"},
			{"name": "email", "anonymization": "blank"}
		]
	}`
	nodes, err := Query(context.Background(), types.CompareRequest{Url: f.url(), Token: "tok", PluginOptions: pluginOpts}, nil)
	if err != nil {
		t.Fatalf("Query returned error: %v", err)
	}
	if _, ok := nodes["redcap/records/data.json"]; !ok {
		t.Fatalf("expected redcap/records/data.json, got %v", nodes)
	}

	streams, err := Streams(context.Background(), nodes, types.StreamParams{Url: f.url(), Token: "tok", PluginOptions: pluginOpts})
	if err != nil {
		t.Fatalf("Streams returned error: %v", err)
	}

	rows := []map[string]string{}
	if err := json.Unmarshal(readStream(t, streams.Streams["redcap/records/data.json"]), &rows); err != nil {
		t.Fatalf("data.json is invalid JSON: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("expected 2 records, got %d", len(rows))
	}
	for i, row := range rows {
		if row["name"] != "" || row["email"] != "" {
			t.Errorf("record %d not blanked: %v", i, row)
		}
	}

	manifest := map[string]interface{}{}
	if err := json.Unmarshal(readStream(t, streams.Streams["redcap/records/manifest.json"]), &manifest); err != nil {
		t.Fatalf("manifest is invalid JSON: %v", err)
	}
	if _, ok := manifest["report_id"]; ok {
		t.Error("records-mode manifest should not contain report_id")
	}
	if form := f.lastForm("record"); form.Get("format") != "json" {
		t.Errorf("record export format = %q, want json", form.Get("format"))
	}
}

func TestStreamsUnknownGeneratedFile(t *testing.T) {
	f := newFakeRedcap()
	defer f.close()

	in := map[string]tree.Node{
		"redcap/report-7/nope.csv": {
			Id: "redcap/report-7/nope.csv",
			Attributes: tree.Attributes{
				URL:    "redcap/report-7/nope.csv",
				IsFile: true,
			},
		},
	}
	_, err := Streams(context.Background(), in, types.StreamParams{
		Url:           f.url(),
		Token:         "tok",
		PluginOptions: `{"exportMode":"report","reportId":"7"}`,
	})
	if err == nil || !strings.Contains(err.Error(), "generated file not found") {
		t.Fatalf("expected generated file not found error, got %v", err)
	}
}
