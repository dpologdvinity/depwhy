package health

import (
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"testing"
	"time"
)

// TestCacheRoundTripAndTTL checks that a stored entry is returned until its
// 24-hour lifetime ends.
func TestCacheRoundTripAndTTL(t *testing.T) {
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	c := &Cache{Dir: t.TempDir(), TTL: 24 * time.Hour, Now: func() time.Time { return now }}
	p := Pkg{"npm", "@x/y", "1.0.0"}
	want := Info{Advisories: []string{"GHSA-1"}, Deprecated: "old", Yanked: true}
	c.Put(p, want)

	if got, ok := c.Get(p); !ok || !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v, %v", got, ok)
	}
	now = now.Add(24*time.Hour - time.Second)
	if _, ok := c.Get(p); !ok {
		t.Fatal("entry should still be valid just before the TTL")
	}
	now = now.Add(2 * time.Second)
	if _, ok := c.Get(p); ok {
		t.Fatal("entry should have expired")
	}
	if _, ok := c.Get(Pkg{"npm", "other", "1"}); ok {
		t.Fatal("unknown key must miss")
	}
}

// TestCacheFileNameIsSafe checks that scoped names and odd characters cannot
// escape the cache directory or break on Windows.
func TestCacheFileNameIsSafe(t *testing.T) {
	dir := t.TempDir()
	c := &Cache{Dir: dir, TTL: time.Hour, Now: time.Now}
	c.Put(Pkg{"npm", "../../etc/passwd:*?", "1/2"}, Info{})
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 {
		t.Fatalf("entries=%v err=%v", entries, err)
	}
	if name := entries[0].Name(); !regexp.MustCompile(`^[0-9a-f]{64}\.json$`).MatchString(name) {
		t.Fatalf("unsafe file name %q", name)
	}
}

// TestCacheCorruptEntryIsAMiss checks that unreadable data is ignored, not fatal.
func TestCacheCorruptEntryIsAMiss(t *testing.T) {
	dir := t.TempDir()
	c := &Cache{Dir: dir, TTL: time.Hour, Now: time.Now}
	p := Pkg{"cargo", "serde", "1.0.0"}
	c.Put(p, Info{Yanked: true})
	files, _ := filepath.Glob(filepath.Join(dir, "*.json"))
	if len(files) != 1 {
		t.Fatalf("files = %v", files)
	}
	if err := os.WriteFile(files[0], []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Get(p); ok {
		t.Fatal("corrupt entry must be a miss")
	}
}

// TestNilCacheIsANoop checks that a nil *Cache disables caching safely.
func TestNilCacheIsANoop(t *testing.T) {
	var c *Cache
	c.Put(Pkg{"npm", "a", "1"}, Info{})
	if _, ok := c.Get(Pkg{"npm", "a", "1"}); ok {
		t.Fatal("nil cache must miss")
	}
}

// TestPutToUnwritableDirIsIgnored checks that cache write failures never panic.
func TestPutToUnwritableDirIsIgnored(t *testing.T) {
	file := filepath.Join(t.TempDir(), "afile")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	c := &Cache{Dir: filepath.Join(file, "sub"), TTL: time.Hour, Now: time.Now}
	c.Put(Pkg{"npm", "a", "1"}, Info{})
}

// TestDefaultCacheDirPrefersXDG checks the XDG_CACHE_HOME rule from the spec.
func TestDefaultCacheDirPrefersXDG(t *testing.T) {
	t.Setenv("XDG_CACHE_HOME", "/custom/cache")
	if got := DefaultCacheDir(); filepath.ToSlash(got) != "/custom/cache/depwhy" {
		t.Fatalf("got %q", got)
	}
}

// TestCacheIgnoresFutureEntries checks that an entry stamped in the future (clock
// skew or a hand-edited file) is a miss rather than being trusted indefinitely.
func TestCacheIgnoresFutureEntries(t *testing.T) {
	now := time.Date(2026, 10, 24, 12, 0, 0, 0, time.UTC)
	c := &Cache{Dir: t.TempDir(), TTL: time.Hour, Now: func() time.Time { return now.Add(48 * time.Hour) }}
	p := Pkg{"npm", "a", "1"}
	c.Put(p, Info{})
	c.Now = func() time.Time { return now }
	if _, ok := c.Get(p); ok {
		t.Fatal("an entry from the future must be a miss")
	}
}

// TestCacheKeysDoNotCollide checks that moving the "@" between name and version
// gives a different entry.
func TestCacheKeysDoNotCollide(t *testing.T) {
	c := &Cache{Dir: t.TempDir(), TTL: time.Hour, Now: time.Now}
	c.Put(Pkg{"npm", "a@b", "c"}, Info{Yanked: true})
	if _, ok := c.Get(Pkg{"npm", "a", "b@c"}); ok {
		t.Fatal("different packages must not share an entry")
	}
	if _, ok := c.Get(Pkg{"npm", "a@b", "c"}); !ok {
		t.Fatal("the stored package must still hit")
	}
}
