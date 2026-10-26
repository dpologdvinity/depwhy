package health

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// lookable reports whether p can be asked about: it needs a name and a registry
// version. Versions such as "file:../x" or "git/abc" name no published release,
// and "." or ".." would turn into a path to another registry URL.
func lookable(p Pkg) bool {
	return p.Name != "" && p.Name != "." && p.Name != ".." &&
		p.Version != "" && !strings.ContainsAny(p.Version, ":/")
}

// Lookup returns what is known online about each package. Packages that cannot be
// looked up (see lookable) are left out of the result. Lookup never fails: when a
// service is unreachable the affected information is simply missing and a warning
// says so (SPEC §12.4). Only complete answers are cached.
func (c *Client) Lookup(ctx context.Context, pkgs []Pkg) (map[Pkg]Info, []string) {
	result := map[Pkg]Info{}
	var misses []Pkg
	seen := map[Pkg]bool{}
	for _, p := range pkgs {
		if !lookable(p) || seen[p] {
			continue
		}
		seen[p] = true
		if !c.Refresh {
			if info, ok := c.Cache.Get(p); ok {
				result[p] = info
				continue
			}
		}
		misses = append(misses, p)
	}
	if len(misses) == 0 {
		return result, nil
	}
	sort.Slice(misses, func(i, j int) bool { // stable order keeps requests and warnings reproducible
		a, b := misses[i], misses[j]
		if a.Ecosystem != b.Ecosystem {
			return a.Ecosystem < b.Ecosystem
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.Version < b.Version
	})

	var warnings []string
	advisories, osvErr := c.advisories(ctx, misses)
	if osvErr != nil {
		warnings = append(warnings, "online lookup failed: OSV advisories: "+osvErr.Error())
	}

	// Registry lookups run concurrently, at most Concurrency at a time.
	type outcome struct {
		info Info
		err  error
	}
	outcomes := make([]outcome, len(misses))
	limit := c.Concurrency
	if limit <= 0 {
		limit = maxConcurrent
	}
	sem := make(chan struct{}, limit)
	var wg sync.WaitGroup
	for i, p := range misses {
		wg.Add(1)
		sem <- struct{}{}
		go func(i int, p Pkg) {
			defer wg.Done()
			defer func() { <-sem }()
			info, err := c.status(ctx, p)
			outcomes[i] = outcome{info, err}
		}(i, p)
	}
	wg.Wait()

	failures := map[string]int{}     // registry label -> failed packages
	firstError := map[string]error{} // registry label -> first error, in sorted package order
	for i, p := range misses {
		o := outcomes[i]
		info := o.info
		if o.err != nil {
			label := registryName[p.Ecosystem]
			failures[label]++
			if firstError[label] == nil {
				firstError[label] = o.err
			}
		}
		if osvErr == nil {
			info.Advisories = advisories[p]
		}
		result[p] = info
		if osvErr == nil && o.err == nil {
			c.Cache.Put(p, info)
		}
	}
	labels := make([]string, 0, len(failures))
	for l := range failures {
		labels = append(labels, l)
	}
	sort.Strings(labels)
	for _, l := range labels {
		warnings = append(warnings, fmt.Sprintf("online lookup failed: %s (%s): %v", l, plural(failures[l]), firstError[l]))
	}
	return result, warnings
}

// plural formats a package count: "1 package" or "3 packages".
func plural(n int) string {
	if n == 1 {
		return "1 package"
	}
	return fmt.Sprintf("%d packages", n)
}
