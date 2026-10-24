package health

import (
	"context"
	"net/http"
	"net/url"
)

// status asks the package's registry whether p is deprecated (npm) or yanked
// (PyPI, crates.io). A 404 means the registry does not know the version, for
// example a private package; that is "no information", not an error.
func (c *Client) status(ctx context.Context, p Pkg) (Info, error) {
	name, version := url.PathEscape(p.Name), url.PathEscape(p.Version)
	var info Info
	switch p.Ecosystem {
	case "npm":
		// A scoped name's slash must be sent escaped: @x%2Fy.
		var resp struct {
			Deprecated any `json:"deprecated"`
		}
		if _, err := c.do(ctx, http.MethodGet, c.NPMURL+"/"+name+"/"+version, nil, &resp); err != nil {
			return Info{}, err
		}
		// npm sends the message as a string; anything else means "not deprecated".
		if msg, ok := resp.Deprecated.(string); ok {
			info.Deprecated = msg
		}
	case "python":
		var resp struct {
			Info struct {
				Yanked bool `json:"yanked"`
			} `json:"info"`
		}
		if _, err := c.do(ctx, http.MethodGet, c.PyPIURL+"/"+name+"/"+version+"/json", nil, &resp); err != nil {
			return Info{}, err
		}
		info.Yanked = resp.Info.Yanked
	case "cargo":
		var resp struct {
			Version struct {
				Yanked bool `json:"yanked"`
			} `json:"version"`
		}
		if _, err := c.do(ctx, http.MethodGet, c.CratesURL+"/"+name+"/"+version, nil, &resp); err != nil {
			return Info{}, err
		}
		info.Yanked = resp.Version.Yanked
	}
	return info, nil
}

// registryName is the label used for an ecosystem's registry in warnings.
var registryName = map[string]string{"npm": "npm registry", "python": "PyPI", "cargo": "crates.io"}
