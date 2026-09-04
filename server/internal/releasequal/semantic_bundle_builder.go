package releasequal

import (
	"context"
	"fmt"

	"github.com/ailiheizi/restoreweave/server/internal/search"
)

// BuildAdmittedSemanticBundleArchive creates a candidate-only offline archive
// from an already admitted host bundle. It never installs or mutates the
// source bundle; the destination is a newly created archive path.
func BuildAdmittedSemanticBundleArchive(ctx context.Context, destination, sourceRoot string) (search.SemanticBundleArchive, error) {
	admission, err := search.LoadSemanticBundle(sourceRoot)
	if err != nil {
		return search.SemanticBundleArchive{}, fmt.Errorf("load semantic bundle: %w", err)
	}
	if err := search.ValidateDefaultSemanticBundleAdmission(admission); err != nil {
		return search.SemanticBundleArchive{}, fmt.Errorf("validate default semantic bundle: %w", err)
	}
	return search.PackageSemanticBundleArchive(ctx, destination, sourceRoot, admission)
}
