// Author: Eryk Kulikowski @ KU Leuven (2026). Apache 2.0 License

package redcap2

import (
	"bufio"
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/md5"
	"crypto/sha256"
	"encoding/base64"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"integration/app/config"
	"integration/app/logging"
	"integration/app/plugin/types"
	"io"
	"net/http"
	"net/url"
	"regexp"
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
	PseudonymizationKey    string           `json:"pseudonymizationKey,omitempty"`
	GeneratedAt            string           `json:"generatedAt"`
}

type generatedBundle struct {
	ReportID string
	Files    map[string][]byte
	// Mime holds explicit mime types for generated files (keyed by path).
	// Files without an entry rely on destination-side type detection.
	Mime map[string]string
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

// defaultHTTPTimeout bounds a single REDCap API request. Large projects can
// need more: configure options.redcapHttpTimeout (a Go duration string, e.g.
// "15m") in the backend config.
const defaultHTTPTimeout = 5 * time.Minute

// parseHTTPTimeout parses a configured timeout, falling back to the default
// for empty, invalid, or non-positive values.
func parseHTTPTimeout(raw string) time.Duration {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return defaultHTTPTimeout
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d <= 0 {
		logging.Logger.Printf("redcap2: invalid redcapHttpTimeout %q, using default %v", raw, defaultHTTPTimeout)
		return defaultHTTPTimeout
	}
	return d
}

func getHTTPClient() *http.Client {
	clientOnce.Do(func() {
		httpClient = &http.Client{
			Timeout: parseHTTPTimeout(config.GetConfig().Options.RedcapHttpTimeout),
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
	opts.PseudonymizationKey = strings.TrimSpace(opts.PseudonymizationKey)
	for i := range opts.Variables {
		opts.Variables[i].Name = strings.TrimSpace(opts.Variables[i].Name)
		switch strings.ToLower(strings.TrimSpace(opts.Variables[i].Anonymization)) {
		case "blank":
			opts.Variables[i].Anonymization = "blank"
		case "drop":
			opts.Variables[i].Anonymization = "drop"
		case "pseudonymize":
			opts.Variables[i].Anonymization = "pseudonymize"
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

// minPseudonymizationKeyBytes is the minimum decoded key length accepted for
// HMAC-SHA256 pseudonymization. 16 bytes (128 bit) is the floor; the UI and
// docs recommend 32 bytes (openssl rand -base64 32).
const minPseudonymizationKeyBytes = 16

// transformPlan is the validated per-export anonymization policy: which field
// gets which irreversible transform, plus the decoded HMAC key when any field
// is pseudonymized. The raw key never leaves this struct: only the SHA-256
// fingerprint is reported (manifest/audit), and nothing key-related is logged.
type transformPlan struct {
	modes          map[string]string // field -> "blank" | "drop" | "pseudonymize"
	key            []byte
	keyFingerprint string // first 16 hex chars of SHA-256(key)
}

func (p transformPlan) isEmpty() bool { return len(p.modes) == 0 }

func (p transformPlan) mode(field string) string { return p.modes[field] }

// transformValue applies a cell-level transform (blank or pseudonymize).
// Empty values stay empty: hashing the empty string would replace genuine
// missingness with a constant that looks like data.
func (p transformPlan) transformValue(field, value string) string {
	switch p.modes[field] {
	case "blank":
		return ""
	case "pseudonymize":
		if value == "" {
			return ""
		}
		mac := hmac.New(sha256.New, p.key)
		mac.Write([]byte(value))
		return hex.EncodeToString(mac.Sum(nil))
	}
	return value
}

// memoizedTransform returns a transformValue wrapper that caches results per
// input value. Used for the EAV record column, where the same record ID
// recurs once per exported field and recomputing the HMAC dominates the
// processing cost of large EAV exports.
func (p transformPlan) memoizedTransform(field string) func(string) string {
	cache := map[string]string{}
	return func(value string) string {
		if cached, ok := cache[value]; ok {
			return cached
		}
		transformed := p.transformValue(field, value)
		cache[value] = transformed
		return transformed
	}
}

// buildTransformPlan validates the per-variable anonymization choices and the
// researcher-provided base64 HMAC key (required iff any field is pseudonymized).
func buildTransformPlan(opts pluginOptions) (transformPlan, error) {
	plan := transformPlan{modes: map[string]string{}}
	usesPseudonymization := false
	for _, v := range opts.Variables {
		if v.Name == "" || v.Anonymization == "none" || v.Anonymization == "" {
			continue
		}
		plan.modes[v.Name] = v.Anonymization
		if v.Anonymization == "pseudonymize" {
			usesPseudonymization = true
		}
	}
	if !usesPseudonymization {
		return plan, nil
	}
	if opts.PseudonymizationKey == "" {
		return transformPlan{}, fmt.Errorf("pseudonymization requires a base64 key (generate one with: openssl rand -base64 32)")
	}
	key, err := base64.StdEncoding.DecodeString(opts.PseudonymizationKey)
	if err != nil {
		return transformPlan{}, fmt.Errorf("pseudonymization key is not valid base64")
	}
	if len(key) < minPseudonymizationKeyBytes {
		return transformPlan{}, fmt.Errorf("pseudonymization key too short: %d bytes decoded, need at least %d (use: openssl rand -base64 32)", len(key), minPseudonymizationKeyBytes)
	}
	plan.key = key
	fingerprint := sha256.Sum256(key)
	plan.keyFingerprint = hex.EncodeToString(fingerprint[:])[:16]
	return plan, nil
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

// dictionary holds the parsed data-dictionary information needed for the
// transforms, label-header translation, metadata filtering, identifier
// preselection, PHI-risk flagging, and manifest documentation.
type dictionary struct {
	fieldOrder    []string            // field names in dictionary order
	fieldType     map[string]string   // field_name -> field_type
	fieldLabel    map[string]string   // field_name -> field_label
	labelFields   map[string][]string // field_label -> field names (labels can collide)
	identifier    map[string]bool     // field_name -> tagged as identifier in REDCap
	validation    map[string]string   // field_name -> text validation type ("" = unvalidated)
	validationMin map[string]string   // field_name -> text_validation_min
	validationMax map[string]string   // field_name -> text_validation_max
	choices       map[string]string   // field_name -> raw select_choices_or_calculations
	hasValidation bool                // the validation column was present in the dictionary
}

func parseDictionary(metadataCSV []byte) dictionary {
	dict := dictionary{
		fieldType:     map[string]string{},
		fieldLabel:    map[string]string{},
		labelFields:   map[string][]string{},
		identifier:    map[string]bool{},
		validation:    map[string]string{},
		validationMin: map[string]string{},
		validationMax: map[string]string{},
		choices:       map[string]string{},
	}
	rows, err := parseCSV(metadataCSV, ',')
	if err != nil || len(rows) == 0 {
		return dict
	}
	nameIdx, typeIdx, labelIdx, identifierIdx, validationIdx, choicesIdx := -1, -1, -1, -1, -1, -1
	minIdx, maxIdx := -1, -1
	for i, col := range rows[0] {
		switch strings.ToLower(strings.TrimSpace(col)) {
		case "field_name":
			nameIdx = i
		case "field_type":
			typeIdx = i
		case "field_label":
			labelIdx = i
		case "identifier":
			identifierIdx = i
		case "text_validation_type_or_show_slider_number":
			validationIdx = i
		case "text_validation_min":
			minIdx = i
		case "text_validation_max":
			maxIdx = i
		case "select_choices_or_calculations":
			choicesIdx = i
		}
	}
	if nameIdx < 0 {
		return dict
	}
	dict.hasValidation = validationIdx >= 0
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
				dict.fieldLabel[name] = label
				dict.labelFields[label] = append(dict.labelFields[label], name)
			}
		}
		if choicesIdx >= 0 && choicesIdx < len(row) {
			choices := strings.TrimSpace(row[choicesIdx])
			if choices != "" {
				dict.choices[name] = choices
			}
		}
		if identifierIdx >= 0 && identifierIdx < len(row) {
			switch strings.ToLower(strings.TrimSpace(row[identifierIdx])) {
			case "y", "yes", "1":
				dict.identifier[name] = true
			}
		}
		if validationIdx >= 0 && validationIdx < len(row) {
			dict.validation[name] = strings.ToLower(strings.TrimSpace(row[validationIdx]))
		}
		if minIdx >= 0 && minIdx < len(row) {
			if v := strings.TrimSpace(row[minIdx]); v != "" {
				dict.validationMin[name] = v
			}
		}
		if maxIdx >= 0 && maxIdx < len(row) {
			if v := strings.TrimSpace(row[maxIdx]); v != "" {
				dict.validationMax[name] = v
			}
		}
	}
	return dict
}

// fetchDictionary downloads and parses the project data dictionary.
func fetchDictionary(ctx context.Context, baseURL, token string) (dictionary, error) {
	body, err := redcapRequest(ctx, baseURL, baseForm(token, "metadata", "csv"))
	if err != nil {
		return dictionary{}, err
	}
	return parseDictionary(body), nil
}

// phiRiskNote returns a warning for fields whose values can carry free-text
// identifying information even though the field is not identifier-tagged:
// notes fields and unvalidated text fields. REDCap's own de-identified export
// rights strip these field types for the same reason.
func phiRiskNote(dict dictionary, field string) string {
	base := baseFieldName(field)
	switch dict.fieldType[base] {
	case "notes":
		return "free-text notes field: may contain identifying information"
	case "text":
		// Only flag unvalidated text when the dictionary actually carried the
		// validation column; otherwise every text field would be flagged.
		if dict.hasValidation && dict.validation[base] == "" {
			return "unvalidated text field: may contain identifying information"
		}
	}
	return ""
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
// names. Raw headers resolve to the column name itself plus the checkbox base
// name, so a transform rule keyed on either "phones___2" or "phones" matches
// the expansion column. Label headers are translated through the dictionary,
// including "Label (choice=...)" checkbox headers. Unknown headers resolve to
// themselves so that pseudo-columns (record, redcap_event_name,
// redcap_survey_identifier, ...) stay stable.
func resolveHeaderFields(header string, labelHeaders bool, dict dictionary) []string {
	header = strings.TrimSpace(header)
	if !labelHeaders {
		base := baseFieldName(header)
		if base != header {
			return []string{header, base}
		}
		return []string{header}
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

// anonymizationAudit records the outcome of one transform rule so that silent
// no-ops are impossible: every requested transform reports how much data it
// actually touched.
type anonymizationAudit struct {
	Field   string `json:"field"`
	Mode    string `json:"mode"`
	Matched int    `json:"matched"`
	Note    string `json:"note,omitempty"`
}

func transformVerb(mode string) string {
	switch mode {
	case "drop":
		return "dropped"
	case "pseudonymize":
		return "pseudonymized"
	default:
		return "blanked"
	}
}

func buildAudit(plan transformPlan, matched map[string]int, unit string, notes map[string]string) []anonymizationAudit {
	fields := make([]string, 0, len(plan.modes))
	for field := range plan.modes {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	audit := make([]anonymizationAudit, 0, len(fields))
	for _, field := range fields {
		entry := anonymizationAudit{Field: field, Mode: plan.modes[field], Matched: matched[field]}
		if entry.Matched == 0 {
			entry.Note = "field not present in export"
		} else {
			entry.Note = fmt.Sprintf("%s %d %s", transformVerb(entry.Mode), entry.Matched, unit)
		}
		if extra := notes[field]; extra != "" {
			entry.Note += "; " + extra
		}
		audit = append(audit, entry)
	}
	return audit
}

// transformFlatCSV applies the anonymization plan to a flat CSV export. A rule
// for field f matches columns named f, checkbox expansions f___code, and —
// when headers are labels — columns whose label translates back to f.
// Dropped columns are removed entirely (and excluded from the exported field
// list so their dictionary rows disappear from metadata.csv); blank and
// pseudonymize rewrite cell values in place.
// Returns the (possibly rewritten) data, the exported dictionary field names,
// and the per-rule audit.
func transformFlatCSV(data []byte, delimiter rune, plan transformPlan, labelHeaders bool, dict dictionary) ([]byte, []string, []anonymizationAudit, error) {
	rows, err := parseCSV(data, delimiter)
	if err != nil {
		return nil, nil, nil, err
	}
	if len(rows) == 0 {
		return data, nil, buildAudit(plan, nil, "columns", nil), nil
	}
	header := rows[0]
	exported := make([]string, 0, len(header))
	seen := map[string]bool{}
	matched := map[string]int{}
	cellField := map[int]string{} // column -> field with a cell-level transform
	dropCols := map[int]bool{}
	for i, col := range header {
		candidates := resolveHeaderFields(col, labelHeaders, dict)
		var ruleField string
		for _, field := range candidates {
			if plan.modes[field] != "" {
				ruleField = field
				break
			}
		}
		if ruleField != "" {
			matched[ruleField]++
			if plan.modes[ruleField] == "drop" {
				dropCols[i] = true
				continue // dropped fields are no longer part of the export
			}
			cellField[i] = ruleField
		}
		for _, field := range candidates {
			if !seen[field] {
				seen[field] = true
				exported = append(exported, field)
			}
		}
	}
	audit := buildAudit(plan, matched, "columns", nil)
	if len(cellField) == 0 && len(dropCols) == 0 {
		return data, exported, audit, nil
	}
	out := make([][]string, 0, len(rows))
	for rowIdx, row := range rows {
		newRow := make([]string, 0, len(row))
		for colIdx, cell := range row {
			if dropCols[colIdx] {
				continue
			}
			if rowIdx > 0 {
				if field, ok := cellField[colIdx]; ok {
					cell = plan.transformValue(field, cell)
				}
			}
			newRow = append(newRow, cell)
		}
		out = append(out, newRow)
	}
	encoded, err := writeCSV(out, delimiter)
	if err != nil {
		return nil, nil, nil, err
	}
	return encoded, exported, audit, nil
}

// transformEAVCSV applies the anonymization plan to EAV-shaped CSV exports
// (record, [redcap_event_name,] field_name, value): rows whose field_name
// matches a blank/pseudonymize rule get their value cell rewritten, rows
// matching a drop rule are removed. A transform on the record-ID field (the
// first dictionary field) is additionally applied to the "record" column of
// every row — otherwise raw record identifiers would survive in the linking
// column. Falls back to flat handling if the EAV columns cannot be located.
func transformEAVCSV(data []byte, delimiter rune, plan transformPlan, dict dictionary) ([]byte, []string, []anonymizationAudit, error) {
	rows, err := parseCSV(data, delimiter)
	if err != nil {
		return nil, nil, nil, err
	}
	if len(rows) == 0 {
		return data, nil, buildAudit(plan, nil, "rows", nil), nil
	}
	fieldIdx, valueIdx, recordIdx := -1, -1, -1
	for i, col := range rows[0] {
		switch strings.ToLower(strings.TrimSpace(col)) {
		case "field_name":
			fieldIdx = i
		case "value":
			valueIdx = i
		case "record":
			recordIdx = i
		}
	}
	if fieldIdx < 0 || valueIdx < 0 {
		return transformFlatCSV(data, delimiter, plan, false, dict)
	}
	recordField := recordIDField(dict)
	recordMode := ""
	if recordField != "" && recordIdx >= 0 {
		recordMode = plan.modes[recordField]
	}
	transformRecord := plan.memoizedTransform(recordField)
	exported := eavExportedFields(dict)
	seen := map[string]bool{}
	for _, field := range exported {
		seen[field] = true
	}
	matched := map[string]int{}
	notes := map[string]string{}
	changed := false
	out := make([][]string, 0, len(rows))
	out = append(out, rows[0])
	for rowIdx := 1; rowIdx < len(rows); rowIdx++ {
		row := rows[rowIdx]
		field := ""
		if fieldIdx < len(row) {
			field = baseFieldName(strings.TrimSpace(row[fieldIdx]))
		}
		if field != "" && !seen[field] {
			seen[field] = true
			exported = append(exported, field)
		}
		if field != "" && plan.modes[field] == "drop" {
			matched[field]++
			changed = true
			continue
		}
		if field != "" && plan.modes[field] != "" && valueIdx < len(row) {
			row[valueIdx] = plan.transformValue(field, row[valueIdx])
			matched[field]++
			changed = true
		}
		if recordMode != "" && recordMode != "drop" && recordIdx < len(row) && row[recordIdx] != "" {
			row[recordIdx] = transformRecord(row[recordIdx])
			changed = true
			notes[recordField] = "also applied to the EAV record column"
		}
		out = append(out, row)
	}
	// Dropped fields are removed from the export, so their dictionary rows
	// must not survive in metadata.csv.
	exported = withoutDroppedFields(exported, plan)
	audit := buildAudit(plan, matched, "rows", notes)
	if !changed {
		return data, exported, audit, nil
	}
	encoded, err := writeCSV(out, delimiter)
	if err != nil {
		return nil, nil, nil, err
	}
	return encoded, exported, audit, nil
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

// recordIDField returns the project's record identifier field: REDCap defines
// it as the first field of the data dictionary.
func recordIDField(dict dictionary) string {
	if len(dict.fieldOrder) > 0 {
		return dict.fieldOrder[0]
	}
	return ""
}

// withoutDroppedFields removes fields with a drop rule from an exported-field
// list, so that dropped fields also disappear from the filtered metadata.csv.
func withoutDroppedFields(fields []string, plan transformPlan) []string {
	out := make([]string, 0, len(fields))
	for _, field := range fields {
		if plan.modes[field] == "drop" {
			continue
		}
		out = append(out, field)
	}
	return out
}

// jsonValueString renders a JSON cell value for transformation. REDCap exports
// values as strings, but numbers are normalized defensively.
func jsonValueString(v interface{}) string {
	switch s := v.(type) {
	case nil:
		return ""
	case string:
		return s
	default:
		return fmt.Sprintf("%v", s)
	}
}

// transformFlatJSON applies the anonymization plan to flat JSON exports. JSON
// exports always use raw field names as keys, so only checkbox base-name
// matching applies. Dropped keys are removed from every row.
func transformFlatJSON(data []byte, plan transformPlan) ([]byte, []string, []anonymizationAudit, error) {
	rows := make([]map[string]interface{}, 0)
	if err := json.Unmarshal(data, &rows); err != nil {
		return nil, nil, nil, err
	}
	keys := map[string]bool{}
	matchedKeys := map[string]string{} // key -> field with a transform rule
	for _, row := range rows {
		for k := range row {
			keys[k] = true
		}
	}
	exportedSet := map[string]bool{}
	exported := []string{}
	for k := range keys {
		field := baseFieldName(k)
		// A rule keyed on the expansion column itself wins over the base field.
		ruleField := ""
		if plan.modes[k] != "" {
			ruleField = k
		} else if plan.modes[field] != "" {
			ruleField = field
		}
		if ruleField != "" {
			matchedKeys[k] = ruleField
		}
		if plan.modes[ruleField] == "drop" {
			continue
		}
		if !exportedSet[field] {
			exportedSet[field] = true
			exported = append(exported, field)
		}
	}
	sort.Strings(exported)
	matched := map[string]int{}
	for _, field := range matchedKeys {
		matched[field]++
	}
	audit := buildAudit(plan, matched, "columns", nil)
	if len(matchedKeys) == 0 {
		return data, exported, audit, nil
	}
	for _, row := range rows {
		for k, field := range matchedKeys {
			if _, ok := row[k]; !ok {
				continue
			}
			if plan.modes[field] == "drop" {
				delete(row, k)
				continue
			}
			row[k] = plan.transformValue(field, jsonValueString(row[k]))
		}
	}
	out, err := json.Marshal(rows)
	if err != nil {
		return nil, nil, nil, err
	}
	return out, exported, audit, nil
}

// transformEAVJSON applies the anonymization plan to EAV JSON rows. Like the
// CSV variant, a transform on the record-ID field is also applied to the
// "record" key of every row. Falls back to flat handling when rows are not
// EAV-shaped.
func transformEAVJSON(data []byte, plan transformPlan, dict dictionary) ([]byte, []string, []anonymizationAudit, error) {
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
		return transformFlatJSON(data, plan)
	}
	recordField := recordIDField(dict)
	recordMode := ""
	if recordField != "" {
		recordMode = plan.modes[recordField]
	}
	transformRecord := plan.memoizedTransform(recordField)
	exported := eavExportedFields(dict)
	seen := map[string]bool{}
	for _, field := range exported {
		seen[field] = true
	}
	matched := map[string]int{}
	notes := map[string]string{}
	changed := false
	out := make([]map[string]interface{}, 0, len(rows))
	for _, row := range rows {
		name, _ := row["field_name"].(string)
		field := baseFieldName(strings.TrimSpace(name))
		if field != "" && !seen[field] {
			seen[field] = true
			exported = append(exported, field)
		}
		if field != "" && plan.modes[field] == "drop" {
			matched[field]++
			changed = true
			continue
		}
		if field != "" && plan.modes[field] != "" {
			if _, ok := row["value"]; ok {
				row["value"] = plan.transformValue(field, jsonValueString(row["value"]))
				changed = true
			}
			matched[field]++
		}
		if recordMode != "" && recordMode != "drop" {
			if rec, ok := row["record"]; ok {
				if recStr := jsonValueString(rec); recStr != "" {
					row["record"] = transformRecord(recStr)
					changed = true
					notes[recordField] = "also applied to the EAV record column"
				}
			}
		}
		out = append(out, row)
	}
	exported = withoutDroppedFields(exported, plan)
	audit := buildAudit(plan, matched, "rows", notes)
	if !changed {
		return data, exported, audit, nil
	}
	encoded, err := json.Marshal(out)
	if err != nil {
		return nil, nil, nil, err
	}
	return encoded, exported, audit, nil
}

// processExportData routes the raw API payload through the mode-appropriate
// transform implementation and reports the exported dictionary fields plus the
// anonymization audit. Dropping the record-ID field is rejected for EAV
// exports: the record column cannot be removed without destroying the EAV
// structure, and leaving it would silently keep the identifiers.
func processExportData(data []byte, opts pluginOptions, plan transformPlan, dict dictionary) ([]byte, []string, []anonymizationAudit, error) {
	if isEAV(opts) {
		if field := recordIDField(dict); field != "" && plan.modes[field] == "drop" {
			return nil, nil, nil, fmt.Errorf("dropping the record id field (%s) is not supported for EAV exports: use pseudonymize or blank instead", field)
		}
	}
	switch {
	case opts.DataFormat == "json" && isEAV(opts):
		return transformEAVJSON(data, plan, dict)
	case opts.DataFormat == "json":
		return transformFlatJSON(data, plan)
	case isEAV(opts):
		return transformEAVCSV(data, reportDelimiter(opts), plan, dict)
	default:
		return transformFlatCSV(data, reportDelimiter(opts), plan, headersAreLabels(opts), dict)
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

// variableSelectItems builds a sorted, deduplicated list of SelectItem values
// for the anonymization table. Identifier-tagged fields (resolved through the
// checkbox base name) are returned with Selected=true, signalling the frontend
// to auto-blank them; free-text fields carry a PHI-risk note.
func variableSelectItems(fields []string, dict dictionary) []types.SelectItem {
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
		out = append(out, types.SelectItem{
			Label:    field,
			Value:    field,
			Selected: dict.identifier[baseFieldName(field)],
			Note:     phiRiskNote(dict, field),
		})
	}
	return out
}

// listVariablesFromReport fetches column headers from a report export (CSV
// header-only request). The header request is always raw, comma-delimited:
// transform rules are keyed by field name, so the variable list must contain
// field names even when the actual export uses label headers or another
// delimiter. Falls back to the full dictionary field list if the report
// header fetch fails.
func listVariablesFromReport(ctx context.Context, baseURL, token, reportID string, _ pluginOptions) ([]types.SelectItem, error) {
	dict, dictErr := fetchDictionary(ctx, baseURL, token)

	form := baseForm(token, "report", "csv")
	form.Set("report_id", reportID)

	fields, err := redcapRequestHeaderOnly(ctx, baseURL, form, ',')
	if err != nil {
		// Fallback: derive the field list from the data dictionary.
		if dictErr != nil {
			return nil, dictErr
		}
		fields = dict.fieldOrder
	}
	return variableSelectItems(fields, dict), nil
}

// listVariablesFromMetadata returns all project fields from the data
// dictionary. Used for record export mode where there is no report to derive
// headers from.
func listVariablesFromMetadata(ctx context.Context, baseURL, token string) ([]types.SelectItem, error) {
	dict, err := fetchDictionary(ctx, baseURL, token)
	if err != nil {
		return nil, err
	}
	return variableSelectItems(dict.fieldOrder, dict), nil
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

// exportProjectXML fetches the CDISC ODM project metadata (metadata only — no
// record data) for the optional project_metadata.xml sidecar.
func exportProjectXML(ctx context.Context, baseURL, token string) ([]byte, error) {
	form := url.Values{}
	form.Set("token", token)
	form.Set("content", "project_xml")
	form.Set("returnMetadataOnly", "true")
	form.Set("returnFormat", "json")
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
	TransformModes              map[string]string // field -> transform mode, for echo redaction
	RecordIDField               string
	KeyFingerprint              string            // SHA-256 fingerprint of the HMAC key (never the key itself)
	ExtraFiles                  map[string]string // additional manifest file entries (sidecars, ODM)
}

// filterLogicFieldRe extracts the field names referenced by a REDCap filter
// logic expression: [field], [field(code)], [event][field], ...
var filterLogicFieldRe = regexp.MustCompile(`\[([a-zA-Z0-9_]+)`)

// redactedEcho replaces a manifest parameter echo that would leak values of an
// anonymized field. The manifest documents that redaction happened instead of
// silently omitting the parameter.
func redactedEcho(note string) map[string]interface{} {
	return map[string]interface{}{
		"redacted": true,
		"note":     note,
	}
}

// recordsEcho returns the manifest echo for the records filter. When the
// record-ID field is anonymized, echoing the requested record IDs verbatim
// would leak the very identifiers the transform removed from the data.
func recordsEcho(opts pluginOptions, extras manifestExtras) interface{} {
	if len(opts.Records) == 0 {
		return opts.Records
	}
	if extras.RecordIDField != "" && extras.TransformModes[extras.RecordIDField] != "" {
		return redactedEcho(fmt.Sprintf("%d record ids hidden: the record id field (%s) is anonymized", len(opts.Records), extras.RecordIDField))
	}
	return opts.Records
}

// filterLogicEcho returns the manifest echo for filterLogic. Filter logic can
// embed literal values ([name] = "John"), so it is redacted whenever it
// references an anonymized field.
func filterLogicEcho(opts pluginOptions, extras manifestExtras) interface{} {
	if opts.FilterLogic == "" || len(extras.TransformModes) == 0 {
		return opts.FilterLogic
	}
	for _, m := range filterLogicFieldRe.FindAllStringSubmatch(opts.FilterLogic, -1) {
		if extras.TransformModes[baseFieldName(m[1])] != "" {
			return redactedEcho("filter logic hidden: it references anonymized fields")
		}
	}
	return opts.FilterLogic
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
			"records":              recordsEcho(opts, extras),
			"filter_logic":         filterLogicEcho(opts, extras),
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
	for key, path := range extras.ExtraFiles {
		if path != "" {
			manifest["files"].(map[string]string)[key] = path
		}
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
	if extras.KeyFingerprint != "" {
		manifest["anonymization"] = map[string]interface{}{
			"method":          "hmac-sha256",
			"key_fingerprint": extras.KeyFingerprint,
			"note":            "pseudonyms are hex-encoded HMAC-SHA256 values; the same key reproduces the same pseudonyms across exports",
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

	plan, err := buildTransformPlan(opts)
	if err != nil {
		return generatedBundle{}, err
	}
	dataBytes, dataFields, audit, err := processExportData(rawData, opts, plan, dict)
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

	// Optional CDISC ODM sidecar (metadata only); failure must not block the export.
	odmPath := ""
	if odmBytes, odmErr := exportProjectXML(ctx, baseURL, token); odmErr != nil {
		warnings = append(warnings, fmt.Sprintf("project metadata (ODM) export failed: %v", odmErr))
	} else {
		odmPath = basePath + "/project_metadata.xml"
		files[odmPath] = odmBytes
	}

	projectID, projectTitle := projectIdentity(projectInfoBytes)
	extras := manifestExtras{
		Audit:            audit,
		FileUploadFields: dict.fileUploadFields(),
		ProjectID:        projectID,
		ProjectTitle:     projectTitle,
		TransformModes:   plan.modes,
		RecordIDField:    recordIDField(dict),
		KeyFingerprint:   plan.keyFingerprint,
		ExtraFiles: map[string]string{
			"project_metadata": odmPath,
			"croissant":        basePath + "/croissant.json",
			"ro_crate":         basePath + "/ro-crate-metadata.json",
			"ddi_cdi":          basePath + "/ddi-cdi.jsonld",
		},
	}
	// In an unfiltered flat records export, dictionary fields missing from the
	// output reveal server-side stripping (token export rights). With filters
	// or report definitions the diff is expected, so it is not recorded.
	// Client-side dropped fields are excluded: their absence is deliberate and
	// already documented by the anonymization audit.
	if opts.ExportMode == "records" && !isEAV(opts) &&
		len(opts.Fields) == 0 && len(opts.Forms) == 0 && len(opts.Events) == 0 {
		exported := make(map[string]bool, len(dataFields))
		for _, field := range dataFields {
			exported[field] = true
		}
		for _, field := range dict.fieldOrder {
			if !exported[field] && plan.modes[field] != "drop" {
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

	// Metadata sidecars (Croissant, RO-Crate, DDI-CDI) are rendered from one
	// normalized model over the final bundle contents (incl. manifest.json).
	// They never block the export; failures are logged.
	model := buildSidecarModel(opts, plan, dict, basePath, files, dataPath, redcapVersion, projectID, projectTitle)
	mime := map[string]string{}
	for _, warning := range addSidecars(model, basePath, files, mime) {
		logging.Logger.Printf("redcap2: %s", warning)
	}

	logging.Logger.Printf("redcap2: generated %d virtual files (mode: %s, report: %s)", len(files), opts.ExportMode, reportID)
	return generatedBundle{
		ReportID: reportID,
		Files:    files,
		Mime:     mime,
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
		// The key participates in the cache key (different keys produce
		// different pseudonyms) but is only ever used as MD5 input here —
		// it is never stored or logged in recoverable form.
		PseudonymizationKey: opts.PseudonymizationKey,
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
