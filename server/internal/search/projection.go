package search

import (
	"context"
	"database/sql"
	"errors"
	"os"

	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
)

// LexicalSubjectCoverage returns the subject IDs actually present in one
// generation. It is disposable projection evidence, never catalog state.
func LexicalSubjectCoverage(ctx context.Context, generation sqlite.IndexGeneration) (map[string]struct{}, error) {
	if generation.Dimension != DimensionLexical || generation.DBPath == "" {
		return nil, ErrUnavailable
	}
	info, err := os.Lstat(generation.DBPath)
	if err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		if err == nil {
			err = ErrUnavailable
		}
		return nil, err
	}
	db, err := sql.Open("sqlite", generation.DBPath)
	if err != nil {
		return nil, err
	}
	defer db.Close()
	rows, err := db.QueryContext(ctx, `SELECT DISTINCT subject_id FROM documents WHERE subject_id <> ''`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]struct{}{}
	for rows.Next() {
		var subject string
		if err := rows.Scan(&subject); err != nil {
			return nil, err
		}
		out[subject] = struct{}{}
	}
	return out, rows.Err()
}

// SemanticSubjectCoverage validates the admitted semantic generation and
// returns only subject IDs proven by its signed receipt/backend coverage.
func (idx *Indexer) SemanticSubjectCoverage(ctx context.Context, workspaceID string) (map[string]struct{}, error) {
	if idx == nil || idx.Store == nil || idx.SemanticProvider == nil || idx.SemanticZvec == nil || idx.SemanticManifest == (EmbeddingGenerationManifest{}) {
		return nil, ErrUnavailable
	}
	generation, err := idx.Store.LatestIndexGeneration(ctx, workspaceID, DimensionSemantic)
	if err != nil {
		return nil, err
	}
	if err := idx.ensureSemanticGenerationVerified(ctx, generation); err != nil {
		return nil, err
	}
	identities, _, _, err := readSemanticGenerationReceipt(generation)
	if err != nil {
		return nil, err
	}
	out := map[string]struct{}{}
	for _, identity := range identities {
		if identity.SubjectID != "" {
			out[identity.SubjectID] = struct{}{}
		}
	}
	if len(out) == 0 {
		return nil, errors.New("semantic generation has no subject coverage")
	}
	return out, nil
}
