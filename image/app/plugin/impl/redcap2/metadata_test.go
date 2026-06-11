// Author: Eryk Kulikowski @ KU Leuven (2026). Apache 2.0 License

package redcap2

import (
	"context"
	"integration/app/plugin/types"
	"reflect"
	"strings"
	"testing"
)

func TestMetadataRequiresUrlAndToken(t *testing.T) {
	if _, err := Metadata(context.Background(), types.StreamParams{}); err == nil {
		t.Fatal("expected error for missing url and token")
	}
}

func TestMetadataMapsProjectInfo(t *testing.T) {
	f := newFakeRedcap()
	f.projectJSON = `{
		"project_id": 42,
		"project_title": "Hypertension Cohort",
		"project_notes": "Longitudinal cohort of hypertension patients.",
		"purpose": "2",
		"purpose_other": "Research on blood pressure",
		"project_pi_firstname": "Ada",
		"project_pi_lastname": "Lovelace",
		"project_irb_number": "IRB-2026-007",
		"project_grant_number": "G0A1234N",
		"is_longitudinal": "0"
	}`
	defer f.close()

	meta, err := Metadata(context.Background(), types.StreamParams{Url: f.url(), Token: "tok"})
	if err != nil {
		t.Fatalf("Metadata returned error: %v", err)
	}

	if meta.Title != "Hypertension Cohort" {
		t.Errorf("Title = %q", meta.Title)
	}
	wantDescription := []string{
		"Longitudinal cohort of hypertension patients.",
		"Purpose: Research on blood pressure",
	}
	if !reflect.DeepEqual(meta.DsDescription, wantDescription) {
		t.Errorf("DsDescription = %v, want %v", meta.DsDescription, wantDescription)
	}
	if len(meta.Author) != 1 || meta.Author[0].AuthorName != "Lovelace, Ada" {
		t.Errorf("Author = %v, want PI Lovelace, Ada", meta.Author)
	}
	if len(meta.GrantNumber) != 1 || meta.GrantNumber[0].GrantNumberValue != "G0A1234N" {
		t.Errorf("GrantNumber = %v", meta.GrantNumber)
	}
	if len(meta.OtherId) != 2 || meta.OtherId[0].OtherIdAgency != "IRB" || meta.OtherId[0].OtherIdValue != "IRB-2026-007" {
		t.Errorf("OtherId = %v", meta.OtherId)
	}
	if !strings.Contains(meta.OtherId[1].OtherIdValue, "project:42") || meta.OtherId[1].OtherIdAgency != "REDCap" {
		t.Errorf("REDCap project id reference = %v", meta.OtherId[1])
	}
}

func TestMetadataMinimalProjectInfo(t *testing.T) {
	f := newFakeRedcap()
	defer f.close()

	meta, err := Metadata(context.Background(), types.StreamParams{Url: f.url(), Token: "tok"})
	if err != nil {
		t.Fatalf("Metadata returned error: %v", err)
	}
	if meta.Title != "Demo" {
		t.Errorf("Title = %q, want Demo", meta.Title)
	}
	if len(meta.Author) != 0 || len(meta.GrantNumber) != 0 || len(meta.DsDescription) != 0 {
		t.Errorf("minimal project should map only title and project id, got %+v", meta)
	}
	if len(meta.OtherId) != 1 || meta.OtherId[0].OtherIdAgency != "REDCap" {
		t.Errorf("OtherId = %v, want only the REDCap project reference", meta.OtherId)
	}
}

func TestMetadataLastNameOnlyPI(t *testing.T) {
	f := newFakeRedcap()
	f.projectJSON = `{"project_id":1,"project_title":"T","project_pi_lastname":"Curie"}`
	defer f.close()

	meta, err := Metadata(context.Background(), types.StreamParams{Url: f.url(), Token: "tok"})
	if err != nil {
		t.Fatalf("Metadata returned error: %v", err)
	}
	if len(meta.Author) != 1 || meta.Author[0].AuthorName != "Curie" {
		t.Errorf("Author = %v, want Curie", meta.Author)
	}
}
