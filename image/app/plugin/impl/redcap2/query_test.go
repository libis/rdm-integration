// Author: Eryk Kulikowski @ KU Leuven (2026). Apache 2.0 License

package redcap2

import (
	"context"
	"integration/app/plugin/types"
	"sort"
	"strings"
	"testing"
)

func TestQueryRequiresUrlAndToken(t *testing.T) {
	if _, err := Query(context.Background(), types.CompareRequest{}, nil); err == nil {
		t.Fatal("expected error for missing url and token")
	}
}

func TestQueryRequiresReportIDInReportMode(t *testing.T) {
	f := newFakeRedcap()
	defer f.close()

	_, err := Query(context.Background(), types.CompareRequest{Url: f.url(), Token: "tok"}, nil)
	if err == nil || !strings.Contains(err.Error(), "missing report id") {
		t.Fatalf("expected missing report id error, got %v", err)
	}
	if f.calls("report") != 0 {
		t.Error("no API calls expected when report id is missing")
	}
}

func TestQueryReportModeGeneratesBundle(t *testing.T) {
	f := newFakeRedcap()
	defer f.close()

	nodes, err := Query(context.Background(), types.CompareRequest{
		Url:           f.url(),
		Token:         "tok",
		PluginOptions: `{"exportMode":"report","reportId":"7"}`,
	}, nil)
	if err != nil {
		t.Fatalf("Query returned error: %v", err)
	}

	wantPaths := []string{
		"redcap/report-7/data.csv",
		"redcap/report-7/manifest.json",
		"redcap/report-7/metadata.csv",
		"redcap/report-7/project_info.json",
	}
	gotPaths := make([]string, 0, len(nodes))
	for path := range nodes {
		gotPaths = append(gotPaths, path)
	}
	sort.Strings(gotPaths)
	if strings.Join(gotPaths, "|") != strings.Join(wantPaths, "|") {
		t.Fatalf("paths = %v, want %v", gotPaths, wantPaths)
	}

	node := nodes["redcap/report-7/data.csv"]
	if node.Name != "data.csv" || node.Path != "redcap/report-7" || !node.Attributes.IsFile {
		t.Errorf("unexpected node shape: %+v", node)
	}
	if node.Attributes.RemoteHashType != types.Md5 {
		t.Errorf("RemoteHashType = %q, want %q", node.Attributes.RemoteHashType, types.Md5)
	}
	if node.Attributes.RemoteHash != md5Hex([]byte(testDataCSV)) {
		t.Errorf("RemoteHash = %q, want md5 of report data", node.Attributes.RemoteHash)
	}
	if node.Attributes.RemoteFileSize != int64(len(testDataCSV)) {
		t.Errorf("RemoteFileSize = %d, want %d", node.Attributes.RemoteFileSize, len(testDataCSV))
	}

	form := f.lastForm("report")
	if form.Get("report_id") != "7" || form.Get("type") != "flat" {
		t.Errorf("unexpected report form: %v", form)
	}
	if f.calls("record") != 0 {
		t.Error("report mode must not call the record endpoint")
	}
}

func TestQueryReportModeOmitsRecordOnlyFilters(t *testing.T) {
	f := newFakeRedcap()
	defer f.close()

	_, err := Query(context.Background(), types.CompareRequest{
		Url:   f.url(),
		Token: "tok",
		PluginOptions: `{
			"exportMode": "report",
			"reportId": "7",
			"fields": ["name"],
			"forms": ["demographics"],
			"events": ["baseline_arm_1"],
			"records": ["1"],
			"filterLogic": "[age] > 30",
			"dateRangeBegin": "2026-01-01",
			"dateRangeEnd": "2026-01-31",
			"exportSurveyFields": true,
			"exportDataAccessGroups": true
		}`,
	}, nil)
	if err != nil {
		t.Fatalf("Query returned error: %v", err)
	}

	form := f.lastForm("report")
	for _, key := range []string{
		"fields", "forms", "events", "records", "filterLogic",
		"dateRangeBegin", "dateRangeEnd", "exportSurveyFields", "exportDataAccessGroups",
	} {
		if _, ok := form[key]; ok {
			t.Errorf("record-only parameter %q must not be sent to content=report", key)
		}
	}
}

