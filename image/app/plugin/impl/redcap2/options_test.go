// Author: Eryk Kulikowski @ KU Leuven (2026). Apache 2.0 License

package redcap2

import (
	"context"
	"integration/app/plugin/types"
	"strings"
	"testing"
)

func TestOptionsRequiresUrlAndToken(t *testing.T) {
	if _, err := Options(context.Background(), types.OptionsRequest{}); err == nil {
		t.Fatal("expected error for missing url and token")
	}
}

func TestOptionsNonVariablesRequestReturnsEmpty(t *testing.T) {
	f := newFakeRedcap()
	defer f.close()

	items, err := Options(context.Background(), types.OptionsRequest{Url: f.url(), Token: "tok"})
	if err != nil {
		t.Fatalf("Options returned error: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("expected empty option list, got %v", items)
	}
	if f.calls("metadata") != 0 || f.calls("report") != 0 {
		t.Error("no API calls expected for non-variables request")
	}
}

// checkVariableItems asserts the standard field list from the fake server:
// sorted alphabetically with the identifier-tagged fields pre-selected.
func checkVariableItems(t *testing.T, items []types.SelectItem) {
	t.Helper()
	wantFields := []string{"age", "email", "name", "record_id"}
	if len(items) != len(wantFields) {
		t.Fatalf("got %d items (%v), want %d", len(items), items, len(wantFields))
	}
	for i, item := range items {
		if item.Label != wantFields[i] || item.Value != wantFields[i] {
			t.Errorf("item %d = %v, want %s", i, item, wantFields[i])
		}
		wantSelected := wantFields[i] == "email" || wantFields[i] == "name"
		if item.Selected != wantSelected {
			t.Errorf("item %s Selected = %v, want %v (identifier auto-detection)", item.Label, item.Selected, wantSelected)
		}
	}
}

func TestOptionsVariablesRecordsMode(t *testing.T) {
	f := newFakeRedcap()
	defer f.close()

	items, err := Options(context.Background(), types.OptionsRequest{
		Url:           f.url(),
		Token:         "tok",
		PluginOptions: `{"exportMode":"records","request":"variables"}`,
	})
	if err != nil {
		t.Fatalf("Options returned error: %v", err)
	}
	checkVariableItems(t, items)
	if f.calls("report") != 0 {
		t.Error("records mode variable lookup must not call the report endpoint")
	}
}

func TestOptionsVariablesReportMode(t *testing.T) {
	f := newFakeRedcap()
	defer f.close()

	items, err := Options(context.Background(), types.OptionsRequest{
		Url:           f.url(),
		Token:         "tok",
		PluginOptions: `{"request":"variables","reportId":"7"}`,
	})
	if err != nil {
		t.Fatalf("Options returned error: %v", err)
	}
	checkVariableItems(t, items)
	form := f.lastForm("report")
	if form.Get("report_id") != "7" {
		t.Errorf("report header request report_id = %q, want 7", form.Get("report_id"))
	}
}

func TestOptionsVariablesReportModeFallsBackToMetadata(t *testing.T) {
	f := newFakeRedcap()
	f.failReport = true
	defer f.close()

	items, err := Options(context.Background(), types.OptionsRequest{
		Url:           f.url(),
		Token:         "tok",
		PluginOptions: `{"request":"variables","reportId":"7"}`,
	})
	if err != nil {
		t.Fatalf("Options returned error: %v", err)
	}
	checkVariableItems(t, items)
}

func TestOptionsVariablesReportModeMissingReportID(t *testing.T) {
	f := newFakeRedcap()
	defer f.close()

	_, err := Options(context.Background(), types.OptionsRequest{
		Url:           f.url(),
		Token:         "tok",
		PluginOptions: `{"request":"variables"}`,
	})
	if err == nil || !strings.Contains(err.Error(), "missing report id") {
		t.Fatalf("expected missing report id error, got %v", err)
	}
}

func TestOptionsVariablesReportModeUsesOptionFallback(t *testing.T) {
	f := newFakeRedcap()
	defer f.close()

	items, err := Options(context.Background(), types.OptionsRequest{
		Url:           f.url(),
		Token:         "tok",
		Option:        "9",
		PluginOptions: `{"request":"variables"}`,
	})
	if err != nil {
		t.Fatalf("Options returned error: %v", err)
	}
	checkVariableItems(t, items)
	if form := f.lastForm("report"); form.Get("report_id") != "9" {
		t.Errorf("report header request report_id = %q, want 9 (from Option)", form.Get("report_id"))
	}
}
