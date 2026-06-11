// Author: Eryk Kulikowski @ KU Leuven (2026). Apache 2.0 License

package redcap2

import (
	"bufio"
	"bytes"
	"context"
	"crypto/md5"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"integration/app/logging"
	"integration/app/plugin/types"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
)

type variableOption struct {
	Name          string `json:"name"`
	Anonymization string `json:"anonymization"`
}

type pluginOptions struct {
	ExportMode             string           `json:"exportMode"` // "report" or "records"
	Request                string           `json:"request"`
	ReportID               string           `json:"reportId"`
	DataFormat             string           `json:"dataFormat"`
	Fields                 []string         `json:"fields"`
	Forms                  []string         `json:"forms"`
	Events                 []string         `json:"events"`
	Records                []string         `json:"records"`
	FilterLogic            string           `json:"filterLogic"`
	DateRangeBegin         string           `json:"dateRangeBegin"`
	DateRangeEnd           string           `json:"dateRangeEnd"`
	RecordType             string           `json:"recordType"`
	CsvDelimiter           string           `json:"csvDelimiter"`
	RawOrLabel             string           `json:"rawOrLabel"`
	RawOrLabelHeaders      string           `json:"rawOrLabelHeaders"`
	ExportSurveyFields     bool             `json:"exportSurveyFields"`
	ExportDataAccessGroups bool             `json:"exportDataAccessGroups"`
	Variables              []variableOption `json:"variables"`
	GeneratedAt            string           `json:"generatedAt"`
}

type generatedBundle struct {
	ReportID string
	Files    map[string][]byte
}

var (
	httpClient *http.Client
	clientOnce sync.Once
)

// bundleCacheEntry holds a cached export bundle and its expiry time.
type bundleCacheEntry struct {
	bundle    generatedBundle
	expiresAt time.Time
}

// bundleStore is a simple TTL cache for generated bundles.
type bundleStore struct {
	mu      sync.Mutex
	entries map[string]bundleCacheEntry
}

const bundleCacheTTL = 5 * time.Minute

// maxCacheableBundleBytes bounds how much exported (potentially sensitive)
// data a single bundle may pin in process memory; larger bundles are rebuilt
// on demand instead of cached. Variable so tests can lower it.
var maxCacheableBundleBytes = 64 << 20

var globalBundleCache = &bundleStore{entries: make(map[string]bundleCacheEntry)}

func (b generatedBundle) size() int {
	total := 0
	for _, data := range b.Files {
		total += len(data)
	}
	return total
}

func (s *bundleStore) get(key string) (generatedBundle, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	entry, ok := s.entries[key]
	if !ok || time.Now().After(entry.expiresAt) {
		delete(s.entries, key)
		return generatedBundle{}, false
	}
	return entry.bundle, true
}

func (s *bundleStore) set(key string, b generatedBundle) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Lazy eviction: sweep expired entries on every set to prevent unbounded growth.
	now := time.Now()
	for k, entry := range s.entries {
		if now.After(entry.expiresAt) {
			delete(s.entries, k)
		}
	}
	s.entries[key] = bundleCacheEntry{bundle: b, expiresAt: now.Add(bundleCacheTTL)}
}

func getHTTPClient() *http.Client {
	clientOnce.Do(func() {
		httpClient = &http.Client{
			Timeout: 5 * time.Minute,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
				DisableKeepAlives:   false,
			},
		}
	})
	return httpClient
}

func parsePluginOptions(raw string) (pluginOptions, error) {
	opts := pluginOptions{
		ExportMode:        "report",
		DataFormat:        "csv",
		RecordType:        "flat",
		CsvDelimiter:      ",",
		RawOrLabel:        "raw",
		RawOrLabelHeaders: "raw",
		GeneratedAt:       "missing-generated-at",
	}
	if strings.TrimSpace(raw) == "" {
		return opts, nil
	}
	if err := json.Unmarshal([]byte(raw), &opts); err != nil {
		return pluginOptions{}, fmt.Errorf("invalid pluginOptions JSON: %w", err)
	}
	normalizePluginOptions(&opts)
	return opts, nil
}

func normalizePluginOptions(opts *pluginOptions) {
	switch strings.ToLower(strings.TrimSpace(opts.ExportMode)) {
	case "records":
		opts.ExportMode = "records"
	default:
		opts.ExportMode = "report"
	}

	opts.Request = strings.TrimSpace(opts.Request)
	opts.ReportID = strings.TrimSpace(opts.ReportID)
	opts.FilterLogic = strings.TrimSpace(opts.FilterLogic)
	opts.DateRangeBegin = strings.TrimSpace(opts.DateRangeBegin)
	opts.DateRangeEnd = strings.TrimSpace(opts.DateRangeEnd)
	if strings.TrimSpace(opts.GeneratedAt) == "" {
		opts.GeneratedAt = "missing-generated-at"
	}

	switch strings.ToLower(strings.TrimSpace(opts.DataFormat)) {
	case "json":
		opts.DataFormat = "json"
	default:
		opts.DataFormat = "csv"
	}

	switch strings.ToLower(strings.TrimSpace(opts.RecordType)) {
	case "eav":
		opts.RecordType = "eav"
	default:
		opts.RecordType = "flat"
	}

	switch strings.ToLower(strings.TrimSpace(opts.CsvDelimiter)) {
	case "tab", "\\t", "tsv":
		opts.CsvDelimiter = "\t"
	default:
		opts.CsvDelimiter = ","
	}

	// "both" is not a real REDCap API value (PyCap docstring fossil) — normalize to raw.
	switch strings.ToLower(strings.TrimSpace(opts.RawOrLabel)) {
	case "label":
		opts.RawOrLabel = "label"
	default:
		opts.RawOrLabel = "raw"
	}

	switch strings.ToLower(strings.TrimSpace(opts.RawOrLabelHeaders)) {
	case "label":
		opts.RawOrLabelHeaders = "label"
	default:
		opts.RawOrLabelHeaders = "raw"
	}

	opts.Fields = normalizeStringSlice(opts.Fields)
	opts.Forms = normalizeStringSlice(opts.Forms)
	opts.Events = normalizeStringSlice(opts.Events)
	opts.Records = normalizeStringSlice(opts.Records)
	for i := range opts.Variables {
		opts.Variables[i].Name = strings.TrimSpace(opts.Variables[i].Name)
		switch strings.ToLower(strings.TrimSpace(opts.Variables[i].Anonymization)) {
		case "blank":
			opts.Variables[i].Anonymization = "blank"
		default:
			opts.Variables[i].Anonymization = "none"
		}
	}
}