func TestQueryRecordsModeSendsFilters(t *testing.T) {
	f := newFakeRedcap()
	defer f.close()

	nodes, err := Query(context.Background(), types.CompareRequest{
		Url:   f.url(),
		Token: "tok",
		PluginOptions: `{
			"exportMode": "records",
			"recordType": "eav",
			"csvDelimiter": "tab",
			"rawOrLabel": "label",
			"rawOrLabelHeaders": "label",
			"fields": ["age", "name"],
			"forms": ["demographics"],
			"events": ["baseline_arm_1"],
			"records": ["1", "2"],
			"filterLogic": "[age] > 20",
			"dateRangeBegin": "2026-01-02",
			"dateRangeEnd": "2026-01-31",
			"exportSurveyFields": true,
			"exportDataAccessGroups": true
		}`,
	}, nil)
	if err != nil {
		t.Fatalf("Query returned error: %v", err)
	}

	if _, ok := nodes["redcap/records/data.csv"]; !ok {
		t.Errorf("records mode should generate redcap/records paths, got %v", nodes)
	}

	form := f.lastForm("record")
	want := map[string]string{
		"type":                   "eav",
		"csvDelimiter":           "tab",
		"rawOrLabel":             "label",
		"rawOrLabelHeaders":      "label",
		"fields":                 "age,name",
		"forms":                  "demographics",
		"events":                 "baseline_arm_1",
		"records":                "1,2",
		"filterLogic":            "[age] > 20",
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
	if f.calls("report") != 0 {
		t.Error("records mode must not call the report endpoint")
	}
}

func TestQueryLongitudinalAddsEventFiles(t *testing.T) {
	f := newFakeRedcap()
	f.longitudinal = true
	defer f.close()

	nodes, err := Query(context.Background(), types.CompareRequest{
		Url:           f.url(),
		Token:         "tok",
		PluginOptions: `{"exportMode":"report","reportId":"7"}`,
	}, nil)
	if err != nil {
		t.Fatalf("Query returned error: %v", err)
	}
	if len(nodes) != 6 {
		t.Fatalf("expected 6 files for longitudinal project, got %d", len(nodes))
	}
	events, ok := nodes["redcap/report-7/events.csv"]
	if !ok || events.Attributes.RemoteHash != md5Hex([]byte(testEventsCSV)) {
		t.Errorf("events.csv missing or wrong hash: %+v", events)
	}
	mapping, ok := nodes["redcap/report-7/form_event_mapping.csv"]
	if !ok || mapping.Attributes.RemoteHash != md5Hex([]byte(testMappingCSV)) {
		t.Errorf("form_event_mapping.csv missing or wrong hash: %+v", mapping)
	}
}

func TestQueryUsesOptionAsReportIDFallback(t *testing.T) {
	f := newFakeRedcap()
	defer f.close()

	nodes, err := Query(context.Background(), types.CompareRequest{
		Url:    f.url(),
		Token:  "tok",
		Option: "9",
	}, nil)
	if err != nil {
		t.Fatalf("Query returned error: %v", err)
	}
	if _, ok := nodes["redcap/report-9/data.csv"]; !ok {
		t.Errorf("expected report id from Option to drive paths, got %v", nodes)
	}
}

func TestQueryHashesAreDeterministicAcrossServers(t *testing.T) {
	f1 := newFakeRedcap()
	defer f1.close()
	f2 := newFakeRedcap()
	defer f2.close()

	pluginOpts := `{"exportMode":"report","reportId":"7","variables":[{"name":"email","anonymization":"blank"}]}`
	nodes1, err := Query(context.Background(), types.CompareRequest{Url: f1.url(), Token: "tok", PluginOptions: pluginOpts}, nil)
	if err != nil {
		t.Fatalf("first Query returned error: %v", err)
	}
	nodes2, err := Query(context.Background(), types.CompareRequest{Url: f2.url(), Token: "tok", PluginOptions: pluginOpts}, nil)
	if err != nil {
		t.Fatalf("second Query returned error: %v", err)
	}

	if len(nodes1) != len(nodes2) {
		t.Fatalf("node counts differ: %d vs %d", len(nodes1), len(nodes2))
	}
	for path, node1 := range nodes1 {
		node2, ok := nodes2[path]
		if !ok {
			t.Errorf("path %s missing in second result", path)
			continue
		}
		if node1.Attributes.RemoteHash != node2.Attributes.RemoteHash {
			t.Errorf("hash for %s differs between identical exports", path)
		}
	}
}

func TestQueryReportExportFailure(t *testing.T) {
	f := newFakeRedcap()
	f.failReport = true
	defer f.close()

	_, err := Query(context.Background(), types.CompareRequest{
		Url:           f.url(),
		Token:         "tok",
		PluginOptions: `{"exportMode":"report","reportId":"7"}`,
	}, nil)
	if err == nil || !strings.Contains(err.Error(), "report export failed") {
		t.Fatalf("expected report export failure, got %v", err)
	}
}

func TestSplitPath(t *testing.T) {
	tests := []struct {
		in         string
		wantParent string
		wantName   string
	}{
		{in: "redcap/report-7/data.csv", wantParent: "redcap/report-7", wantName: "data.csv"},
		{in: "file.csv", wantParent: "", wantName: "file.csv"},
		{in: "", wantParent: "", wantName: ""},
		{in: " redcap/x ", wantParent: "redcap", wantName: "x"},
	}
	for _, tt := range tests {
		parent, name := splitPath(tt.in)
		if parent != tt.wantParent || name != tt.wantName {
			t.Errorf("splitPath(%q) = (%q, %q), want (%q, %q)", tt.in, parent, name, tt.wantParent, tt.wantName)
		}
	}
}
