//go:build cgo && purego && arm64 && supervised_integration && (darwin || linux)

package processor

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ailiheizi/restoreweave/server/internal/processor/sandbox"
	"github.com/ailiheizi/restoreweave/server/internal/search"
)

const (
	semanticQualificationReportPath = "RESTOREWEAVE_SEMANTIC_QUALIFICATION_REPORT"
	semanticQualificationConfig     = "sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
)

type semanticQualificationCase struct {
	ID       string
	Language string
	Document string
	Query    string
}

type semanticQualificationReport struct {
	Schema                    string    `json:"schema"`
	GeneratedAt               time.Time `json:"generated_at"`
	Status                    string    `json:"status"`
	OS                        string    `json:"os"`
	Arch                      string    `json:"arch"`
	GoVersion                 string    `json:"go_version"`
	ProfileID                 string    `json:"profile_id"`
	ProfileDigest             string    `json:"profile_digest"`
	SemanticSpace             string    `json:"semantic_space"`
	Dimension                 int       `json:"dimension"`
	CorpusCases               int       `json:"corpus_cases"`
	BundleBytes               uint64    `json:"bundle_bytes"`
	WorkerStartupMS           float64   `json:"worker_startup_ms"`
	DocumentBatchMS           float64   `json:"document_batch_ms"`
	QueryP50MS                float64   `json:"query_p50_ms"`
	QueryP95MS                float64   `json:"query_p95_ms"`
	QueryRequests             int       `json:"query_requests"`
	QueryConcurrency          int       `json:"query_concurrency"`
	RecallAt1                 float64   `json:"recall_at_1"`
	RecallAt5                 float64   `json:"recall_at_5"`
	WorkerPeakSampledRSSBytes uint64    `json:"worker_peak_sampled_rss_bytes"`
	RSSSamplingIntervalMS     float64   `json:"rss_sampling_interval_ms"`
	RSSScope                  string    `json:"rss_scope"`
}

