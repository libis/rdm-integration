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

var globalBundleCache = &bundleStore{entries: make(map[string]bundleCacheEntry)}

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

	switch strings.ToLower(strings.TrimSpace(opts.RawOrLabel)) {
	case "label":
		opts.RawOrLabel = "label"
	case "both":
		opts.RawOrLabel = "both"
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

// applySharedExportParams sets parameters valid for both content=report and content=record.
func applySharedExportParams(form url.Values, opts pluginOptions) {
	if opts.RecordType != "" {
		form.Set("type", opts.RecordType)
	}
	if opts.CsvDelimiter == "\t" {
		form.Set("csvDelimiter", "tab")
	}
	if opts.RawOrLabel != "" && opts.RawOrLabel != "raw" {
		form.Set("rawOrLabel", opts.RawOrLabel)
	}
	if opts.RawOrLabelHeaders != "" && opts.RawOrLabelHeaders != "raw" {
		form.Set("rawOrLabelHeaders", opts.RawOrLabelHeaders)
	}
}

// applyRecordOnlyFilters sets parameters only valid for content=record exports.
// These parameters are not supported by the content=report endpoint.
func applyRecordOnlyFilters(form url.Values, opts pluginOptions) {
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

func applyBlankCSV(data []byte, delimiter rune, blanks map[string]bool) ([]byte, []string, error) {
	rows, err := parseCSV(data, delimiter)
	if err != nil {
		return nil, nil, err
	}
	if len(rows) == 0 {
		return data, nil, nil
	}
	header := append([]string(nil), rows[0]...)
	if len(blanks) == 0 {
		return data, header, nil
	}
	indices := make([]int, 0, len(header))
	for i, field := range header {
		if blanks[field] {
			indices = append(indices, i)
		}
	}
	if len(indices) == 0 {
		return data, header, nil
	}
	for rowIdx := 1; rowIdx < len(rows); rowIdx++ {
		for _, colIdx := range indices {
			if colIdx < len(rows[rowIdx]) {
				rows[rowIdx][colIdx] = ""
			}
		}
	}
	out, err := writeCSV(rows, delimiter)
	if err != nil {
		return nil, nil, err
	}
	return out, header, nil
}

func applyBlankJSON(data []byte, blanks map[string]bool) ([]byte, []string, error) {
	rows := make([]map[string]interface{}, 0)
	if err := json.Unmarshal(data, &rows); err != nil {
		return nil, nil, err
	}
	keys := map[string]bool{}
	for _, row := range rows {
		for k := range row {
			keys[k] = true
		}
	}
	fields := make([]string, 0, len(keys))
	for k := range keys {
		fields = append(fields, k)
	}
	sort.Strings(fields)

	if len(blanks) == 0 {
		return data, fields, nil
	}

	for _, row := range rows {
		for field := range blanks {
			if _, ok := row[field]; ok {
				row[field] = ""
			}
		}
	}

	out, err := json.Marshal(rows)
	if err != nil {
		return nil, nil, err
	}
	return out, fields, nil
}

func exportReportData(ctx context.Context, baseURL, token string, opts pluginOptions) ([]byte, []string, error) {
	form := baseForm(token, "report", opts.DataFormat)
	form.Set("report_id", opts.ReportID)
	applySharedExportParams(form, opts)

	body, err := redcapRequest(ctx, baseURL, form)
	if err != nil {
		return nil, nil, err
	}

	blanks := blankFields(opts)
	if opts.DataFormat == "json" {
		return applyBlankJSON(body, blanks)
	}
	return applyBlankCSV(body, reportDelimiter(opts), blanks)
}

func exportRecordData(ctx context.Context, baseURL, token string, opts pluginOptions) ([]byte, []string, error) {
	form := baseForm(token, "record", opts.DataFormat)
	applySharedExportParams(form, opts)
	applyRecordOnlyFilters(form, opts)

	body, err := redcapRequest(ctx, baseURL, form)
	if err != nil {
		return nil, nil, err
	}

	blanks := blankFields(opts)
	if opts.DataFormat == "json" {
		return applyBlankJSON(body, blanks)
	}
	return applyBlankCSV(body, reportDelimiter(opts), blanks)
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

func makeManifest(opts pluginOptions, reportID, dataPath, metadataPath, projectInfoPath, eventsPath, mappingPath, redcapVersion string, warnings []string) ([]byte, error) {
	manifest := map[string]interface{}{
		"plugin":         "redcap2",
		"export_mode":    opts.ExportMode,
		"generated_at":   opts.GeneratedAt,
		"redcap_version": redcapVersion,
		"export": map[string]interface{}{
			"data_format":          opts.DataFormat,
			"record_type":          opts.RecordType,
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

	if len(opts.Variables) > 0 {
		manifest["variables"] = opts.Variables
	}
	if len(warnings) > 0 {
		manifest["warnings"] = warnings
	}

	return json.MarshalIndent(manifest, "", "  ")
}

func buildExportBundle(ctx context.Context, baseURL, token string, opts pluginOptions, reportID string) (generatedBundle, error) {
	var dataBytes []byte
	var dataFields []string
	var err error
	var basePath string

	if opts.ExportMode == "records" {
		dataBytes, dataFields, err = exportRecordData(ctx, baseURL, token, opts)
		if err != nil {
			return generatedBundle{}, fmt.Errorf("record export failed: %w", err)
		}
		basePath = "redcap/records"
	} else {
		dataBytes, dataFields, err = exportReportData(ctx, baseURL, token, opts)
		if err != nil {
			return generatedBundle{}, fmt.Errorf("report export failed: %w", err)
		}
		safeID := sanitizeReportID(reportID)
		basePath = fmt.Sprintf("redcap/report-%s", safeID)
	}

	metadataRaw, err := exportMetadataCSV(ctx, baseURL, token, nil)
	if err != nil {
		return generatedBundle{}, fmt.Errorf("metadata export failed: %w", err)
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
	globalBundleCache.set(key, bundle)
	return bundle, nil
}