func normalizeStringSlice(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, 0, len(in))
	seen := make(map[string]bool, len(in))
	for _, raw := range in {
		v := strings.TrimSpace(raw)
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func getAPIURL(baseURL string) string {
	base := strings.TrimSpace(baseURL)
	if strings.HasSuffix(base, "/api") {
		return base + "/"
	}
	if strings.HasSuffix(base, "/api/") {
		return base
	}
	return strings.TrimSuffix(base, "/") + "/api/"
}

func redcapRequest(ctx context.Context, baseURL string, form url.Values) ([]byte, error) {
	apiURL := getAPIURL(baseURL)
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		apiURL,
		bytes.NewBufferString(form.Encode()),
	)
	if err != nil {
		return nil, err
	}
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Add("Accept", "*/*")

	resp, err := getHTTPClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("redcap request failed: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read redcap response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("redcap request failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	trimmed := strings.TrimSpace(string(body))
	if strings.HasPrefix(strings.ToUpper(trimmed), "ERROR") {
		return nil, fmt.Errorf("redcap error: %s", trimmed)
	}
	return body, nil
}

func baseForm(token, content, format string) url.Values {
	form := url.Values{}
	form.Set("token", token)
	form.Set("content", content)
	form.Set("format", format)
	form.Set("returnFormat", "json")
	return form
}

// isEAV reports whether the export produces EAV-shaped output.
// Only records-mode exports have a type parameter; reports are always flat.
func isEAV(opts pluginOptions) bool {
	return opts.ExportMode == "records" && opts.RecordType == "eav"
}

// headersAreLabels reports whether the export's column headers will be field
// labels instead of field names. rawOrLabelHeaders only applies to flat CSV.
func headersAreLabels(opts pluginOptions) bool {
	return opts.DataFormat == "csv" && opts.RawOrLabelHeaders == "label" && !isEAV(opts)
}

// applySharedExportParams sets parameters valid for both content=report and
// content=record. Note: content=report has no "type" parameter (reports are
// always flat) — type is set in applyRecordOnlyFilters.
func applySharedExportParams(form url.Values, opts pluginOptions) {
	if opts.DataFormat == "csv" && opts.CsvDelimiter == "\t" {
		form.Set("csvDelimiter", "tab")
	}
	if opts.RawOrLabel == "label" {
		form.Set("rawOrLabel", "label")
	}
	if headersAreLabels(opts) {
		form.Set("rawOrLabelHeaders", "label")
	}
}

// applyRecordOnlyFilters sets parameters only valid for content=record exports.
// These parameters are not supported by the content=report endpoint.
func applyRecordOnlyFilters(form url.Values, opts pluginOptions) {
	if opts.RecordType != "" {
		form.Set("type", opts.RecordType)
	}
	if len(opts.Fields) > 0 {
		form.Set("fields", strings.Join(opts.Fields, ","))
	}
	if len(opts.Forms) > 0 {
		form.Set("forms", strings.Join(opts.Forms, ","))
	}
	if len(opts.Events) > 0 {
		form.Set("events", strings.Join(opts.Events, ","))
	}
	if len(opts.Records) > 0 {
		form.Set("records", strings.Join(opts.Records, ","))
	}
	if opts.ExportSurveyFields {
		form.Set("exportSurveyFields", "true")
	}
	if opts.ExportDataAccessGroups {
		form.Set("exportDataAccessGroups", "true")
	}
	if opts.FilterLogic != "" {
		form.Set("filterLogic", opts.FilterLogic)
	}
	if opts.DateRangeBegin != "" {
		v := opts.DateRangeBegin
		if len(v) == 10 { // YYYY-MM-DD without time component
			v += " 00:00:00"
		}
		form.Set("dateRangeBegin", v)
	}
	if opts.DateRangeEnd != "" {
		v := opts.DateRangeEnd
		if len(v) == 10 {
			v += " 23:59:59"
		}
		form.Set("dateRangeEnd", v)
	}
}

func reportDelimiter(opts pluginOptions) rune {
	if opts.CsvDelimiter == "\t" {
		return '\t'
	}
	return ','
}

func blankFields(opts pluginOptions) map[string]bool {
	res := map[string]bool{}
	for _, v := range opts.Variables {
		if v.Name == "" {
			continue
		}
		if v.Anonymization == "blank" {
			res[v.Name] = true
		}
	}
	return res
}

func parseCSV(data []byte, delimiter rune) ([][]string, error) {
	reader := csv.NewReader(bytes.NewReader(data))
	reader.Comma = delimiter
	reader.FieldsPerRecord = -1
	return reader.ReadAll()
}

func writeCSV(rows [][]string, delimiter rune) ([]byte, error) {
	var b bytes.Buffer
	writer := csv.NewWriter(&b)
	writer.Comma = delimiter
	if err := writer.WriteAll(rows); err != nil {
		return nil, err
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// dictionary holds the parsed data-dictionary information needed for blanking,
// label-header translation, metadata filtering, and manifest documentation.
type dictionary struct {
	fieldOrder  []string            // field names in dictionary order
	fieldType   map[string]string   // field_name -> field_type
	labelFields map[string][]string // field_label -> field names (labels can collide)
}

func parseDictionary(metadataCSV []byte) dictionary {
	dict := dictionary{
		fieldType:   map[string]string{},
		labelFields: map[string][]string{},
	}
	rows, err := parseCSV(metadataCSV, ',')
	if err != nil || len(rows) == 0 {
		return dict
	}
	nameIdx, typeIdx, labelIdx := -1, -1, -1
	for i, col := range rows[0] {
		switch strings.ToLower(strings.TrimSpace(col)) {
		case "field_name":
			nameIdx = i
		case "field_type":
			typeIdx = i
		case "field_label":
			labelIdx = i
		}
	}
	if nameIdx < 0 {
		return dict
	}
	for _, row := range rows[1:] {
		if nameIdx >= len(row) {
			continue
		}
		name := strings.TrimSpace(row[nameIdx])
		if name == "" {
			continue
		}
		dict.fieldOrder = append(dict.fieldOrder, name)
		if typeIdx >= 0 && typeIdx < len(row) {
			dict.fieldType[name] = strings.ToLower(strings.TrimSpace(row[typeIdx]))
		}
		if labelIdx >= 0 && labelIdx < len(row) {
			label := strings.TrimSpace(row[labelIdx])
			if label != "" {
				dict.labelFields[label] = append(dict.labelFields[label], name)
			}
		}
	}
	return dict
}

// fileUploadFields returns the dictionary fields of type "file" — per-record
// attachments that are documented in the manifest but never downloaded.
func (d dictionary) fileUploadFields() []string {
	res := []string{}
	for _, name := range d.fieldOrder {
		if d.fieldType[name] == "file" {
			res = append(res, name)
		}
	}
	return res
}

// baseFieldName strips a checkbox expansion suffix: "phones___2" -> "phones".
func baseFieldName(col string) string {
	if i := strings.Index(col, "___"); i > 0 {
		return col[:i]
	}
	return col
}

// resolveHeaderFields maps a data column header to candidate dictionary field
// names. Raw headers resolve via the checkbox base name; label headers are
// translated through the dictionary, including "Label (choice=...)" checkbox
// headers. Unknown headers resolve to themselves so that pseudo-columns
// (record, redcap_event_name, redcap_survey_identifier, ...) stay stable.
func resolveHeaderFields(header string, labelHeaders bool, dict dictionary) []string {
	header = strings.TrimSpace(header)
	if !labelHeaders {
		return []string{baseFieldName(header)}
	}
	label := header
	if i := strings.Index(header, " (choice="); i > 0 {
		label = strings.TrimSpace(header[:i])
	}
	if fields, ok := dict.labelFields[label]; ok && len(fields) > 0 {
		return fields
	}
	return []string{baseFieldName(header)}
}

// anonymizationAudit records the outcome of one blank rule so that silent
// no-ops are impossible: every requested transform reports how much data it
// actually touched.
type anonymizationAudit struct {
	Field   string `json:"field"`
	Mode    string `json:"mode"`
	Matched int    `json:"matched"`
	Note    string `json:"note,omitempty"`
}

func buildAudit(blanks map[string]bool, matched map[string]int, unit string) []anonymizationAudit {
	fields := make([]string, 0, len(blanks))
	for field := range blanks {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	audit := make([]anonymizationAudit, 0, len(fields))
	for _, field := range fields {
		entry := anonymizationAudit{Field: field, Mode: "blank", Matched: matched[field]}
		if entry.Matched == 0 {
			entry.Note = "field not present in export"
		} else {
			entry.Note = fmt.Sprintf("blanked %d %s", entry.Matched, unit)
		}
		audit = append(audit, entry)
	}
	return audit
}

// blankFlatCSV blanks matching columns of a flat CSV export. A blank rule for
// field f matches columns named f, checkbox expansions f___code, and — when
// headers are labels — columns whose label translates back to f.
// Returns the (possibly rewritten) data, the exported dictionary field names,
// and the per-rule audit.
func blankFlatCSV(data []byte, delimiter rune, blanks map[string]bool, labelHeaders bool, dict dictionary) ([]byte, []string, []anonymizationAudit, error) {
	rows, err := parseCSV(data, delimiter)
	if err != nil {
		return nil, nil, nil, err
	}
	if len(rows) == 0 {
		return data, nil, buildAudit(blanks, nil, "columns"), nil
	}
	header := rows[0]
	exported := make([]string, 0, len(header))
	seen := map[string]bool{}
	matched := map[string]int{}
	blankCols := []int{}
	for i, col := range header {
		candidates := resolveHeaderFields(col, labelHeaders, dict)
		for _, field := range candidates {
			if !seen[field] {
				seen[field] = true
				exported = append(exported, field)
			}
		}
		for _, field := range candidates {
			if blanks[field] {
				blankCols = append(blankCols, i)
				matched[field]++
				break
			}
		}
	}
	audit := buildAudit(blanks, matched, "columns")
	if len(blankCols) == 0 {
		return data, exported, audit, nil
	}
	for rowIdx := 1; rowIdx < len(rows); rowIdx++ {
		for _, colIdx := range blankCols {
			if colIdx < len(rows[rowIdx]) {
				rows[rowIdx][colIdx] = ""
			}
		}
	}
	out, err := writeCSV(rows, delimiter)
	if err != nil {
		return nil, nil, nil, err
	}
	return out, exported, audit, nil
}

// blankEAVCSV blanks the value cells of EAV-shaped CSV exports
// (record, [redcap_event_name,] field_name, value): rows whose field_name
// matches a blanked field get an empty value. Falls back to flat handling if
// the EAV columns cannot be located.
func blankEAVCSV(data []byte, delimiter rune, blanks map[string]bool, dict dictionary) ([]byte, []string, []anonymizationAudit, error) {
	rows, err := parseCSV(data, delimiter)
	if err != nil {
		return nil, nil, nil, err
	}
	if len(rows) == 0 {
		return data, nil, buildAudit(blanks, nil, "rows"), nil
	}
	fieldIdx, valueIdx := -1, -1
	for i, col := range rows[0] {
		switch strings.ToLower(strings.TrimSpace(col)) {
		case "field_name":
			fieldIdx = i
		case "value":
			valueIdx = i
		}
	}
	if fieldIdx < 0 || valueIdx < 0 {
		return blankFlatCSV(data, delimiter, blanks, false, dict)
	}
	exported := eavExportedFields(dict)
	seen := map[string]bool{}
	for _, field := range exported {
		seen[field] = true
	}
	matched := map[string]int{}
	changed := false
	for rowIdx := 1; rowIdx < len(rows); rowIdx++ {
		row := rows[rowIdx]
		if fieldIdx >= len(row) {
			continue
		}
		field := baseFieldName(strings.TrimSpace(row[fieldIdx]))
		if field == "" {
			continue
		}
		if !seen[field] {
			seen[field] = true
			exported = append(exported, field)
		}
		if blanks[field] && valueIdx < len(row) {
			if row[valueIdx] != "" {
				row[valueIdx] = ""
				changed = true
			}
			matched[field]++
		}
	}
	audit := buildAudit(blanks, matched, "rows")
	if !changed {
		return data, exported, audit, nil
	}
	out, err := writeCSV(rows, delimiter)
	if err != nil {
		return nil, nil, nil, err
	}
	return out, exported, audit, nil
}

// eavExportedFields seeds the exported-field set for EAV outputs with the
// record-ID field (the first dictionary field): EAV rows reference it in the
// "record" column rather than as a field_name row, and it must stay in the
// filtered metadata.
func eavExportedFields(dict dictionary) []string {
	if len(dict.fieldOrder) > 0 {
		return []string{dict.fieldOrder[0]}
	}
	return []string{}
}

// blankFlatJSON blanks matching keys of flat JSON exports. JSON exports always
// use raw field names as keys, so only checkbox base-name matching applies.
func blankFlatJSON(data []byte, blanks map[string]bool) ([]byte, []string, []anonymizationAudit, error) {
	rows := make([]map[string]interface{}, 0)
	if err := json.Unmarshal(data, &rows); err != nil {
		return nil, nil, nil, err
	}
	keys := map[string]bool{}
	matchedKeys := map[string]string{} // key -> blanked field
	for _, row := range rows {
		for k := range row {
			keys[k] = true
		}
	}
	exportedSet := map[string]bool{}
	exported := []string{}
	for k := range keys {
		field := baseFieldName(k)
		if !exportedSet[field] {
			exportedSet[field] = true
			exported = append(exported, field)
		}
		if blanks[field] {
			matchedKeys[k] = field
		}
	}
	sort.Strings(exported)
	matched := map[string]int{}
	for _, field := range matchedKeys {
		matched[field]++
	}
	audit := buildAudit(blanks, matched, "columns")
	if len(matchedKeys) == 0 {
		return data, exported, audit, nil
	}
	for _, row := range rows {
		for k := range matchedKeys {
			if _, ok := row[k]; ok {
				row[k] = ""
			}
		}
	}
	out, err := json.Marshal(rows)
	if err != nil {
		return nil, nil, nil, err
	}
	return out, exported, audit, nil
}

// blankEAVJSON blanks the "value" of EAV JSON rows whose "field_name" matches
// a blanked field. Falls back to flat handling when rows are not EAV-shaped.
func blankEAVJSON(data []byte, blanks map[string]bool, dict dictionary) ([]byte, []string, []anonymizationAudit, error) {
	rows := make([]map[string]interface{}, 0)
	if err := json.Unmarshal(data, &rows); err != nil {
		return nil, nil, nil, err
	}
	isEAVShaped := len(rows) > 0
	for _, row := range rows {
		if _, ok := row["field_name"]; !ok {
			isEAVShaped = false
			break
		}
	}
	if !isEAVShaped {
		return blankFlatJSON(data, blanks)
	}
	exported := eavExportedFields(dict)
	seen := map[string]bool{}
	for _, field := range exported {
		seen[field] = true
	}
	matched := map[string]int{}
	changed := false
	for _, row := range rows {
		name, _ := row["field_name"].(string)
		field := baseFieldName(strings.TrimSpace(name))
		if field == "" {
			continue
		}
		if !seen[field] {
			seen[field] = true
			exported = append(exported, field)
		}
		if blanks[field] {
			if _, ok := row["value"]; ok {
				row["value"] = ""
				changed = true
			}
			matched[field]++
		}
	}
	audit := buildAudit(blanks, matched, "rows")
	if !changed {
		return data, exported, audit, nil
	}
	out, err := json.Marshal(rows)
	if err != nil {
		return nil, nil, nil, err
	}
	return out, exported, audit, nil
}

// processExportData routes the raw API payload through the mode-appropriate
// blanking implementation and reports the exported dictionary fields plus the
// anonymization audit.
func processExportData(data []byte, opts pluginOptions, blanks map[string]bool, dict dictionary) ([]byte, []string, []anonymizationAudit, error) {
	switch {
	case opts.DataFormat == "json" && isEAV(opts):
		return blankEAVJSON(data, blanks, dict)
	case opts.DataFormat == "json":
		return blankFlatJSON(data, blanks)
	case isEAV(opts):
		return blankEAVCSV(data, reportDelimiter(opts), blanks, dict)
	default:
		return blankFlatCSV(data, reportDelimiter(opts), blanks, headersAreLabels(opts), dict)
	}
}

func fetchReportData(ctx context.Context, baseURL, token string, opts pluginOptions) ([]byte, error) {
	form := baseForm(token, "report", opts.DataFormat)
	form.Set("report_id", opts.ReportID)
	applySharedExportParams(form, opts)
	return redcapRequest(ctx, baseURL, form)
}

func fetchRecordData(ctx context.Context, baseURL, token string, opts pluginOptions) ([]byte, error) {
	form := baseForm(token, "record", opts.DataFormat)
	applySharedExportParams(form, opts)
	applyRecordOnlyFilters(form, opts)
	return redcapRequest(ctx, baseURL, form)
}

// redcapRequestHeaderOnly fetches only the first CSV line of a REDCap response,
// avoiding the cost of downloading the full dataset when only the column names are needed.
func redcapRequestHeaderOnly(ctx context.Context, baseURL string, form url.Values, delimiter rune) ([]string, error) {
	apiURL := getAPIURL(baseURL)
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		apiURL,
		bytes.NewBufferString(form.Encode()),
	)
	if err != nil {
		return nil, err
	}
	req.Header.Add("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Add("Accept", "*/*")

	resp, err := getHTTPClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("redcap request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("redcap request failed with status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 1*1024*1024) // header rows can be wide
	if !scanner.Scan() {
		return nil, fmt.Errorf("empty response from REDCap report header request")
	}
	line := scanner.Text()
	trimmed := strings.TrimSpace(line)
	if strings.HasPrefix(strings.ToUpper(trimmed), "ERROR") {
		return nil, fmt.Errorf("redcap error: %s", trimmed)
	}
	reader := csv.NewReader(strings.NewReader(line))
	reader.Comma = delimiter
	reader.FieldsPerRecord = -1
	record, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("failed to parse report header: %w", err)
	}
	return record, nil
}

func fallbackFieldsFromMetadata(ctx context.Context, baseURL, token string) ([]string, error) {
	form := baseForm(token, "metadata", "csv")
	body, err := redcapRequest(ctx, baseURL, form)
	if err != nil {
		return nil, err
	}
	rows, err := parseCSV(body, ',')
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	fieldIdx := -1
	for i, col := range rows[0] {
		if strings.EqualFold(strings.TrimSpace(col), "field_name") {
			fieldIdx = i
			break
		}
	}
	if fieldIdx < 0 {
		return nil, nil
	}
	res := make([]string, 0, len(rows)-1)
	seen := map[string]bool{}
	for _, row := range rows[1:] {
		if fieldIdx >= len(row) {
			continue
		}
		field := strings.TrimSpace(row[fieldIdx])
		if field == "" || seen[field] {
			continue
		}
		seen[field] = true
		res = append(res, field)
	}
	sort.Strings(res)
	return res, nil
}

func deduplicatedSelectItems(fields []string) []types.SelectItem {
	seen := make(map[string]bool, len(fields))
	unique := make([]string, 0, len(fields))
	for _, f := range fields {
		f = strings.TrimSpace(f)
		if f != "" && !seen[f] {
			seen[f] = true
			unique = append(unique, f)
		}
	}
	sort.Strings(unique)
	out := make([]types.SelectItem, 0, len(unique))
	for _, field := range unique {
		out = append(out, types.SelectItem{Label: field, Value: field})
	}
	return out
}

// deduplicatedSelectItemsWithIdentifiers builds a sorted, deduplicated list of SelectItem values.
// Fields present in the identifiers set are returned with Selected=true, signalling the frontend
// to auto-blank them (they are REDCap identifier-tagged fields).
func deduplicatedSelectItemsWithIdentifiers(fields []string, identifiers map[string]bool) []types.SelectItem {
	seen := make(map[string]bool, len(fields))
	unique := make([]string, 0, len(fields))
	for _, f := range fields {
		f = strings.TrimSpace(f)
		if f != "" && !seen[f] {
			seen[f] = true
			unique = append(unique, f)
		}
	}
	sort.Strings(unique)
	out := make([]types.SelectItem, 0, len(unique))
	for _, field := range unique {
		out = append(out, types.SelectItem{Label: field, Value: field, Selected: identifiers[field]})
	}
	return out
}

// identifierFieldsFromMetadata fetches the project metadata and returns a set of field names
// that REDCap has tagged as identifiers (identifier column = "y" in the data dictionary).
func identifierFieldsFromMetadata(ctx context.Context, baseURL, token string) (map[string]bool, error) {
	form := baseForm(token, "metadata", "csv")
	body, err := redcapRequest(ctx, baseURL, form)
	if err != nil {
		return nil, err
	}
	rows, err := parseCSV(body, ',')
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	fieldIdx := -1
	identifierIdx := -1
	for i, col := range rows[0] {
		switch strings.ToLower(strings.TrimSpace(col)) {
		case "field_name":
			fieldIdx = i
		case "identifier":
			identifierIdx = i
		}
	}
	if fieldIdx < 0 || identifierIdx < 0 {
		return nil, nil
	}
	res := make(map[string]bool)
	for _, row := range rows[1:] {
		if fieldIdx >= len(row) || identifierIdx >= len(row) {
			continue
		}
		field := strings.TrimSpace(row[fieldIdx])
		ident := strings.ToLower(strings.TrimSpace(row[identifierIdx]))
		if field != "" && (ident == "y" || ident == "yes" || ident == "1") {
			res[field] = true
		}
	}
	return res, nil
}

// listVariablesFromReport fetches column headers from a report export (CSV header-only request).
// Falls back to the full metadata field list if the report header fetch fails.
// Fields tagged as identifiers in REDCap are returned with Selected=true.
func listVariablesFromReport(ctx context.Context, baseURL, token, reportID string, opts pluginOptions) ([]types.SelectItem, error) {
	identifiers, _ := identifierFieldsFromMetadata(ctx, baseURL, token)

	form := baseForm(token, "report", "csv")
	form.Set("report_id", reportID)
	applySharedExportParams(form, opts)

	fields, err := redcapRequestHeaderOnly(ctx, baseURL, form, ',')
	if err != nil {
		// Fallback: derive field list from project metadata.
		fields, err = fallbackFieldsFromMetadata(ctx, baseURL, token)
		if err != nil {
			return nil, err
		}
	}
	return deduplicatedSelectItemsWithIdentifiers(fields, identifiers), nil
}

// listVariablesFromMetadata returns all project fields from the metadata endpoint.
// Used for record export mode where there is no report to derive headers from.
// Fields tagged as identifiers in REDCap (identifier column = "y") are returned
// with Selected=true so the frontend can auto-blank them.
func listVariablesFromMetadata(ctx context.Context, baseURL, token string) ([]types.SelectItem, error) {
	identifiers, err := identifierFieldsFromMetadata(ctx, baseURL, token)
	if err != nil {
		return nil, err
	}
	fields, err := fallbackFieldsFromMetadata(ctx, baseURL, token)
	if err != nil {
		return nil, err
	}
	return deduplicatedSelectItemsWithIdentifiers(fields, identifiers), nil
}

func exportMetadataCSV(ctx context.Context, baseURL, token string, fields []string) ([]byte, error) {
	form := baseForm(token, "metadata", "csv")
	if len(fields) > 0 {
		form.Set("fields", strings.Join(fields, ","))
	}
	return redcapRequest(ctx, baseURL, form)
}

func filterMetadataCSV(data []byte, fields []string) ([]byte, error) {
	if len(fields) == 0 {
		return data, nil
	}
	allowed := map[string]bool{}
	for _, field := range fields {
		if strings.TrimSpace(field) != "" {
			allowed[strings.TrimSpace(field)] = true
		}
	}
	if len(allowed) == 0 {
		return data, nil
	}

	rows, err := parseCSV(data, ',')
	if err != nil || len(rows) == 0 {
		return data, err
	}
	fieldIdx := -1
	for i, col := range rows[0] {
		if strings.EqualFold(strings.TrimSpace(col), "field_name") {
			fieldIdx = i
			break
		}
	}
	if fieldIdx < 0 {
		return data, nil
	}
	filtered := make([][]string, 0, len(rows))
	filtered = append(filtered, rows[0])
	for _, row := range rows[1:] {
		if fieldIdx >= len(row) {
			continue
		}
		if allowed[strings.TrimSpace(row[fieldIdx])] {
			filtered = append(filtered, row)
		}
	}
	return writeCSV(filtered, ',')
}

func exportProjectInfo(ctx context.Context, baseURL, token string) ([]byte, bool, error) {
	form := baseForm(token, "project", "json")
	body, err := redcapRequest(ctx, baseURL, form)
	if err != nil {
		return nil, false, err
	}
	return body, detectLongitudinal(body), nil
}

func detectLongitudinal(payload []byte) bool {
	check := func(v interface{}) bool {
		switch s := v.(type) {
		case bool:
			return s
		case string:
			switch strings.ToLower(strings.TrimSpace(s)) {
			case "1", "true", "yes", "y":
				return true
			}
		case float64:
			return s != 0
		}
		return false
	}

	var obj map[string]interface{}
	if err := json.Unmarshal(payload, &obj); err == nil {
		for _, key := range []string{"is_longitudinal", "is_longitudinal_project"} {
			if v, ok := obj[key]; ok && check(v) {
				return true
			}
		}
	}

	var arr []map[string]interface{}
	if err := json.Unmarshal(payload, &arr); err == nil {
		for _, row := range arr {
			for _, key := range []string{"is_longitudinal", "is_longitudinal_project"} {
				if v, ok := row[key]; ok && check(v) {
					return true
				}
			}
		}
	}
	return false
}

func exportVersion(ctx context.Context, baseURL, token string) string {
	form := baseForm(token, "version", "json")
	body, err := redcapRequest(ctx, baseURL, form)
	if err != nil {
		return ""
	}
	trimmed := strings.TrimSpace(string(body))
	if trimmed == "" {
		return ""
	}
	var asString string
	if err := json.Unmarshal(body, &asString); err == nil {
		return strings.TrimSpace(asString)
	}
	return strings.Trim(trimmed, "\"")
}

func exportCSVContent(ctx context.Context, baseURL, token, content string) ([]byte, error) {
	form := baseForm(token, content, "csv")
	return redcapRequest(ctx, baseURL, form)
}

func sanitizeReportID(reportID string) string {
	if reportID == "" {
		return "unknown"
	}
	var b strings.Builder
	for _, r := range reportID {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '_' || r == '-' || r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	safe := b.String()
	if safe == "" {
		return "unknown"
	}
	return safe
}

// manifestExtras carries the provenance and audit context recorded alongside
// the export parameters (decisions 2026-06-11: attachments documented, never
// downloaded; every anonymization rule reports its outcome).
type manifestExtras struct {
	Audit                       []anonymizationAudit
	FileUploadFields            []string
	ProjectID                   interface{}
	ProjectTitle                string
	DictionaryFieldsNotExported []string
}

func makeManifest(opts pluginOptions, reportID, dataPath, metadataPath, projectInfoPath, eventsPath, mappingPath, redcapVersion string, warnings []string, extras manifestExtras) ([]byte, error) {
	recordType := opts.RecordType
	if opts.ExportMode != "records" {
		recordType = "flat" // content=report has no type parameter
	}
	manifest := map[string]interface{}{
		"plugin":         "redcap2",
		"export_mode":    opts.ExportMode,
		"generated_at":   opts.GeneratedAt,
		"redcap_version": redcapVersion,
		"export": map[string]interface{}{
			"data_format":          opts.DataFormat,
			"record_type":          recordType,
			"csv_delimiter":        opts.CsvDelimiter,
			"raw_or_label":         opts.RawOrLabel,
			"raw_or_label_headers": opts.RawOrLabelHeaders,
			"fields":               opts.Fields,
			"forms":                opts.Forms,
			"events":               opts.Events,
			"records":              opts.Records,
			"filter_logic":         opts.FilterLogic,
			"date_range_begin":     opts.DateRangeBegin,
			"date_range_end":       opts.DateRangeEnd,
		},
		"files": map[string]string{
			"data":         dataPath,
			"metadata":     metadataPath,
			"project_info": projectInfoPath,
		},
	}
	if opts.ExportMode == "report" {
		manifest["report_id"] = reportID
	}
	if eventsPath != "" {
		manifest["files"].(map[string]string)["events"] = eventsPath
	}
	if mappingPath != "" {
		manifest["files"].(map[string]string)["form_event_mapping"] = mappingPath
	}

	if extras.ProjectID != nil || extras.ProjectTitle != "" {
		manifest["project"] = map[string]interface{}{
			"id":    extras.ProjectID,
			"title": extras.ProjectTitle,
		}
	}
	if len(extras.FileUploadFields) > 0 {
		manifest["attachments"] = map[string]interface{}{
			"file_upload_fields": extras.FileUploadFields,
			"exported":           false,
			"note":               "per-record file attachments are not exported by this plugin",
		}
	}
	if len(extras.Audit) > 0 {
		manifest["anonymization_audit"] = extras.Audit
		for _, entry := range extras.Audit {
			if entry.Matched == 0 {
				warnings = append(warnings, fmt.Sprintf("anonymization: field %q matched no exported data", entry.Field))
			}
		}
	}
	if len(extras.DictionaryFieldsNotExported) > 0 {
		manifest["dictionary_fields_not_exported"] = extras.DictionaryFieldsNotExported
	}

	if len(opts.Variables) > 0 {
		manifest["variables"] = opts.Variables
	}
	if len(warnings) > 0 {
		manifest["warnings"] = warnings
	}

	return json.MarshalIndent(manifest, "", "  ")
}

// projectIdentity extracts project_id and project_title from a
// content=project JSON payload (object or single-element array form).
func projectIdentity(payload []byte) (interface{}, string) {
	read := func(obj map[string]interface{}) (interface{}, string) {
		title, _ := obj["project_title"].(string)
		return obj["project_id"], title
	}
	var obj map[string]interface{}
	if err := json.Unmarshal(payload, &obj); err == nil {
		return read(obj)
	}
	var arr []map[string]interface{}
	if err := json.Unmarshal(payload, &arr); err == nil && len(arr) > 0 {
		return read(arr[0])
	}
	return nil, ""
}

func buildExportBundle(ctx context.Context, baseURL, token string, opts pluginOptions, reportID string) (generatedBundle, error) {
	// The dictionary drives blanking, header translation, and metadata
	// filtering, so it is fetched before the data export.
	metadataRaw, err := exportMetadataCSV(ctx, baseURL, token, nil)
	if err != nil {
		return generatedBundle{}, fmt.Errorf("metadata export failed: %w", err)
	}
	dict := parseDictionary(metadataRaw)

	var rawData []byte
	var basePath string
	if opts.ExportMode == "records" {
		rawData, err = fetchRecordData(ctx, baseURL, token, opts)
		if err != nil {
			return generatedBundle{}, fmt.Errorf("record export failed: %w", err)
		}
		basePath = "redcap/records"
	} else {
		rawData, err = fetchReportData(ctx, baseURL, token, opts)
		if err != nil {
			return generatedBundle{}, fmt.Errorf("report export failed: %w", err)
		}
		safeID := sanitizeReportID(reportID)
		basePath = fmt.Sprintf("redcap/report-%s", safeID)
	}

	blanks := blankFields(opts)
	dataBytes, dataFields, audit, err := processExportData(rawData, opts, blanks, dict)
	if err != nil {
		return generatedBundle{}, fmt.Errorf("export processing failed: %w", err)
	}

	metadataBytes, err := filterMetadataCSV(metadataRaw, dataFields)
	if err != nil {
		return generatedBundle{}, fmt.Errorf("metadata filtering failed: %w", err)
	}

	projectInfoBytes, isLongitudinal, err := exportProjectInfo(ctx, baseURL, token)
	if err != nil {
		return generatedBundle{}, fmt.Errorf("project info export failed: %w", err)
	}
	redcapVersion := exportVersion(ctx, baseURL, token)
	dataFileName := "data.csv"
	if opts.DataFormat == "json" {
		dataFileName = "data.json"
	}
	dataPath := basePath + "/" + dataFileName
	metadataPath := basePath + "/metadata.csv"
	projectInfoPath := basePath + "/project_info.json"
	eventsPath := ""
	mappingPath := ""
	warnings := []string{}

	files := map[string][]byte{
		dataPath:        dataBytes,
		metadataPath:    metadataBytes,
		projectInfoPath: projectInfoBytes,
	}

	if isLongitudinal {
		eventsBytes, eventsErr := exportCSVContent(ctx, baseURL, token, "event")
		if eventsErr != nil {
			warnings = append(warnings, fmt.Sprintf("events export failed: %v", eventsErr))
		} else {
			eventsPath = basePath + "/events.csv"
			files[eventsPath] = eventsBytes
		}

		mappingBytes, mappingErr := exportCSVContent(ctx, baseURL, token, "formEventMapping")
		if mappingErr != nil {
			warnings = append(warnings, fmt.Sprintf("form-event mapping export failed: %v", mappingErr))
		} else {
			mappingPath = basePath + "/form_event_mapping.csv"
			files[mappingPath] = mappingBytes
		}
	}

	projectID, projectTitle := projectIdentity(projectInfoBytes)
	extras := manifestExtras{
		Audit:            audit,
		FileUploadFields: dict.fileUploadFields(),
		ProjectID:        projectID,
		ProjectTitle:     projectTitle,
	}
	// In an unfiltered flat records export, dictionary fields missing from the
	// output reveal server-side stripping (token export rights). With filters
	// or report definitions the diff is expected, so it is not recorded.
	if opts.ExportMode == "records" && !isEAV(opts) &&
		len(opts.Fields) == 0 && len(opts.Forms) == 0 && len(opts.Events) == 0 {
		exported := make(map[string]bool, len(dataFields))
		for _, field := range dataFields {
			exported[field] = true
		}
		for _, field := range dict.fieldOrder {
			if !exported[field] {
				extras.DictionaryFieldsNotExported = append(extras.DictionaryFieldsNotExported, field)
			}
		}
	}

	manifestBytes, err := makeManifest(
		opts,
		reportID,
		dataPath,
		metadataPath,
		projectInfoPath,
		eventsPath,
		mappingPath,
		redcapVersion,
		warnings,
		extras,
	)
	if err != nil {
		return generatedBundle{}, fmt.Errorf("manifest generation failed: %w", err)
	}
	files[basePath+"/manifest.json"] = manifestBytes

	logging.Logger.Printf("redcap2: generated %d virtual files (mode: %s, report: %s)", len(files), opts.ExportMode, reportID)
	return generatedBundle{
		ReportID: reportID,
		Files:    files,
	}, nil
}

func md5Hex(data []byte) string {
	sum := md5.Sum(data)
	return fmt.Sprintf("%x", sum)
}

// bundleCacheKey returns a stable key for the export bundle cache.
// GeneratedAt is intentionally excluded so the same underlying data always
// produces the same key regardless of when the user pressed "Continue to compare".
func bundleCacheKey(baseURL, token string, opts pluginOptions) string {
	stable := pluginOptions{
		ExportMode:             opts.ExportMode,
		ReportID:               opts.ReportID,
		DataFormat:             opts.DataFormat,
		Fields:                 opts.Fields,
		Forms:                  opts.Forms,
		Events:                 opts.Events,
		Records:                opts.Records,
		FilterLogic:            opts.FilterLogic,
		DateRangeBegin:         opts.DateRangeBegin,
		DateRangeEnd:           opts.DateRangeEnd,
		RecordType:             opts.RecordType,
		CsvDelimiter:           opts.CsvDelimiter,
		RawOrLabel:             opts.RawOrLabel,
		RawOrLabelHeaders:      opts.RawOrLabelHeaders,
		ExportSurveyFields:     opts.ExportSurveyFields,
		ExportDataAccessGroups: opts.ExportDataAccessGroups,
		Variables:              opts.Variables,
		// GeneratedAt intentionally excluded
	}
	data, _ := json.Marshal(stable)
	h := md5.Sum(append([]byte(baseURL+"\x00"+token+"\x00"), data...))
	return fmt.Sprintf("%x", h)
}

// cachedBuildExportBundle returns a cached bundle when available, otherwise
// calls buildExportBundle and caches the result for bundleCacheTTL.
// This halves API calls (Query + Streams each previously made ~5 requests)
// and guarantees the hashes from Query match the bytes served by Streams.
func cachedBuildExportBundle(ctx context.Context, baseURL, token string, opts pluginOptions, reportID string) (generatedBundle, error) {
	key := bundleCacheKey(baseURL, token, opts)
	if bundle, ok := globalBundleCache.get(key); ok {
		logging.Logger.Printf("redcap2: bundle cache hit (mode: %s, report: %s)", opts.ExportMode, reportID)
		return bundle, nil
	}
	bundle, err := buildExportBundle(ctx, baseURL, token, opts, reportID)
	if err != nil {
		return generatedBundle{}, err
	}
	if bundle.size() <= maxCacheableBundleBytes {
		globalBundleCache.set(key, bundle)
	} else {
		logging.Logger.Printf("redcap2: bundle too large to cache (%d bytes), will rebuild on demand", bundle.size())
	}
	return bundle, nil
}