// TestSupervisedONNXSemanticQualification records bounded component evidence
// for the pinned default model. It is intentionally opt-in: a fixture vector
// or an absent local bundle must never be reported as release evidence.
func TestSupervisedONNXSemanticQualification(t *testing.T) {
	if strings.TrimSpace(os.Getenv("RESTOREWEAVE_RUN_SUPERVISED_ONNX")) != "1" {
		t.Skip("RESTOREWEAVE_RUN_SUPERVISED_ONNX=1 is required for semantic qualification")
	}
	bundleRoot := strings.TrimSpace(os.Getenv("RESTOREWEAVE_SEMANTIC_BUNDLE_ROOT"))
	if bundleRoot == "" {
		modelPath := strings.TrimSpace(os.Getenv("RESTOREWEAVE_BGE_ONNX_MODEL"))
		if modelPath != "" {
			bundleRoot = filepath.Dir(modelPath)
		}
	}
	if bundleRoot == "" {
		t.Skip("RESTOREWEAVE_SEMANTIC_BUNDLE_ROOT or RESTOREWEAVE_BGE_ONNX_MODEL is required")
	}
	bundleRoot, err := filepath.Abs(bundleRoot)
	if err != nil {
		t.Fatal(err)
	}
	bundle, err := search.LoadSemanticBundle(bundleRoot)
	if err != nil {
		t.Fatalf("load semantic bundle: %v", err)
	}
	if err := search.ValidateDefaultSemanticBundleAdmission(bundle); err != nil {
		t.Fatalf("default semantic bundle admission: %v", err)
	}
	if runtime.GOOS == "linux" && !sandbox.Supported() {
		t.Skip("Linux bubblewrap is unavailable for supervised qualification")
	}

	workerBinary := buildRestoreweavedForWorker(t)
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	started := time.Now()
	worker, closeWorker, err := StartONNXWorker(ctx, ONNXWorkerSupervisorOptions{
		Command: workerBinary, BundleRoot: bundleRoot,
		ConfigDigest: semanticQualificationConfig,
		GenerationID: "semantic-qualification-generation", FenceToken: 1,
		SandboxPolicyDigest: sandbox.PolicyDigest(),
		FenceValidator:      func(context.Context) error { return nil },
		HandshakeTimeout:    30 * time.Second,
	})
	if err != nil {
		t.Fatalf("start supervised worker: %v", err)
	}
	startup := time.Since(started)
	defer closeWorker()
	provider, err := NewONNXSemanticEmbeddingProvider(worker)
	if err != nil {
		t.Fatal(err)
	}
	manifest, err := bundle.EmbeddingGenerationManifest(semanticQualificationConfig)
	if err != nil {
		t.Fatal(err)
	}

	cases := []semanticQualificationCase{
		{ID: "flood-recovery", Language: "zh", Document: "城市遭遇暴雨洪水后，档案馆需要恢复受损的纸质目录和数字备份。", Query: "水灾之后怎样找回档案资料"},
		{ID: "bread", Language: "zh", Document: "家庭烘焙酸面包时，需要控制酵母发酵温度和面团含水量。", Query: "做面包如何控制发酵"},
		{ID: "underwater-camera", Language: "zh", Document: "潜水员拍摄珊瑚礁时会使用防水相机壳和水下补光灯。", Query: "海底摄影需要什么装备"},
		{ID: "worker-fencing", Language: "zh", Document: "分布式任务通过租约与 fencing token 防止过期 worker 提交结果。", Query: "如何阻止过期工作节点写入"},
		{ID: "record-catalog", Language: "zh", Document: "古典音乐黑胶唱片需要记录作曲家、指挥家和唱片版本。", Query: "整理古典唱片的编目信息"},
		{ID: "go-project", Language: "zh", Document: "Go 项目包含模块依赖、源代码和自动化测试。", Query: "代码仓库的模块和测试"},
		{ID: "offline-search", Language: "en", Document: "An offline semantic search service loads its model locally and never downloads assets on the first query.", Query: "local search without a network download"},
		{ID: "exact-dedup", Language: "en", Document: "Exact whole-file deduplication reuses storage only when SHA-256 and logical length both match.", Query: "when can identical files share physical storage"},
	}
	documentStarted := time.Now()
	var documents []search.SemanticVector
	for _, language := range []string{"zh", "en"} {
		var inputs []search.SemanticTextInput
		for _, item := range cases {
			if item.Language != language {
				continue
			}
			inputs = append(inputs, search.SemanticTextInput{
				SubjectID: "subject-" + item.ID, SegmentID: "segment-" + item.ID,
				DescriptionDocumentID: "description-" + item.ID, Ordinal: 0,
				Language: item.Language, Text: item.Document,
			})
		}
		batch, embedErr := provider.Embed(ctx, search.SemanticEmbeddingRequest{
			Purpose: search.SemanticEmbeddingDocument, GenerationID: "semantic-qualification-generation",
			Manifest: manifest, Inputs: inputs,
		})
		if embedErr != nil {
			t.Fatalf("embed %s qualification documents: %v", language, embedErr)
		}
		documents = append(documents, batch...)
	}
	documentDuration := time.Since(documentStarted)
	if len(documents) != len(cases) {
		t.Fatalf("document vectors = %d, want %d", len(documents), len(cases))
	}
	for _, document := range documents {
		if err := validateQualificationVector(document.Vector, manifest.Dimension); err != nil {
			t.Fatalf("document vector %s: %v", document.SegmentID, err)
		}
	}

	const (
		queryConcurrency  = 4
		queryRounds       = 4
		rssSampleInterval = 10 * time.Millisecond
	)
	type queryResult struct {
		caseID  string
		latency time.Duration
		top1    bool
		top5    bool
		err     error
	}
	jobs := make(chan semanticQualificationCase)
	results := make(chan queryResult, len(cases)*queryRounds)
	stopSampling := make(chan struct{})
	peakRSS := make(chan uint64, 1)
	go sampleQualificationPeakRSS(os.Getpid(), rssSampleInterval, stopSampling, peakRSS)

	var workers sync.WaitGroup
	for workerIndex := 0; workerIndex < queryConcurrency; workerIndex++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			for item := range jobs {
				queryStarted := time.Now()
				queries, embedErr := provider.Embed(ctx, search.SemanticEmbeddingRequest{
					Purpose: search.SemanticEmbeddingQuery, GenerationID: "semantic-qualification-generation",
					Manifest: manifest,
					Inputs:   []search.SemanticTextInput{{SegmentID: "query-" + item.ID, Language: item.Language, Text: item.Query}},
				})
				result := queryResult{caseID: item.ID, latency: time.Since(queryStarted)}
				if embedErr != nil || len(queries) != 1 {
					result.err = fmt.Errorf("vectors=%d err=%v", len(queries), embedErr)
					results <- result
					continue
				}
				if err := validateQualificationVector(queries[0].Vector, manifest.Dimension); err != nil {
					result.err = err
					results <- result
					continue
				}
				ranked := rankQualificationDocuments(documents, queries[0].Vector)
				want := "subject-" + item.ID
				result.top1 = len(ranked) > 0 && ranked[0] == want
				for index := 0; index < len(ranked) && index < 5; index++ {
					if ranked[index] == want {
						result.top5 = true
						break
					}
				}
				results <- result
			}
		}()
	}
	go func() {
		for round := 0; round < queryRounds; round++ {
			for _, item := range cases {
				jobs <- item
			}
		}
		close(jobs)
		workers.Wait()
		close(results)
	}()

	latencies := make([]time.Duration, 0, len(cases)*queryRounds)
	top1, top5, requests := 0, 0, 0
	var firstQueryErr error
	for result := range results {
		if result.err != nil {
			if firstQueryErr == nil {
				firstQueryErr = fmt.Errorf("embed qualification query %s: %w", result.caseID, result.err)
			}
			continue
		}
		requests++
		latencies = append(latencies, result.latency)
		if result.top1 {
			top1++
		}
		if result.top5 {
			top5++
		}
	}
	close(stopSampling)
	workerPeakRSS := <-peakRSS
	if firstQueryErr != nil {
		t.Fatal(firstQueryErr)
	}
	if requests != len(cases)*queryRounds {
		t.Fatalf("qualification query requests = %d, want %d", requests, len(cases)*queryRounds)
	}
	recall1 := float64(top1) / float64(requests)
	recall5 := float64(top5) / float64(requests)
	if recall1 < 0.75 || recall5 < 1 {
		t.Fatalf("semantic qualification recall@1=%.3f recall@5=%.3f", recall1, recall5)
	}

	report := semanticQualificationReport{
		Schema: "restoreweave.semantic-qualification.v1", GeneratedAt: time.Now().UTC(), Status: "PASSED",
		OS: runtime.GOOS, Arch: runtime.GOARCH, GoVersion: runtime.Version(),
		ProfileID: bundle.Descriptor.ProfileID, ProfileDigest: bundle.ProfileDigest,
		SemanticSpace: manifest.SemanticSpace, Dimension: manifest.Dimension,
		CorpusCases: len(cases), BundleBytes: semanticQualificationBundleBytes(bundle),
		WorkerStartupMS: durationMilliseconds(startup), DocumentBatchMS: durationMilliseconds(documentDuration),
		QueryP50MS:    durationMilliseconds(qualificationPercentile(latencies, 0.50)),
		QueryP95MS:    durationMilliseconds(qualificationPercentile(latencies, 0.95)),
		QueryRequests: requests, QueryConcurrency: queryConcurrency,
		RecallAt1: recall1, RecallAt5: recall5,
		WorkerPeakSampledRSSBytes: workerPeakRSS,
		RSSSamplingIntervalMS:     durationMilliseconds(rssSampleInterval),
		RSSScope:                  "sum of test-process descendant RSS sampled via ps during concurrent query load",
	}
	if path := strings.TrimSpace(os.Getenv(semanticQualificationReportPath)); path != "" {
		if err := writeSemanticQualificationReport(path, report); err != nil {
			t.Fatal(err)
		}
	}
	t.Logf("semantic qualification: %+v", report)
}

