//go:build integration

package resolver

import (
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// Every pipeline run of the project policy tests parses the stubs in tools/testdata again (twice
// for runOnboardingShards: once here, once in runPipelineTransform), and the stress runs parse
// them once more. The files do not change during a run, so each file is parsed once per content
// and every caller gets its own deep copy: a resolver may keep and change the objects it is seeded
// with. A file is keyed by path, size and modification time, so the copies tests write into
// t.TempDir(), and a stub a test edits or removes there, are never served stale.
//
// The same holds for the rendered Policies the hub pass reads: ResolvePolicy parses each input
// document, and the render cache hands out the same text every time, so the parse is memoized by
// document text (policyDocMemo, resolver.go). Every shard Policy of a run also reads the same
// rendered config through the hub fromYaml, so that parse is memoized too (hubFromYAML), with a
// deep copy per call because a template may change what it gets. These caches change only how
// often the same input is parsed, never what a test resolves.
//
// The suite allocates heavily and keeps little live, so the collector runs at GOGC=300 unless GOGC
// is set: about 13% less CPU on the 4-CPU CI budget.

type stubFileKey struct {
	path      string
	size, mod int64
}

var stubFileCache sync.Map // stubFileKey -> []unstructured.Unstructured, never handed out

func init() {
	loadTestResources = cachedLoadTestResources
	policyDocMemo = &sync.Map{}
	hubFromYAML = memoFromYAML
	if os.Getenv("GOGC") == "" {
		debug.SetGCPercent(300)
	}
}

// fromYAMLMemo keeps the most recent fromYAMLMemoMax parses: the reuse is within a run, so the
// cache stays small however many configs the scale tests generate.
const fromYAMLMemoMax = 64

type fromYAMLResult struct {
	v   any
	err error
}

var (
	fromYAMLMu    sync.Mutex
	fromYAMLMemo  = map[string]fromYAMLResult{}
	fromYAMLOrder []string
)

// memoFromYAML is the library's fromYAML (yaml.v3 Unmarshal into any, the partial value kept on
// error) with the parse shared and the result deep-copied.
func memoFromYAML(s string) (any, error) {
	fromYAMLMu.Lock()
	r, ok := fromYAMLMemo[s]
	fromYAMLMu.Unlock()
	if !ok {
		var m any
		err := yaml.Unmarshal([]byte(s), &m)
		r = fromYAMLResult{v: m, err: err}
		fromYAMLMu.Lock()
		if _, dup := fromYAMLMemo[s]; !dup {
			fromYAMLMemo[s] = r
			fromYAMLOrder = append(fromYAMLOrder, s)
			if len(fromYAMLOrder) > fromYAMLMemoMax {
				delete(fromYAMLMemo, fromYAMLOrder[0])
				fromYAMLOrder = fromYAMLOrder[1:]
			}
		}
		fromYAMLMu.Unlock()
	}
	return deepCopyYAMLValue(r.v), r.err
}

// deepCopyYAMLValue copies the maps and slices yaml.v3 decodes into any; every other value it
// produces (strings, numbers, booleans, nil, times) is immutable.
func deepCopyYAMLValue(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, e := range x {
			out[k] = deepCopyYAMLValue(e)
		}
		return out
	case map[any]any:
		out := make(map[any]any, len(x))
		for k, e := range x {
			out[k] = deepCopyYAMLValue(e)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = deepCopyYAMLValue(e)
		}
		return out
	default:
		return v
	}
}

// cachedStubFile returns the objects of one stub file, as LoadTestResources and
// stressParseStubsLenient parse it: malformed and empty documents are skipped.
func cachedStubFile(path string) ([]unstructured.Unstructured, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	key := stubFileKey{path: path, size: info.Size(), mod: info.ModTime().UnixNano()}
	v, ok := stubFileCache.Load(key)
	if !ok {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		v, _ = stubFileCache.LoadOrStore(key, stressParseStubsLenient(string(data)))
	}
	cached := v.([]unstructured.Unstructured)
	out := make([]unstructured.Unstructured, len(cached))
	for i := range cached {
		out[i] = *cached[i].DeepCopy()
	}
	return out, nil
}

// cachedLoadTestResources is LoadTestResources through the per-file cache: the same files in the
// same order, the same documents skipped.
func cachedLoadTestResources(testdataDir string) ([]unstructured.Unstructured, error) {
	if testdataDir == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(testdataDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []unstructured.Unstructured
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".yaml") {
			continue
		}
		objs, err := cachedStubFile(filepath.Join(testdataDir, e.Name()))
		if err != nil {
			return nil, err
		}
		out = append(out, objs...)
	}
	return out, nil
}
