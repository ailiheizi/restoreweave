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
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/ailiheizi/restoreweave/server/internal/repository"
)

func main() {
	profile := flag.String("profile", "", "repository profile: raw or zstd")
	repoPath := flag.String("repo", "", "repository path to create")
	corpus := flag.String("corpus", "", "corpus directory to place")
	flag.Parse()
	if *profile == "" || *repoPath == "" || *corpus == "" {
		fmt.Fprintln(os.Stderr, "usage: savings-report-runner -profile raw|zstd -repo DIR -corpus DIR")
		os.Exit(2)
	}

	ctx := context.Background()
	var driver repository.Driver
	var err error
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

	paths, err := corpusFiles(*corpus)
	if err != nil {
		fmt.Fprintf(os.Stderr, "list corpus: %v\n", err)
		os.Exit(1)
	}
	placements := make([]repository.Receipt, 0, len(paths))
	for _, path := range paths {
		body, err := os.Open(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "open %s: %v\n", path, err)
			os.Exit(1)
		}
		receipt, placeErr := driver.Place(ctx, body)
		closeErr := body.Close()
		if placeErr != nil {
			fmt.Fprintf(os.Stderr, "place %s: %v\n", path, placeErr)
			os.Exit(1)
		}
		if closeErr != nil {
			fmt.Fprintf(os.Stderr, "close %s: %v\n", path, closeErr)
			os.Exit(1)
		}
		placements = append(placements, receipt)
	}

	report, err := repository.MeasureSavings(ctx, driver, placements)
	if err != nil {
		fmt.Fprintf(os.Stderr, "measure savings: %v\n", err)
		os.Exit(1)
	}
	desc := repository.DescribeProfile(driver)
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
	fmt.Printf("physical/logical ratio:    %.3f\n", ratio(report.PhysicalStoredBytes, report.LogicalBytes))
	fmt.Printf("mechanisms:                %v\n", report.Mechanisms)
}

func printOverheadCategory(name string, category repository.SavingsOverheadCategory) {
	if category.Status != repository.SavingsCategoryMeasured {
		fmt.Printf("%-27s %s\n", name+":", category.Status)
		return
	}
	fmt.Printf("%-27s %d\n", name+":", category.Bytes)
}

// corpusFiles returns the sorted regular files under the corpus directory.
func corpusFiles(root string) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !entry.Type().IsRegular() {
			return nil
		}
		paths = append(paths, path)
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	return paths, nil
}

func ratio(n, d int64) float64 {
	if d == 0 {
		return 0
	}
	return float64(n) / float64(d)
}
