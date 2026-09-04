//go:build savingsreport

// Command savings-report-runner is the Go side of scripts/savings-report.sh.
// It places a deterministic corpus into a fresh in-tree repository profile and
// prints the mechanism-separated MeasureSavings report for that repository.
// It is guarded by the "savingsreport" build tag so it never participates in
// the normal repository package build. This is measurement infrastructure
// only: no engine name becomes normative, and local-zstd-v1 remains a
// candidate pending encryption/chunking/GC/repair/corpus qualification gates.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ailiheizi/restoreweave/server/internal/repository"
	"github.com/ailiheizi/restoreweave/server/internal/repository/qualificationcorpus"
)

func main() {
	profile := flag.String("profile", "", "repository profile: raw or zstd")
	repoPath := flag.String("repo", "", "repository path to create")
	corpus := flag.String("corpus", "", "existing corpus directory to read")
	manifestOut := flag.String("manifest-out", "", "optional canonical corpus manifest JSON path")
	manifestIn := flag.String("manifest-in", "", "operator-supplied corpus manifest JSON to verify")
	evidenceOut := flag.String("evidence-out", "", "optional candidate evidence JSON path")
	manifestOnly := flag.Bool("manifest-only", false, "hash the corpus and write its manifest without opening a repository")
	generateCorpus := flag.String("generate-corpus", "", "generate the internal deterministic qualification corpus at this new path")
	generateSizeMiB := flag.Int("generate-size-mib", 0, "generated corpus opaque payload size in MiB (0 uses the small fixture)")
	generateSeed := flag.String("generate-seed", qualificationcorpus.DefaultSeed, "deterministic generated corpus seed")
	catalogPath := flag.String("catalog-path", "", "optional catalog file or directory to measure read-only")
	indexPath := flag.String("index-path", "", "optional search index file or directory to measure read-only")
	modelPath := flag.String("model-path", "", "optional model file or directory to measure read-only")
	temporaryPeakBytes := flag.Int64("temporary-peak-bytes", -1, "caller-supplied temporary peak bytes (-1 means UNMEASURED)")
	flag.Parse()
	if *generateCorpus != "" {
		if *corpus != "" || *profile != "" || *repoPath != "" || *manifestOnly || *manifestIn != "" || *evidenceOut != "" ||
			*catalogPath != "" || *indexPath != "" || *modelPath != "" || *temporaryPeakBytes != -1 {
			fmt.Fprintln(os.Stderr, "-generate-corpus cannot be combined with repository or manifest flags")
			os.Exit(2)
		}
		if *manifestOut == "" {
			fmt.Fprintln(os.Stderr, "-manifest-out is required with -generate-corpus")
			os.Exit(2)
		}
		if err := validateOutputPath(*generateCorpus, *manifestOut); err != nil {
			fmt.Fprintf(os.Stderr, "unsafe generated manifest output: %v\n", err)
			os.Exit(1)
		}
		manifest, err := qualificationcorpus.GenerateWithOptions(*generateCorpus, qualificationcorpus.Options{Seed: *generateSeed, SizeMiB: *generateSizeMiB})
		if err != nil {
			fmt.Fprintf(os.Stderr, "generate corpus: %v\n", err)
			os.Exit(1)
		}
		if err := qualificationcorpus.WriteManifest(*manifestOut, manifest); err != nil {
			fmt.Fprintf(os.Stderr, "write generated corpus manifest: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("corpus_manifest_digest=%s\ncorpus_files=%d\n", manifest.Digest, len(manifest.Entries))
		return
	}
	if *corpus == "" || (!*manifestOnly && (*profile == "" || *repoPath == "")) || (*manifestOnly && *manifestOut == "") {
		fmt.Fprintln(os.Stderr, "usage: savings-report-runner (-profile raw|zstd -repo DIR | -manifest-only) -corpus DIR -manifest-out FILE [-evidence-out FILE]")
		os.Exit(2)
	}
	if *manifestOnly && *evidenceOut != "" {
		fmt.Fprintln(os.Stderr, "-evidence-out is not valid with -manifest-only")
		os.Exit(2)
	}
	if !*manifestOnly {
		if err := validateWorkDir(*corpus, *repoPath); err != nil {
			fmt.Fprintf(os.Stderr, "unsafe work directory: %v\n", err)
			os.Exit(1)
		}
		if err := validateDeploymentPaths(*corpus, *repoPath, *catalogPath, *indexPath, *modelPath); err != nil {
			fmt.Fprintf(os.Stderr, "unsafe deployment paths: %v\n", err)
			os.Exit(1)
		}
		if *temporaryPeakBytes < -1 {
			fmt.Fprintln(os.Stderr, "-temporary-peak-bytes must be non-negative or omitted")
			os.Exit(2)
		}
	}
	if err := validateOutputPath(*corpus, *manifestOut); err != nil {
		fmt.Fprintf(os.Stderr, "unsafe manifest output: %v\n", err)
		os.Exit(1)
	}
	if err := validateOutputPath(*corpus, *evidenceOut); err != nil {
		fmt.Fprintf(os.Stderr, "unsafe evidence output: %v\n", err)
		os.Exit(1)
	}
	manifest, err := BuildCorpusManifest(*corpus)
	if err != nil {
		fmt.Fprintf(os.Stderr, "scan corpus: %v\n", err)
		os.Exit(1)
	}
	if *manifestIn != "" {
		expected, readErr := ReadCorpusManifest(*manifestIn)
		if readErr != nil {
			fmt.Fprintf(os.Stderr, "read corpus manifest: %v\n", readErr)
			os.Exit(1)
		}
		if verifyErr := VerifyCorpusManifest(*corpus, expected); verifyErr != nil {
			fmt.Fprintf(os.Stderr, "verify corpus manifest: %v\n", verifyErr)
			os.Exit(1)
		}
		manifest = expected
	}
	if *manifestOnly {
		if err := writeCorpusManifestSummary(*manifestOut, manifest, os.Stdout); err != nil {
			fmt.Fprintf(os.Stderr, "write corpus manifest: %v\n", err)
			os.Exit(1)
		}
		return
	}

	ctx := context.Background()
	var driver repository.Driver
	switch *profile {
	case "raw":
		driver, err = repository.OpenDir(*repoPath)
	case "zstd":
		driver, err = repository.OpenZstdDir(*repoPath)
	default:
		fmt.Fprintf(os.Stderr, "unsupported profile %q\n", *profile)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "open %s repository: %v\n", *profile, err)
		os.Exit(1)
	}

	started := time.Now()
	placements := make([]repository.Receipt, 0, len(manifest.Entries))
	for _, entry := range manifest.Entries {
		path := filepath.Join(*corpus, filepath.FromSlash(entry.Path))
		info, statErr := os.Lstat(path)
		if statErr != nil || info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			if statErr == nil {
				statErr = fmt.Errorf("path is no longer a regular file")
			}
			fmt.Fprintf(os.Stderr, "open %s: corpus changed: %v\n", entry.Path, statErr)
			os.Exit(1)
		}
		body, err := openCorpusFile(*corpus, path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "open %s: %v\n", path, err)
			os.Exit(1)
		}
		expectedContentID := repository.AlgorithmSHA256 + ":" + entry.SHA256
		receipt, placeErr := driver.PlaceExact(ctx, expectedContentID, body)
		closeErr := body.Close()
		if placeErr != nil {
			fmt.Fprintf(os.Stderr, "place %s: %v\n", path, placeErr)
			os.Exit(1)
		}
		if closeErr != nil {
			fmt.Fprintf(os.Stderr, "close %s: %v\n", path, closeErr)
			os.Exit(1)
		}
		if receipt.ContentID != expectedContentID || receipt.Bytes != entry.Bytes {
			fmt.Fprintf(os.Stderr, "place %s: receipt differs from corpus manifest\n", entry.Path)
			os.Exit(1)
		}
		placements = append(placements, receipt)
	}
	after, err := BuildCorpusManifest(*corpus)
	if err != nil {
		fmt.Fprintf(os.Stderr, "rescan corpus: %v\n", err)
		os.Exit(1)
	}
	if !manifestEqual(manifest, after) {
		fmt.Fprintln(os.Stderr, "corpus changed during placement: manifest before and after differ")
		os.Exit(1)
	}

	report, err := repository.MeasureSavings(ctx, driver, placements)
	if err != nil {
		fmt.Fprintf(os.Stderr, "measure savings: %v\n", err)
		os.Exit(1)
	}
	externalOverhead, err := MeasureDeploymentOverhead(*catalogPath, *indexPath, *modelPath, *temporaryPeakBytes)
	if err != nil {
		fmt.Fprintf(os.Stderr, "measure deployment overhead: %v\n", err)
		os.Exit(1)
	}
	deployment := deploymentSavings(report, externalOverhead)
	repositoryManifest, err := BuildCorpusManifest(*repoPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "scan repository manifest: %v\n", err)
		os.Exit(1)
	}
	deployment.RepositoryManifest = repositoryManifest
	if manifest.Digest != "" && repositoryManifest.Digest != "" {
		deployment.Correlation = "CORRELATED_SINGLE_RUN"
	}
	desc := repository.DescribeProfile(driver)
	if err := writeJSONFile(*manifestOut, manifest); err != nil {
		fmt.Fprintf(os.Stderr, "write corpus manifest: %v\n", err)
		os.Exit(1)
	}
	if err := writeJSONFile(*evidenceOut, candidateEvidence(manifest, driver, desc, report, started, deployment)); err != nil {
		fmt.Fprintf(os.Stderr, "write candidate evidence: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("profile:                   %s / %s\n", desc.Repository, desc.Compression)
	fmt.Printf("logical bytes:             %d\n", report.LogicalBytes)
	fmt.Printf("duplicate files:           %d\n", report.DuplicateFiles)
	fmt.Printf("duplicate bytes:           %d\n", report.DuplicateBytes)
	fmt.Printf("compression saved bytes:   %d\n", report.CompressionSavedBytes)
	fmt.Printf("physical stored bytes:     %d\n", report.PhysicalStoredBytes)
	fmt.Printf("repository growth bytes:   %d\n", report.RepositoryGrowthBytes)
	fmt.Printf("overhead bytes:            %d\n", report.OverheadBytes)
	printOverheadCategory("overhead.repository_metadata", report.Overhead.RepositoryMetadata)
	printOverheadCategory("overhead.recovery_records", report.Overhead.RecoveryRecords)
	printOverheadCategory("overhead.index", report.Overhead.Index)
	printOverheadCategory("overhead.model", report.Overhead.Model)
	printOverheadCategory("overhead.temporary", report.Overhead.Temporary)
	fmt.Printf("net physical savings:      %d\n", report.NetPhysicalSavingsBytes)
	printOverheadCategory("deployment.catalog", deployment.External.Catalog)
	printOverheadCategory("deployment.index", deployment.External.Index)
	printOverheadCategory("deployment.model", deployment.External.Model)
	printOverheadCategory("deployment.temporary", deployment.External.Temporary)
	printOverheadCategory("deployment.net_savings", deployment.Net)
	fmt.Printf("physical/logical ratio:    %.3f\n", ratio(report.PhysicalStoredBytes, report.LogicalBytes))
	fmt.Printf("mechanisms:                %v\n", report.Mechanisms)
}

func writeCorpusManifestSummary(path string, manifest CorpusManifest, output io.Writer) error {
	if err := writeJSONFile(path, manifest); err != nil {
		return err
	}
	var logicalBytes int64
	for _, entry := range manifest.Entries {
		logicalBytes += entry.Bytes
	}
	if _, err := fmt.Fprintf(output, "corpus_manifest_digest=%s\n", manifest.Digest); err != nil {
		return err
	}
	if _, err := fmt.Fprintf(output, "corpus_files=%d\n", len(manifest.Entries)); err != nil {
		return err
	}
	_, err := fmt.Fprintf(output, "logical_bytes=%d\n", logicalBytes)
	return err
}

func printOverheadCategory(name string, category repository.SavingsOverheadCategory) {
	if category.Status != repository.SavingsCategoryMeasured {
		fmt.Printf("%-27s %s\n", name+":", category.Status)
		return
	}
	fmt.Printf("%-27s %d\n", name+":", category.Bytes)
}

func validateOutputPath(corpus, output string) error {
	if strings.TrimSpace(output) == "" {
		return nil
	}
	overlap, err := pathsOverlap(corpus, output)
	if err != nil {
		return err
	}
	if overlap {
		return errors.New("output must not be written inside the read-only corpus")
	}
	return nil
}

func ratio(n, d int64) float64 {
	if d == 0 {
		return 0
	}
	return float64(n) / float64(d)
}