func validateQualificationVector(vector []float32, dimension int) error {
	if len(vector) != dimension {
		return fmt.Errorf("dimension = %d, want %d", len(vector), dimension)
	}
	for index, value := range vector {
		if math.IsNaN(float64(value)) || math.IsInf(float64(value), 0) {
			return fmt.Errorf("component %d is not finite", index)
		}
	}
	return nil
}

func sampleQualificationPeakRSS(rootPID int, interval time.Duration, stop <-chan struct{}, result chan<- uint64) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	peak := qualificationDescendantRSSBytes(rootPID)
	for {
		select {
		case <-ticker.C:
			if current := qualificationDescendantRSSBytes(rootPID); current > peak {
				peak = current
			}
		case <-stop:
			if current := qualificationDescendantRSSBytes(rootPID); current > peak {
				peak = current
			}
			result <- peak
			return
		}
	}
}

func rankQualificationDocuments(documents []search.SemanticVector, query []float32) []string {
	type scored struct {
		subject string
		score   float64
	}
	values := make([]scored, 0, len(documents))
	for _, document := range documents {
		var score float64
		for index, value := range document.Vector {
			if index < len(query) {
				score += float64(value) * float64(query[index])
			}
		}
		values = append(values, scored{subject: document.SubjectID, score: score})
	}
	sort.Slice(values, func(i, j int) bool {
		if values[i].score == values[j].score {
			return values[i].subject < values[j].subject
		}
		return values[i].score > values[j].score
	})
	result := make([]string, len(values))
	for index, value := range values {
		result[index] = value.subject
	}
	return result
}

