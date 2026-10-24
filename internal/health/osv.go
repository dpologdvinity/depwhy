package health

import (
	"context"
	"fmt"
	"net/http"
	"sort"
)

// osvBatchSize is the most queries OSV accepts in one querybatch request.
const osvBatchSize = 1000

// osvEcosystem maps depwhy's ecosystem names to OSV's.
var osvEcosystem = map[string]string{"npm": "npm", "python": "PyPI", "cargo": "crates.io"}

// advisories asks OSV which advisories affect each package, in batches. The
// result maps each package to its sorted, de-duplicated advisory IDs (empty when
// none). On error nothing is returned: a partial answer could not be told apart
// from "no advisories".
func (c *Client) advisories(ctx context.Context, pkgs []Pkg) (map[Pkg][]string, error) {
	out := make(map[Pkg][]string, len(pkgs))
	for start := 0; start < len(pkgs); start += osvBatchSize {
		batch := pkgs[start:min(start+osvBatchSize, len(pkgs))]
		ids, err := c.osvBatch(ctx, batch)
		if err != nil {
			return nil, err
		}
		for i, p := range batch {
			out[p] = ids[i]
		}
	}
	return out, nil
}

// osvBatch sends one querybatch request. Results come back in query order, so
// they are matched to packages by position; a different count is an error.
func (c *Client) osvBatch(ctx context.Context, batch []Pkg) ([][]string, error) {
	type pkgRef struct {
		Name      string `json:"name"`
		Ecosystem string `json:"ecosystem"`
	}
	type query struct {
		Package pkgRef `json:"package"`
		Version string `json:"version"`
	}
	req := struct {
		Queries []query `json:"queries"`
	}{}
	for _, p := range batch {
		req.Queries = append(req.Queries, query{pkgRef{p.Name, osvEcosystem[p.Ecosystem]}, p.Version})
	}
	var resp struct {
		Results []struct {
			Vulns []struct {
				ID string `json:"id"`
			} `json:"vulns"`
		} `json:"results"`
	}
	if _, err := c.do(ctx, http.MethodPost, c.OSVURL+"/v1/querybatch", req, &resp); err != nil {
		return nil, err
	}
	if len(resp.Results) != len(batch) {
		return nil, fmt.Errorf("OSV returned %d results for %d queries", len(resp.Results), len(batch))
	}
	out := make([][]string, len(batch))
	for i, r := range resp.Results {
		seen := map[string]bool{}
		for _, v := range r.Vulns {
			if v.ID != "" && !seen[v.ID] {
				seen[v.ID] = true
				out[i] = append(out[i], v.ID)
			}
		}
		sort.Strings(out[i])
	}
	return out, nil
}
