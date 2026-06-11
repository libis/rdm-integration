// Author: Eryk Kulikowski @ KU Leuven (2024). Apache 2.0 License

package globus

import (
	"context"
	"encoding/json"
	"fmt"
	"integration/app/logging"
	"integration/app/plugin/types"
	"net/url"
	"path"
	"strings"
)

func Options(ctx context.Context, params types.OptionsRequest) ([]types.SelectItem, error) {
	if params.Url == "" || params.Token == "" {
		return nil, fmt.Errorf("options: missing parameters: expected url, token, got: %+v", params)
	}
	return listFolderItems(ctx, params)
}

func listFolderItems(ctx context.Context, params types.OptionsRequest) ([]types.SelectItem, error) {
	if params.Option != "" {
		// User expanded a specific node — return flat children for that folder.
		return doListFolderItems(ctx, params)
	}
	return resolveAndBuildInitialTree(ctx, params)
}

// resolveAndBuildInitialTree handles the initial folder picker for an endpoint.
//
// Globus endpoints differ wildly in how they resolve shorthand inputs like
// "/~/" or "/{server_default}/":
//
//   - GCS personal endpoints (Linux, macOS, Windows): "/~/" resolves to the
//     user's home directory and the LS response carries the absolute path
//     (e.g. "/home/user/", "/Users/user/", "C:/Users/user/").
//   - Mapped collections backed by POSIX storage: same — "/~/" usually
//     resolves to the user's home.
//   - Mapped collections backed by iRODS (e.g. KU Leuven's
//     ghum.irods.icts.kuleuven.be): "/~/" lists fine but the response's
//     absolute_path comes back unchanged ("/~/") — Globus does not resolve
//     it to the user's iRODS home (e.g. "/ghum/home/u0050020/").
//   - Some collections set DefaultDirectory to a templated path like
//     "/{server_default}/" or to an explicit absolute path, others leave it
//     empty entirely.
//
// To survive all of these variants we:
//
//  1. Build an ordered list of candidate paths (DefaultDirectory if any,
//     "/~/", "/").
//  2. For each candidate, attempt the listing; treat NotFound errors as a
//     signal to try the next candidate.
//  3. From a successful listing, derive the most authoritative resolved
//     directory we can: response.absolute_path > parent of first child >
//     queried path.
//  4. If the resolved path is "meaningful" (a real concrete absolute path,
//     not an unresolved shorthand and not just "/"), build a nested
//     hierarchy expanded down to that directory.
//  5. Otherwise fall back to returning whatever flat listing succeeded —
//     the user can still navigate manually. Never auto-mark "/" or any
//     unresolved node as selected, so the user is not nudged into writing
//     to a path they may not own.
//
// Every step is logged so we can diagnose new endpoint quirks quickly.
func resolveAndBuildInitialTree(ctx context.Context, params types.OptionsRequest) ([]types.SelectItem, error) {
	endpoint, endpointErr := getEndpoint(ctx, params)
	if endpointErr != nil && isNotFoundError(endpointErr) {
		// Frontend placeholders (e.g. "start") should not surface as HTTP 500.
		logging.Logger.Printf("globus options: endpoint %q not found, returning empty list", params.RepoName)
		return []types.SelectItem{}, nil
	}
	if endpointErr != nil {
		// Don't fail — log and try the listing fallbacks anyway.
		logging.Logger.Printf("globus options: getEndpoint error for %q: %v (continuing with fallback candidates)", params.RepoName, endpointErr)
	}

	candidates := buildCandidatePaths(endpoint)
	logging.Logger.Printf("globus options: candidate starting paths for %q: %v (defaultDirectory=%q)", params.RepoName, candidates, endpoint.DefaultDirectory)

	var bestFallback *attemptResult
	var lastErr error
	for _, candidate := range candidates {
		attempt, err := tryListPath(ctx, params, candidate)
		if err != nil {
			if isNotFoundError(err) {
				logging.Logger.Printf("globus options: candidate %q for %q returned not-found, trying next", candidate, params.RepoName)
				continue
			}
			lastErr = err
			logging.Logger.Printf("globus options: candidate %q for %q failed (%v), trying next", candidate, params.RepoName, err)
			continue
		}
		logging.Logger.Printf("globus options: candidate %q for %q listed: items=%d responseAbsolutePath=%q resolvedDir=%q meaningful=%v", candidate, params.RepoName, len(attempt.items), attempt.responseAbsolutePath, attempt.resolvedDir, attempt.meaningful)
		if attempt.meaningful {
			return buildHierarchy(attempt.resolvedDir, attempt.items), nil
		}
		// Track the best fallback: prefer the first attempt that produced any
		// folder entries. An empty listing of "/~/" is much less useful than a
		// non-empty listing of "/" (which lets the user navigate further).
		if bestFallback == nil || (len(bestFallback.items) == 0 && len(attempt.items) > 0) {
			bestFallback = attempt
		}
	}
	firstSuccessful := bestFallback

	if firstSuccessful != nil {
		// No candidate gave us a real absolute path. Show whatever flat
		// listing we managed to obtain — much better UX than a single "~".
		// Do NOT mark any node as selected: we don't want to nudge the user
		// to upload into "/" or another unresolved path.
		logging.Logger.Printf("globus options: %q — no candidate produced a meaningful resolved path; returning flat listing of %q (%d items)", params.RepoName, firstSuccessful.candidate, len(firstSuccessful.items))
		return firstSuccessful.items, nil
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return []types.SelectItem{}, nil
}

// attemptResult captures the outcome of listing a single candidate path.
type attemptResult struct {
	candidate            string
	responseAbsolutePath string
	resolvedDir          string
	items                []types.SelectItem
	meaningful           bool
}

func tryListPath(ctx context.Context, params types.OptionsRequest, candidate string) (*attemptResult, error) {
	items, absolutePath, err := listFolderItemsResolvingPath(ctx, params, candidate)
	if err != nil {
		return nil, err
	}
	resolved := pickResolvedDir(candidate, absolutePath, items)
	return &attemptResult{
		candidate:            candidate,
		responseAbsolutePath: absolutePath,
		resolvedDir:          resolved,
		items:                items,
		meaningful:           isMeaningfulHierarchyPath(resolved),
	}, nil
}

// listFolderItemsResolvingPath performs a single non-recursive listing and
// returns the directory entries (folders only, as SelectItem) along with the
// listed directory's resolved absolute_path as reported by Globus.
func listFolderItemsResolvingPath(ctx context.Context, params types.OptionsRequest, requestedPath string) ([]types.SelectItem, string, error) {
	folder := normalizeEndpointPath(requestedPath)
	if folder == "" {
		folder = "/"
	}
	entries, absolutePath, err := listOnce(ctx, folder, params.Url+"/operation/endpoint/"+params.RepoName+"/ls", params.Token)
	if err != nil {
		return []types.SelectItem{}, absolutePath, err
	}
	res := make([]types.SelectItem, 0, len(entries))
	for _, e := range entries {
		if e.IsDir {
			res = append(res, types.SelectItem{Label: e.Name, Value: e.Id})
		}
	}
	return res, absolutePath, nil
}

// buildCandidatePaths returns the ordered list of paths to try when
// determining where to anchor the folder tree on initial load. Order matters:
// the first candidate that yields a meaningful absolute_path wins.
func buildCandidatePaths(endpoint Response) []string {
	seen := map[string]bool{}
	candidates := []string{}
	add := func(p string) {
		if p == "" {
			return
		}
		if seen[p] {
			return
		}
		seen[p] = true
		candidates = append(candidates, p)
	}
	if endpoint.DefaultDirectory != "" {
		resolved := resolveDefaultDirectory(endpoint.DefaultDirectory)
		add(resolved)
	}
	// Globus shorthand for the user's home — resolves on most endpoints.
	add("/~/")
	// Root — last-resort fallback. Always show *something*.
	add("/")
	return candidates
}

// pickResolvedDir picks the most authoritative absolute path representation
// of the listed directory. Preference:
//  1. Globus response.absolute_path (when non-empty AND not just an echo of
//     the queried shorthand).
//  2. Parent of the first child entry's Id (which carries the response's
//     absolute_path inside it via listDirEntries).
//  3. The candidate path itself (last resort).
func pickResolvedDir(candidate, responseAbsolutePath string, items []types.SelectItem) string {
	normalizedCandidate := ensureTrailingSlash(normalizeEndpointPath(candidate))
	if responseAbsolutePath != "" {
		normalized := ensureTrailingSlash(normalizeEndpointPath(responseAbsolutePath))
		if normalized != "" && normalized != normalizedCandidate {
			return normalized
		}
		// Even when echoed, prefer the response's representation — it is at
		// worst as informative as the candidate.
		if normalized != "" {
			return normalized
		}
	}
	for _, item := range items {
		v, ok := item.Value.(string)
		if !ok || v == "" {
			continue
		}
		parent := path.Dir(strings.TrimSuffix(v, "/"))
		if parent == "" || parent == "." {
			continue
		}
		return ensureTrailingSlash(parent)
	}
	return normalizedCandidate
}

// isMeaningfulHierarchyPath reports whether the given path is a real,
// concrete absolute directory worth building a nested hierarchy for.
// Filters out:
//   - empty strings
//   - "/" (root) — flat listing is more appropriate
//   - shorthand placeholders ("~", "{server_default}", etc.) that endpoints
//     occasionally fail to resolve
func isMeaningfulHierarchyPath(p string) bool {
	if p == "" || p == "/" {
		return false
	}
	if strings.Contains(p, "~") || strings.Contains(p, "{") || strings.Contains(p, "}") {
		return false
	}
	return strings.Trim(p, "/") != ""
}

func ensureTrailingSlash(p string) string {
	if p == "" {
		return p
	}
	if !strings.HasSuffix(p, "/") {
		p = p + "/"
	}
	return p
}

// buildHierarchy constructs a nested tree of SelectItems from the root "/"
// down to targetDir, with targetDir's children populated and targetDir
// pre-selected and pre-expanded. Each ancestor along the way is also
// pre-expanded so the user lands directly on the target directory.
//
// If targetDir is not a meaningful hierarchy path (root, empty, contains
// shorthand placeholders) the function returns the children flat — the
// caller is expected to gate this with isMeaningfulHierarchyPath, but we
// double-check here too as a defense in depth.
func buildHierarchy(targetDir string, children []types.SelectItem) []types.SelectItem {
	targetDir = ensureTrailingSlash(targetDir)
	if !strings.HasPrefix(targetDir, "/") {
		targetDir = "/" + targetDir
	}
	if !isMeaningfulHierarchyPath(targetDir) {
		// Shouldn't happen given our gate, but be defensive.
		return children
	}

	segments := splitPathSegments(targetDir)
	if len(segments) == 0 {
		return children
	}

	// Build the deepest node (target) first, then wrap ancestors around it.
	targetNode := types.SelectItem{
		Label:    segments[len(segments)-1],
		Value:    "/" + strings.Join(segments, "/") + "/",
		Selected: true,
		Expanded: true,
		Children: children,
	}

	current := targetNode
	for i := len(segments) - 2; i >= 0; i-- {
		ancestorPath := "/" + strings.Join(segments[:i+1], "/") + "/"
		current = types.SelectItem{
			Label:    segments[i],
			Value:    ancestorPath,
			Expanded: true,
			Children: []types.SelectItem{current},
		}
	}

	return []types.SelectItem{current}
}

// splitPathSegments splits an absolute path into non-empty segments, e.g.
// "/ghum/home/u0050020/" -> ["ghum", "home", "u0050020"]. Empty segments
// (from doubled slashes) are dropped defensively.
func splitPathSegments(p string) []string {
	trimmed := strings.Trim(p, "/")
	if trimmed == "" {
		return nil
	}
	parts := strings.Split(trimmed, "/")
	out := parts[:0]
	for _, s := range parts {
		if s != "" {
			out = append(out, s)
		}
	}
	return out
}

func doListFolderItems(ctx context.Context, params types.OptionsRequest) (res []types.SelectItem, err error) {
	folder := normalizeEndpointPath(params.Option)
	if folder == "" {
		folder = "/"
	}
	items, err := listItems(ctx, folder, params.Url+"/operation/endpoint/"+params.RepoName+"/ls", params.Token, params.User, false)
	res = []types.SelectItem{}
	if err != nil {
		return res, err
	}
	for _, e := range items {
		if e.IsDir {
			res = append(res, types.SelectItem{Label: e.Name, Value: e.Id})
		}
	}
	return res, nil
}

func getEndpoint(ctx context.Context, params types.OptionsRequest) (Response, error) {
	url := params.Url + "/endpoint/" + params.RepoName
	res, err := DoGlobusRequest(ctx, url, "GET", params.Token, nil)
	if err != nil {
		return Response{}, err
	}
	response := Response{}
	err = json.Unmarshal(res, &response)
	if err != nil {
		return Response{}, err
	}
	if response.Code != "" && response.Message != "" {
		return response, fmt.Errorf("%v: %v", response.Code, response.Message)
	}
	return response, nil
}

func isNotFoundError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "ClientError.NotFound") || strings.Contains(msg, "EndpointNotFound")
}

func resolveDefaultDirectory(defaultDirectory string) string {
	raw := strings.TrimSpace(defaultDirectory)
	decoded := raw
	if d, err := url.PathUnescape(raw); err == nil && d != "" {
		decoded = d
	}
	if (strings.Contains(raw, "{") && strings.Contains(raw, "}")) ||
		(strings.Contains(decoded, "{") && strings.Contains(decoded, "}")) {
		return "/~/"
	}
	return normalizeEndpointPath(raw)
}
