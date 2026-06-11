// Author: Eryk Kulikowski @ KU Leuven (2026). Apache 2.0 License

package redcap2

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
)

const (
	testDataCSV = "record_id,name,email,age\n1,John,john@example.org,34\n2,Jane,jane@example.org,29\n"

	testDataJSON = `[{"record_id":"1","name":"John","email":"john@example.org","age":"34"},{"record_id":"2","name":"Jane","email":"jane@example.org","age":"29"}]`

	testMetadataCSV = "field_name,form_name,field_type,identifier\n" +
		"record_id,demographics,text,\n" +
		"name,demographics,text,y\n" +
		"email,demographics,text,y\n" +
		"age,demographics,text,\n"

	testEventsCSV  = "event_name,arm_num,unique_event_name\nBaseline,1,baseline_arm_1\n"
	testMappingCSV = "arm_num,unique_event_name,form\n1,baseline_arm_1,demographics\n"
	testVersion    = "14.5.5"
)

// fakeRedcap is a minimal in-memory REDCap API stub. It records every form
// submitted per content type so tests can assert on the exact parameters sent.
type fakeRedcap struct {
	mu           sync.Mutex
	forms        map[string][]url.Values
	longitudinal bool
	failReport   bool
	server       *httptest.Server
}

func newFakeRedcap() *fakeRedcap {
	f := &fakeRedcap{forms: map[string][]url.Values{}}
	f.server = httptest.NewServer(http.HandlerFunc(f.handle))
	return f
}

func (f *fakeRedcap) close() { f.server.Close() }

func (f *fakeRedcap) url() string { return f.server.URL }

func (f *fakeRedcap) calls(content string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.forms[content])
}

func (f *fakeRedcap) lastForm(content string) url.Values {
	f.mu.Lock()
	defer f.mu.Unlock()
	forms := f.forms[content]
	if len(forms) == 0 {
		return nil
	}
	return forms[len(forms)-1]
}

func (f *fakeRedcap) handle(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	form := url.Values{}
	for k, v := range r.PostForm {
		form[k] = append([]string(nil), v...)
	}
	content := form.Get("content")
	f.mu.Lock()
	f.forms[content] = append(f.forms[content], form)
	longitudinal := f.longitudinal
	failReport := f.failReport
	f.mu.Unlock()

	switch content {
	case "report":
		if failReport {
			http.Error(w, "report unavailable", http.StatusInternalServerError)
			return
		}
		writeTestData(w, form)
	case "record":
		writeTestData(w, form)
	case "metadata":
		_, _ = w.Write([]byte(testMetadataCSV))
	case "project":
		longitudinalFlag := "0"
		if longitudinal {
			longitudinalFlag = "1"
		}
		_, _ = w.Write([]byte(`{"project_id":1,"project_title":"Demo","is_longitudinal":"` + longitudinalFlag + `"}`))
	case "version":
		_, _ = w.Write([]byte(testVersion))
	case "event":
		_, _ = w.Write([]byte(testEventsCSV))
	case "formEventMapping":
		_, _ = w.Write([]byte(testMappingCSV))
	default:
		http.Error(w, "unsupported content: "+content, http.StatusBadRequest)
	}
}

func writeTestData(w http.ResponseWriter, form url.Values) {
	if form.Get("format") == "json" {
		_, _ = w.Write([]byte(testDataJSON))
		return
	}
	data := testDataCSV
	if form.Get("csvDelimiter") == "tab" {
		data = strings.ReplaceAll(data, ",", "\t")
	}
	_, _ = w.Write([]byte(data))
}
