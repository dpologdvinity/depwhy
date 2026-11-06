package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// TestJSONSchemaKeys guards docs/json-schema.md, the frozen schema-1 contract.
// It collects every key path that the JSON goldens and a few extra runs (not
// found, --online in each mode) produce, and compares the set with
// testdata/json-keys.txt. A new, removed or renamed key fails here until the list
// is rewritten with -update, which should go with a matching edit to
// docs/json-schema.md (and, for anything but an added key, a schema bump).
func TestJSONSchemaKeys(t *testing.T) {
	keys := map[string]bool{}
	add := func(doc string) {
		t.Helper()
		var v map[string]any
		if err := json.Unmarshal([]byte(doc), &v); err != nil {
			t.Fatalf("invalid JSON: %v\n%s", err, doc)
		}
		mode, _ := v["mode"].(string)
		if v["across"] == true {
			mode += "+across"
		}
		collectKeys(mode, v, keys)
	}

	goldens, err := filepath.Glob(filepath.Join("..", "..", "testdata", "golden", "*.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for _, g := range goldens {
		data, err := os.ReadFile(g)
		if err != nil {
			t.Fatal(err)
		}
		if out, _, _ := strings.Cut(string(data), "\n--- stderr"); strings.HasPrefix(out, "{") {
			add(out)
		}
	}

	useFakeOnline(t)
	mono := stageFixture(t, project("monorepo"))
	for _, args := range [][]string{
		{"-C", project("npm"), "--json", "zzz"},
		{"-C", project("npm"), "--json", "--remove", "zzz"},
		{"-C", project("npm"), "--json", "--online", "--remove", "a"},
		{"-C", project("npm"), "--json", "--remove", "d"}, // not direct: required_by
		{"-C", project("npm"), "--json", "--online", "--dupes"},
		{"-C", mono, "-r", "--json", "--online", "--dupes", "--across"},
	} {
		_, out, errs := run(args...)
		if !strings.HasPrefix(out, "{") {
			t.Fatalf("%v printed no JSON: %s", args, errs)
		}
		add(out)
	}

	var got []string
	for k := range keys {
		got = append(got, k)
	}
	sort.Strings(got)
	checkKeyList(t, strings.Join(got, "\n")+"\n")
}

// collectKeys adds the dotted key path of every object key below v, prefixed with
// the mode. Array elements share the path "[]", so the set describes the shape,
// not the data.
func collectKeys(path string, v any, keys map[string]bool) {
	switch v := v.(type) {
	case map[string]any:
		for k, child := range v {
			p := path + "." + k
			keys[p] = true
			collectKeys(p, child, keys)
		}
	case []any:
		for _, child := range v {
			collectKeys(path+"[]", child, keys)
		}
	}
}

// checkKeyList compares got with testdata/json-keys.txt, or rewrites it with -update.
func checkKeyList(t *testing.T, got string) {
	t.Helper()
	file := filepath.Join("..", "..", "testdata", "json-keys.txt")
	if *update {
		if err := os.WriteFile(file, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("missing key list (run with -update): %v", err)
	}
	if got != string(want) {
		t.Errorf("JSON key paths changed; update docs/json-schema.md, then run go test ./internal/cli -update\n--- got\n%s--- want\n%s", got, want)
	}
}