func semanticQualificationBundleBytes(bundle search.SemanticBundleAdmission) uint64 {
	var total uint64
	for _, asset := range []search.SemanticBundleAsset{
		bundle.Descriptor.Runtime, bundle.Descriptor.ONNXBinding, bundle.Descriptor.ONNXCAPI,
		bundle.Descriptor.Model, bundle.Descriptor.Tokenizer, bundle.Descriptor.Profile,
		bundle.Descriptor.Zvec, bundle.Descriptor.ZvecGo, bundle.Descriptor.License,
		bundle.Descriptor.Notice, bundle.Descriptor.SBOM,
	} {
		total += asset.Size
	}
	return total
}

func qualificationPercentile(values []time.Duration, percentile float64) time.Duration {
	if len(values) == 0 {
		return 0
	}
	ordered := append([]time.Duration(nil), values...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })
	index := int(math.Ceil(percentile*float64(len(ordered)))) - 1
	if index < 0 {
		index = 0
	}
	if index >= len(ordered) {
		index = len(ordered) - 1
	}
	return ordered[index]
}

func durationMilliseconds(value time.Duration) float64 {
	return float64(value.Microseconds()) / 1000
}

func qualificationDescendantRSSBytes(rootPID int) uint64 {
	output, err := exec.Command("ps", "-axo", "pid=,ppid=,rss=").Output()
	if err != nil {
		return 0
	}
	type process struct {
		pid, parent int
		rss         uint64
	}
	var processes []process
	for _, line := range strings.Split(string(output), "\n") {
		fields := strings.Fields(line)
		if len(fields) != 3 {
			continue
		}
		pid, pidErr := strconv.Atoi(fields[0])
		parent, parentErr := strconv.Atoi(fields[1])
		rss, rssErr := strconv.ParseUint(fields[2], 10, 64)
		if pidErr == nil && parentErr == nil && rssErr == nil {
			processes = append(processes, process{pid: pid, parent: parent, rss: rss})
		}
	}
	known := map[int]struct{}{rootPID: {}}
	var totalKiB uint64
	for changed := true; changed; {
		changed = false
		for _, item := range processes {
			if _, ok := known[item.pid]; ok {
				continue
			}
			if _, ok := known[item.parent]; ok {
				known[item.pid] = struct{}{}
				totalKiB += item.rss
				changed = true
			}
		}
	}
	return totalKiB * 1024
}

func writeSemanticQualificationReport(path string, report semanticQualificationReport) error {
	path, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	payload, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	stage, err := os.CreateTemp(filepath.Dir(path), ".semantic-qualification-*.json")
	if err != nil {
		return err
	}
	stagePath := stage.Name()
	defer os.Remove(stagePath)
	if err := stage.Chmod(0o600); err != nil {
		stage.Close()
		return err
	}
	if _, err := stage.Write(payload); err != nil {
		stage.Close()
		return err
	}
	if err := stage.Sync(); err != nil {
		stage.Close()
		return err
	}
	if err := stage.Close(); err != nil {
		return err
	}
	if _, err := os.Lstat(path); err == nil {
		return fmt.Errorf("semantic qualification report already exists: %s", path)
	} else if !os.IsNotExist(err) {
		return err
	}
	return os.Rename(stagePath, path)
}
