package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// Upstream is the latest known state of a grammar's upstream repo.
type Upstream struct {
	// Release is the latest release/tag name, or "" if the repo has none.
	Release string
	// CommitDate is the date (YYYY-MM-DD) of the latest commit on the default
	// branch — always populated on success, the resilient fallback when a repo
	// has no releases at all.
	CommitDate string
	// Kind records how the value was resolved, for the report ("release" or
	// "commit").
	Kind string
}

// UpstreamSource resolves the latest upstream state for an owner/repo slug. It
// is an interface so tests inject a deterministic fake instead of hitting the
// live GitHub API.
type UpstreamSource interface {
	Latest(ctx context.Context, repo string) (Upstream, error)
}

// githubSource queries the GitHub REST API. It is rate-limit-aware: on a
// secondary-rate-limit / 403-with-reset response it honours the reset header
// once before giving up, so a monthly cron over ~28 repos stays well inside the
// authenticated 5000/hr budget without hammering on a throttle.
type githubSource struct {
	client *http.Client
	token  string
	// base overridden in tests; defaults to the real API.
	base string
}

func (g *githubSource) apiBase() string {
	if g.base != "" {
		return g.base
	}
	return "https://api.github.com"
}

func (g *githubSource) Latest(ctx context.Context, repo string) (Upstream, error) {
	// 1) Try the latest published release.
	rel, err := g.latestRelease(ctx, repo)
	if err != nil {
		return Upstream{}, err
	}

	// 2) Always get the default-branch latest commit date — it is the resilient
	// fallback (repos with no releases) and the "how far behind" reference.
	commitDate, cerr := g.latestCommitDate(ctx, repo)
	if cerr != nil && rel == "" {
		// No release AND no commit info — can't say anything about this repo.
		return Upstream{}, cerr
	}

	if rel != "" {
		return Upstream{Release: rel, CommitDate: commitDate, Kind: "release"}, nil
	}
	return Upstream{Release: "", CommitDate: commitDate, Kind: "commit"}, nil
}

// latestRelease returns the latest release tag, or "" if the repo has none
// (a 404 from /releases/latest is the documented "no releases" signal, not an
// error).
func (g *githubSource) latestRelease(ctx context.Context, repo string) (string, error) {
	var body struct {
		TagName string `json:"tag_name"`
	}
	status, err := g.getJSON(ctx, fmt.Sprintf("/repos/%s/releases/latest", repo), &body)
	if err != nil {
		return "", err
	}
	if status == http.StatusNotFound {
		return "", nil // no releases — fall back to commit
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("%s releases/latest: HTTP %d", repo, status)
	}
	return body.TagName, nil
}

// latestCommitDate returns the YYYY-MM-DD date of the most recent commit on the
// default branch.
func (g *githubSource) latestCommitDate(ctx context.Context, repo string) (string, error) {
	var body []struct {
		Commit struct {
			Committer struct {
				Date time.Time `json:"date"`
			} `json:"committer"`
		} `json:"commit"`
	}
	status, err := g.getJSON(ctx, fmt.Sprintf("/repos/%s/commits?per_page=1", repo), &body)
	if err != nil {
		return "", err
	}
	if status != http.StatusOK {
		return "", fmt.Errorf("%s commits: HTTP %d", repo, status)
	}
	if len(body) == 0 {
		return "", fmt.Errorf("%s: no commits", repo)
	}
	return body[0].Commit.Committer.Date.Format("2006-01-02"), nil
}

// getJSON performs a GET, decodes JSON into out on 200, and returns the status
// code. A 404 is returned as a status (not an error) so callers can treat
// "no releases" specially. It retries once on a rate-limit throttle.
func (g *githubSource) getJSON(ctx context.Context, path string, out any) (int, error) {
	for attempt := 0; attempt < 2; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, g.apiBase()+path, nil)
		if err != nil {
			return 0, err
		}
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
		req.Header.Set("User-Agent", "grafel-grammar-freshness")
		if g.token != "" {
			req.Header.Set("Authorization", "Bearer "+g.token)
		}

		resp, err := g.client.Do(req)
		if err != nil {
			return 0, err
		}

		// Rate-limit handling: a 403/429 with remaining==0 means wait for reset.
		if (resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests) &&
			resp.Header.Get("X-RateLimit-Remaining") == "0" && attempt == 0 {
			resp.Body.Close()
			if !g.waitForReset(ctx, resp.Header.Get("X-RateLimit-Reset")) {
				return resp.StatusCode, fmt.Errorf("rate limited and reset wait cancelled")
			}
			continue
		}

		if resp.StatusCode == http.StatusNotFound {
			resp.Body.Close()
			return resp.StatusCode, nil
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return resp.StatusCode, nil
		}
		err = json.NewDecoder(resp.Body).Decode(out)
		resp.Body.Close()
		return resp.StatusCode, err
	}
	return 0, fmt.Errorf("exhausted retries")
}

// waitForReset blocks until the rate-limit reset epoch (capped at 90s so a
// stuck cron job doesn't hang forever). Returns false if the context is done.
func (g *githubSource) waitForReset(ctx context.Context, resetHeader string) bool {
	wait := 60 * time.Second
	if epoch, err := strconv.ParseInt(resetHeader, 10, 64); err == nil {
		if d := time.Until(time.Unix(epoch, 0)); d > 0 {
			wait = d
		}
	}
	if wait > 90*time.Second {
		wait = 90 * time.Second
	}
	select {
	case <-ctx.Done():
		return false
	case <-time.After(wait):
		return true
	}
}
