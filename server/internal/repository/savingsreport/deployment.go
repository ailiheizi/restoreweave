//go:build savingsreport

package main

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/ailiheizi/restoreweave/server/internal/repository"
)

// DeploymentOverhead accounts for physical bytes owned by the surrounding
// deployment rather than by the repository driver. Empty paths remain
// UNMEASURED; they are not silently treated as zero.
type DeploymentOverhead struct {
	Catalog   repository.SavingsOverheadCategory `json:"catalog"`
	Index     repository.SavingsOverheadCategory `json:"index"`
	Model     repository.SavingsOverheadCategory `json:"model"`
	Temporary repository.SavingsOverheadCategory `json:"temporary"`
}

// DeploymentSavings extends the repository report without changing the
// repository package's accounting contract. Net is valid only when every
// deployment category was measured.
type DeploymentSavings struct {
	Overhead           repository.SavingsOverhead         `json:"repository_overhead"`
	External           DeploymentOverhead                 `json:"external_overhead"`
	Net                repository.SavingsOverheadCategory `json:"net_savings"`
	RepositoryManifest CorpusManifest                     `json:"repository_manifest"`
	Correlation        string                             `json:"correlation"`
}

type deploymentPath struct {
	name string
	path string
}

func unmeasuredCategory() repository.SavingsOverheadCategory {
	return repository.SavingsOverheadCategory{Status: repository.SavingsCategoryUnmeasured}
}

func measuredCategory(bytes int64) repository.SavingsOverheadCategory {
	return repository.SavingsOverheadCategory{Bytes: bytes, Status: repository.SavingsCategoryMeasured}
}

// MeasureDeploymentOverhead reads only the explicitly supplied paths. It does
// not create, modify, or follow symlinks, and a caller-supplied temporary peak
// is kept separate from filesystem scans.
func MeasureDeploymentOverhead(catalogPath, indexPath, modelPath string, temporaryPeakBytes int64) (DeploymentOverhead, error) {
	result := DeploymentOverhead{
		Catalog:   unmeasuredCategory(),
		Index:     unmeasuredCategory(),
		Model:     unmeasuredCategory(),
		Temporary: unmeasuredCategory(),
	}
	paths := []deploymentPath{
		{name: "catalog", path: catalogPath},
		{name: "index", path: indexPath},
		{name: "model", path: modelPath},
	}
	for _, item := range paths {
		if strings.TrimSpace(item.path) == "" {
			continue
		}
		bytes, err := measureReadOnlyPath(item.path)
		if err != nil {
			return DeploymentOverhead{}, fmt.Errorf("measure %s path: %w", item.name, err)
		}
		switch item.name {
		case "catalog":
			result.Catalog = measuredCategory(bytes)
		case "index":
			result.Index = measuredCategory(bytes)
		case "model":
			result.Model = measuredCategory(bytes)
		}
	}
	if temporaryPeakBytes >= 0 {
		result.Temporary = measuredCategory(temporaryPeakBytes)
	} else if temporaryPeakBytes != -1 {
		return DeploymentOverhead{}, fmt.Errorf("temporary peak bytes must be non-negative or omitted, got %d", temporaryPeakBytes)
	}
	return result, nil
}

func measureReadOnlyPath(path string) (int64, error) {
	if strings.TrimSpace(path) == "" {
		return 0, errors.New("path is required")
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return 0, fmt.Errorf("resolve path: %w", err)
	}
	info, err := os.Lstat(abs)
	if err != nil {
		return 0, err
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return 0, errors.New("path must not be a symlink")
	}
	if info.Mode().IsRegular() {
		return info.Size(), nil
	}
	if !info.IsDir() {
		return 0, fmt.Errorf("path is not a regular file or directory")
	}

	var total int64
	err = filepath.WalkDir(abs, func(current string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if current == abs {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("contains symlink %q", current)
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("contains non-regular file %q", current)
		}
		fileInfo, err := entry.Info()
		if err != nil {
			return err
		}
		if fileInfo.Size() < 0 || total > (int64(^uint64(0)>>1)-fileInfo.Size()) {
			return errors.New("path size overflows int64")
		}
		total += fileInfo.Size()
		return nil
	})
	if err != nil {
		return 0, err
	}
	return total, nil
}

func validateDeploymentPaths(corpusPath, repositoryPath string, catalogPath, indexPath, modelPath string) error {
	items := []deploymentPath{
		{name: "corpus", path: corpusPath},
		{name: "repository", path: repositoryPath},
		{name: "catalog", path: catalogPath},
		{name: "index", path: indexPath},
		{name: "model", path: modelPath},
	}
	active := make([]deploymentPath, 0, len(items))
	for _, item := range items {
		if strings.TrimSpace(item.path) != "" {
			active = append(active, item)
		}
	}
	for i := range active {
		for j := i + 1; j < len(active); j++ {
			overlap, err := pathsOverlap(active[i].path, active[j].path)
			if err != nil {
				return fmt.Errorf("compare %s and %s paths: %w", active[i].name, active[j].name, err)
			}
			if overlap {
				return fmt.Errorf("%s and %s paths overlap", active[i].name, active[j].name)
			}
		}
	}
	return nil
}

func allDeploymentCategories(overhead DeploymentOverhead) []struct {
	name     string
	category repository.SavingsOverheadCategory
} {
	items := []struct {
		name     string
		category repository.SavingsOverheadCategory
	}{
		{name: "catalog", category: overhead.Catalog},
		{name: "index", category: overhead.Index},
		{name: "model", category: overhead.Model},
		{name: "temporary", category: overhead.Temporary},
	}
	sort.Slice(items, func(i, j int) bool { return items[i].name < items[j].name })
	return items
}

func deploymentSavings(report repository.SavingsReport, external DeploymentOverhead) DeploymentSavings {
	result := DeploymentSavings{
		Overhead:    report.Overhead,
		External:    external,
		Net:         unmeasuredCategory(),
		Correlation: "UNBOUND",
	}
	var externalBytes int64
	complete := true
	for _, item := range allDeploymentCategories(external) {
		if item.category.Status != repository.SavingsCategoryMeasured {
			complete = false
			continue
		}
		externalBytes += item.category.Bytes
	}
	if complete {
		result.Net = measuredCategory(report.LogicalBytes - report.RepositoryGrowthBytes - externalBytes)
	}
	return result
}
