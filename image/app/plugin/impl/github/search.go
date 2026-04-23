// Author: Eryk Kulikowski @ KU Leuven (2023). Apache 2.0 License

package github

import (
	"context"
	"encoding/json"
	"fmt"
	"integration/app/plugin/types"
	"io"
	"net/http"
	"net/url"
	"strings"
)

type SearchResults struct {
	Items []Item `json:"items"`
}

type Item struct {
	FullName string `json:"full_name"`
}

// Search returns repositories matching the query, prioritising repositories
// that the authenticated user owns, collaborates on, or is an organization
// member of. GitHub's global /search/repositories endpoint ranks by stars and
// rarely surfaces a user's own repositories (especially private ones), so we
// first list the user's own repositories (/user/repos) and filter them by the
// query, then augment the result with a scoped /search/repositories call.
//
// Supported query forms:
//   - empty string           -> list the user's repositories
//   - "owner/name" fragment  -> search with +user:<owner> qualifier
//   - anything else          -> list the user's repositories filtered by the
//     query AND a global search for the term
//
// requiredScopes is the set of OAuth scopes the user must grant for the
// integration to see private repositories (and repositories from organizations
// the user belongs to). If any of these is missing from the token, the backend
// returns a `*scopes*...*scopes*` error so the frontend can trigger a
// re-authorization with the additional scopes appended to the authorize URL.
var requiredScopes = []string{"repo", "read:org"}

func Search(ctx context.Context, params types.OptionsRequest) ([]types.SelectItem, error) {
	token := params.Token
	if token == "" {
		return nil, fmt.Errorf("not authorized")
	}

	term := strings.TrimSpace(params.RepoName)
	seen := map[string]bool{}
	res := []types.SelectItem{}

	// 1) Repositories the user has access to (owner, collaborator, org member).
	userRepos, granted, err := listUserRepos(ctx, token)
	if err != nil {
		return nil, err
	}
	if missing := missingScopes(granted); len(missing) > 0 {
		// Trigger re-auth: frontend parses `*scopes*<space-separated>*scopes*`
		// and redirects the user to the OAuth authorize URL with the extra
		// scopes appended. See connect.component.ts:getOptions error handler.
		return nil, fmt.Errorf("*scopes*%s*scopes*", strings.Join(missing, " "))
	}
	lowerTerm := strings.ToLower(term)
	for _, r := range userRepos {
		if term == "" || strings.Contains(strings.ToLower(r.FullName), lowerTerm) {
			if !seen[r.FullName] {
				seen[r.FullName] = true
				res = append(res, types.SelectItem{Label: r.FullName, Value: r.FullName})
			}
		}
	}

	// 2) Global search, scoped by owner when a "owner/name" fragment is typed.
	if term != "" {
		q := term
		if slash := strings.Index(term, "/"); slash > 0 && slash < len(term)-1 {
			owner := term[:slash]
			name := term[slash+1:]
			q = name + "+user:" + owner
		}
		searchItems, err := searchRepositories(ctx, token, q)
		if err != nil {
			return nil, err
		}
		for _, r := range searchItems {
			if !seen[r.FullName] {
				seen[r.FullName] = true
				res = append(res, types.SelectItem{Label: r.FullName, Value: r.FullName})
			}
		}
	}

	return res, nil
}

// missingScopes returns the subset of requiredScopes not present in the
// granted list. A granted scope satisfies a requirement when it equals the
// required value or is a broader parent (e.g. "repo" covers "repo:status";
// "admin:org" covers "read:org").
func missingScopes(granted []string) []string {
	has := map[string]bool{}
	for _, g := range granted {
		has[g] = true
	}
	missing := []string{}
	for _, req := range requiredScopes {
		if has[req] {
			continue
		}
		// parent/umbrella scope support for a few common cases
		if req == "read:org" && (has["admin:org"] || has["write:org"]) {
			continue
		}
		missing = append(missing, req)
	}
	return missing
}

func listUserRepos(ctx context.Context, token string) ([]Item, []string, error) {
	// per_page capped at 100 by GitHub; keep a single page to bound latency.
	u := "https://api.github.com/user/repos?per_page=100&sort=pushed&affiliation=owner,collaborator,organization_member"
	b, status, headers, err := githubGet(ctx, u, token)
	if err != nil {
		return nil, nil, fmt.Errorf("listing user repositories failed: %v", err)
	}
	if status != http.StatusOK {
		return nil, nil, fmt.Errorf("listing user repositories failed: %d - %s", status, string(b))
	}
	items := []Item{}
	if err := json.Unmarshal(b, &items); err != nil {
		return nil, nil, fmt.Errorf("listing user repositories failed: %v", err)
	}
	return items, parseScopes(headers.Get("X-OAuth-Scopes")), nil
}

func searchRepositories(ctx context.Context, token, q string) ([]Item, error) {
	// q is already composed (may contain the literal '+' separator expected by
	// GitHub qualifiers), so we only escape characters that would break the URL.
	u := "https://api.github.com/search/repositories?q=" + strings.ReplaceAll(url.PathEscape(q), "%2B", "+")
	b, status, _, err := githubGet(ctx, u, token)
	if err != nil {
		return nil, fmt.Errorf("search failed: %v", err)
	}
	if status != http.StatusOK {
		return nil, fmt.Errorf("search failed: %d - %s", status, string(b))
	}
	results := SearchResults{}
	if err := json.Unmarshal(b, &results); err != nil {
		return nil, fmt.Errorf("search failed: %v", err)
	}
	return results.Items, nil
}

func githubGet(ctx context.Context, u, token string) ([]byte, int, http.Header, error) {
	request, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return nil, 0, nil, err
	}
	request.Header.Add("Accept", "application/vnd.github+json")
	request.Header.Add("Authorization", "Bearer "+token)
	request.Header.Add("X-GitHub-Api-Version", "2022-11-28")
	r, err := http.DefaultClient.Do(request)
	if err != nil {
		return nil, 0, nil, err
	}
	defer r.Body.Close()
	b, _ := io.ReadAll(r.Body)
	return b, r.StatusCode, r.Header, nil
}

// parseScopes turns GitHub's comma-separated X-OAuth-Scopes header value into
// a slice, trimming whitespace and skipping empty entries.
func parseScopes(header string) []string {
	if header == "" {
		return nil
	}
	parts := strings.Split(header, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
