package trigram

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"runtime/pprof"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cruisinme30/spelunk/daemon/internal/protocol"
	"github.com/cruisinme30/spelunk/daemon/internal/query"
)

// corpusQuery is one reproducible query and the stratum it samples.
type corpusQuery struct {
	Family string `json:"family"`
	Text   string `json:"text"`
}

// TestRandomCorpusSearch is an opt-in seeded, stratified synthetic workload.
// Generate SPELUNK_QUERY_FILE once, then reuse the file and corpus revision.
// After a first session pass, SPELUNK_BENCH_RUNS whole workload rounds run in
// the same order without resetting lexical caches (default 20). "First" is
// not an OS/CPU-cold measurement. File order avoids time-dependent best-match
// ordering. Digests are computed after timing; complete runs must agree.
// SPELUNK_BENCH_SELECT selects query text; SPELUNK_BENCH_PROFILE is diagnostic
// only and must not be enabled when collecting reported latency samples.
func TestRandomCorpusSearch(t *testing.T) {
	root := os.Getenv("SPELUNK_BENCH_ROOT")
	if root == "" {
		t.Skip("set SPELUNK_BENCH_ROOT to run the seeded corpus benchmark")
	}
	ctx := t.Context()
	started := time.Now()
	files, err := ListFiles(ctx, root, WalkOptions{MaxFileBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	shard, err := Build(ctx, root, files, BuildOptions{Symbols: true, MaxFileBytes: 1 << 20})
	if err != nil {
		t.Fatal(err)
	}
	buildMS := float64(time.Since(started).Microseconds()) / 1000
	var sourceBytes int
	manifest := sha256.New()
	paths := make([]string, 0, len(shard.Docs))
	for i := range shard.Docs {
		doc := &shard.Docs[i]
		paths = append(paths, doc.Path)
		sourceBytes += len(doc.Content)
		_, _ = fmt.Fprintf(manifest, "%d:%s%d:", len(doc.Path), doc.Path, len(doc.Content))
		_, _ = manifest.Write(doc.Content)
	}
	if path := os.Getenv("SPELUNK_MANIFEST_FILE"); path != "" {
		raw, marshalErr := json.Marshal(map[string]any{"root": root, "files": paths, "content_manifest_sha256": hex.EncodeToString(manifest.Sum(nil))})
		if marshalErr != nil {
			t.Fatal(marshalErr)
		}
		if writeErr := os.WriteFile(path, raw, 0o600); writeErr != nil {
			t.Fatal(writeErr)
		}
	}
	revision, err := exec.CommandContext(ctx, "git", "-C", root, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	writeMeasurement(t, map[string]any{"event": "build", "files": len(shard.Docs), "source_bytes": sourceBytes,
		"duration_ms": buildMS, "memory": corpusMemory(), "root": root, "workers": runtime.GOMAXPROCS(0),
		"git_revision": strings.TrimSpace(string(revision)), "content_manifest_sha256": hex.EncodeToString(manifest.Sum(nil)),
		"go_version": runtime.Version(), "gomemlimit": os.Getenv("GOMEMLIMIT")})
	queries := loadCorpusQueries(t, shard)
	runs := corpusRuns(t)
	if pattern := os.Getenv("SPELUNK_BENCH_SELECT"); pattern != "" {
		selected, compileErr := regexp.Compile(pattern)
		if compileErr != nil {
			t.Fatal(compileErr)
		}
		queries = slices.DeleteFunc(queries, func(q corpusQuery) bool { return !selected.MatchString(q.Text) })
	}
	if len(queries) == 0 {
		t.Fatal("the workload contains no selected queries")
	}
	raw, err := json.Marshal(queries)
	if err != nil {
		t.Fatal(err)
	}
	writeMeasurement(t, map[string]any{"event": "workload", "queries": queries, "sha256": corpusSHA(raw),
		"runs": runs, "mode": "mixed-session", "order": "file", "time_limit_ms": 2000, "page_size": 500,
		"profiled": os.Getenv("SPELUNK_BENCH_PROFILE") != ""})
	plans := make([]*query.Plan, len(queries))
	samples := make([][]queryTiming, len(queries))
	for i, q := range queries {
		plans[i] = mustPlan(t, q.Text, protocol.Settings{DefaultCount: 500, Order: protocol.ResultOrderFile}, "")
	}
	stopProfile := startCorpusProfile(t)
	repos := []Repo{{ID: "r", Name: "r", Root: root, Shard: shard}}
	for round := 0; round <= runs; round++ {
		for i, q := range queries {
			if round == 0 {
				writeMeasurement(t, map[string]any{"event": "query_start", "query": q.Text, "round": round})
			}
			measured := timeCorpusQuery(ctx, t, plans[i], repos)
			writeMeasurement(t, map[string]any{"event": "sample", "query": q.Text, "round": round, "sample": measured})
			if len(samples[i]) > 0 {
				checkCorpusStable(t, q, samples[i][0], measured)
			}
			samples[i] = append(samples[i], measured)
		}
		writeMeasurement(t, map[string]any{"event": "round", "round": round, "memory": corpusMemory(),
			"cache_charged_bytes": shard.places.used, "cache_entries": len(shard.places.values)})
	}
	stopProfile()
	runtime.GC()
	for i, q := range queries {
		reportCorpusQuery(t, q, samples[i])
	}
	writeMeasurement(t, map[string]any{"event": "retained", "memory": corpusMemory(),
		"cache_charged_bytes": shard.places.used, "cache_entries": len(shard.places.values)})
	runtime.KeepAlive(shard)
}

func corpusRuns(t *testing.T) int {
	t.Helper()
	value := os.Getenv("SPELUNK_BENCH_RUNS")
	if value == "" {
		return 20
	}
	runs, err := strconv.Atoi(value)
	if err != nil || runs < 1 || runs > 1000 {
		t.Fatal("SPELUNK_BENCH_RUNS must be between 1 and 1000")
	}
	return runs
}

func loadCorpusQueries(t *testing.T, shard *Shard) []corpusQuery {
	t.Helper()
	path := os.Getenv("SPELUNK_QUERY_FILE")
	queries := readCorpusQueries(t, path)
	if queries == nil {
		queries = sampleCorpusQueries(shard, 20261110)
		writeCorpusQueries(t, path, queries)
	}
	if err := validateCorpusQueries(queries); err != nil {
		t.Fatal(err)
	}
	return queries
}

func readCorpusQueries(t *testing.T, path string) []corpusQuery {
	t.Helper()
	if path == "" {
		return nil
	}
	raw, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var queries []corpusQuery
	if err = json.Unmarshal(raw, &queries); err != nil {
		t.Fatal(err)
	}
	// An existing null/empty file is invalid, not a request to regenerate it.
	if err = validateCorpusQueries(queries); err != nil {
		t.Fatal(err)
	}
	return queries
}

func validateCorpusQueries(queries []corpusQuery) error {
	if len(queries) == 0 {
		return errors.New("empty query workload")
	}
	seen := map[string]bool{}
	for _, q := range queries {
		if strings.TrimSpace(q.Family) == "" || strings.TrimSpace(q.Text) == "" {
			return errors.New("empty query family or text")
		}
		if seen[q.Text] {
			return fmt.Errorf("duplicate query %q", q.Text)
		}
		seen[q.Text] = true
	}
	return nil
}

// sampleCorpusQueries samples identifiers from random documents and frequency
// strata, then constructs eight families with six queries apiece. It never
// chooses a query because a candidate implementation happens to be fast on it.
func sampleCorpusQueries(shard *Shard, seed int64) []corpusQuery {
	rng := rand.New(rand.NewSource(seed))
	identifier := regexp.MustCompile(`[A-Za-z_][A-Za-z_0-9]{3,39}`)
	frequencies := map[string]int{}
	order := rng.Perm(len(shard.Docs))
	for _, at := range order[:min(500, len(order))] {
		seen := map[string]bool{}
		for _, word := range identifier.FindAll(shard.Docs[at].Content, 1500) {
			seen[string(word)] = true
		}
		for word := range seen {
			frequencies[word]++
		}
	}
	words := make([]string, 0, len(frequencies))
	for word := range frequencies {
		words = append(words, word)
	}
	slices.SortFunc(words, func(a, b string) int {
		if frequencies[a] != frequencies[b] {
			return frequencies[b] - frequencies[a]
		}
		return strings.Compare(a, b)
	})
	if len(words) == 0 || len(order) == 0 {
		return nil
	}
	var out []corpusQuery
	add := func(family, text string) { out = append(out, corpusQuery{Family: family, Text: text}) }
	for i := range 6 {
		// Two common, two medium, and two rare sampled identifiers.
		start := i / 2 * len(words) / 3
		end := min(start+max(1, len(words)/3), len(words))
		if i < 2 {
			end = min(25, len(words))
		}
		word := words[start+rng.Intn(end-start)]
		other := words[rng.Intn(len(words))]
		doc := shard.Docs[order[i%len(order)]]
		add("literal", "content:"+word)
		middle := len(word) / 2
		add("regex", "/"+regexp.QuoteMeta(word[:middle])+".*"+regexp.QuoteMeta(word[middle:])+"/")
		add("path", "type:file f:/^"+strings.ReplaceAll(regexp.QuoteMeta(doc.Path), "/", `\/`)+"$/")
		op := []string{" OR ", " ", " NOT "}[i%3]
		add("boolean", "content:"+word+op+"content:"+other)
		place := []string{"code", "comments", "strings"}[i%3]
		add("context", "content:"+word+" in:"+place)
		filter := "word:yes "
		if i%2 == 0 {
			filter = "case:yes "
		}
		add("filter", filter+"content:"+word)
		symbol := word
		if len(doc.Symbols) > 0 {
			symbol = doc.Symbols[rng.Intn(len(doc.Symbols))].Name
		}
		add("definition", "sym:"+strconv.Quote(symbol))
	}
	for _, text := range []string{"content:spelunk_random_absent_482975019623", "type:code /[a-z]+/", "type:code /a/", "type:code /[0-9]+/", "type:code /[A-Z_]{8,}/", "type:file f:\"does_not_exist_482975019623\""} {
		add("stress", text)
	}
	return out
}

type queryTiming struct {
	TotalMS           float64  `json:"total_ms"`
	FirstMS           *float64 `json:"first_ms"`
	Count             int      `json:"count"`
	Emitted           int      `json:"emitted"`
	Cut               bool     `json:"truncated"`
	Completion        string   `json:"completion"`
	ResultsSHA        string   `json:"ordered_results_sha256"`
	LineIdentitiesSHA string   `json:"line_identities_sha256"`
	LineEmitted       int      `json:"line_emitted"`
	StatsSHA          string   `json:"stats_sha256"`
}

func timeCorpusQuery(ctx context.Context, t *testing.T, plan *query.Plan, repos []Repo) queryTiming {
	t.Helper()
	// Capacity is fixed before the timer. Record collection is identical for
	// before/after runs; JSON serialization and hashing happen after it stops.
	items := make([]protocol.ResultItem, 0, plan.Limit)
	result := queryTiming{}
	started := time.Now()
	stats, err := Search(ctx, plan, repos, 1, func(item protocol.ResultItem) {
		if len(items) == 0 {
			first := float64(time.Since(started).Microseconds()) / 1000
			result.FirstMS = &first
		}
		items = append(items, item)
	})
	result.TotalMS = float64(time.Since(started).Microseconds()) / 1000
	if err != nil {
		t.Fatal(err)
	}
	result.Count, result.Emitted, result.Cut = stats.Total, len(items), stats.Truncated
	result.Completion = "complete"
	if stats.Truncated {
		result.Completion = "deadline_or_incomplete"
		if stats.Total == query.MaxResults {
			result.Completion = "result_cap"
		}
	}
	result.ResultsSHA = corpusDigest(t, items)
	result.LineIdentitiesSHA, result.LineEmitted = corpusLineIdentities(items)
	result.StatsSHA = corpusDigest(t, stats)
	return result
}

func corpusDigest(t *testing.T, value any) string {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	return corpusSHA(raw)
}

func checkCorpusStable(t *testing.T, q corpusQuery, first, next queryTiming) {
	t.Helper()
	// Deadline-limited searches can visit different prefixes, and hidden counts
	// can also exhaust the deadline. Those samples remain explicitly incomplete.
	if first.Cut || next.Cut {
		return
	}
	if first.Count != next.Count || first.Emitted != next.Emitted || first.ResultsSHA != next.ResultsSHA {
		t.Fatalf("complete result set changed for %q: first=%+v next=%+v", q.Text, first, next)
	}
}

func reportCorpusQuery(t *testing.T, q corpusQuery, samples []queryTiming) {
	t.Helper()
	totals := make([]float64, len(samples)-1)
	complete, stable := true, true
	for i, sample := range samples {
		complete = complete && !sample.Cut
		stable = stable && sample.Count == samples[0].Count && sample.ResultsSHA == samples[0].ResultsSHA && sample.StatsSHA == samples[0].StatsSHA
		if i > 0 {
			totals[i-1] = sample.TotalMS
		}
	}
	slices.Sort(totals)
	median := (totals[(len(totals)-1)/2] + totals[len(totals)/2]) / 2
	writeMeasurement(t, map[string]any{"event": "query", "family": q.Family, "query": q.Text,
		"first_session_pass": samples[0], "warm": samples[1:], "runs": len(totals), "p50_ms": median,
		"p95_ms": totals[(95*len(totals)+99)/100-1], "all_complete": complete, "all_results_and_stats_stable": stable})
}

func TestValidateCorpusQueries(t *testing.T) {
	for _, queries := range [][]corpusQuery{nil, {{Family: "literal"}}, {{Text: "x"}}, {{Family: "a", Text: "x"}, {Family: "b", Text: "x"}}} {
		if validateCorpusQueries(queries) == nil {
			t.Errorf("accepted invalid workload %#v", queries)
		}
	}
	if err := validateCorpusQueries([]corpusQuery{{Family: "literal", Text: "content:x"}}); err != nil {
		t.Fatal(err)
	}
}

func corpusMemory() map[string]uint64 {
	var memory runtime.MemStats
	runtime.ReadMemStats(&memory)
	out := map[string]uint64{"heap_bytes": memory.HeapAlloc, "heap_sys_bytes": memory.HeapSys}
	if raw, err := os.ReadFile("/proc/self/status"); err == nil {
		for _, line := range strings.Split(string(raw), "\n") {
			fields := strings.Fields(line)
			if len(fields) < 2 || fields[0] != "VmRSS:" && fields[0] != "VmHWM:" {
				continue
			}
			value, err := strconv.ParseUint(fields[1], 10, 64)
			if err == nil {
				out[fields[0]] = value * 1024
			}
		}
	}
	return out
}

func writeMeasurement(t *testing.T, record any) {
	t.Helper()
	raw, err := json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	t.Log(string(raw))
}

func startCorpusProfile(t *testing.T) func() {
	t.Helper()
	path := os.Getenv("SPELUNK_BENCH_PROFILE")
	if path == "" {
		return func() {}
	}
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := pprof.StartCPUProfile(file); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	return func() {
		pprof.StopCPUProfile()
		if err := file.Close(); err != nil {
			t.Error(err)
		}
	}
}

func corpusSHA(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}

func writeCorpusQueries(t *testing.T, path string, queries []corpusQuery) {
	t.Helper()
	if path == "" {
		return
	}
	raw, err := json.MarshalIndent(queries, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, append(raw, '\n'), 0o600); err != nil {
		t.Fatal(err)
	}
}

// corpusLineIdentities is the tool-neutral first-page comparison used by the
// separate rg/git-grep reference workload. It intentionally keeps duplicates.
func corpusLineIdentities(items []protocol.ResultItem) (string, int) {
	lines := make([]protocol.ResultItem, 0, len(items))
	for _, item := range items {
		if item.Kind == query.KindLine {
			lines = append(lines, item)
		}
	}
	slices.SortFunc(lines, func(a, b protocol.ResultItem) int {
		if order := strings.Compare(a.Path, b.Path); order != 0 {
			return order
		}
		return a.Line - b.Line
	})
	digest := sha256.New()
	for _, item := range lines {
		_, _ = fmt.Fprintf(digest, "%s\x00%d\n", item.Path, item.Line)
	}
	return hex.EncodeToString(digest.Sum(nil)), len(lines)
}
