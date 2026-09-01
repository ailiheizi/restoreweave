package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

func validProtectionMode(value ProtectionMode) bool {
	switch value {
	case ProtectionStoreExact, ProtectionStoreExactWithExternalFallback,
		ProtectionLinkOnly, ProtectionMetadataOnly:
		return true
	default:
		return false
	}
}

func validProtectionOutcome(value ProtectionOutcome) bool {
	switch value {
	case ProtectionExactProtected, ProtectionExactFallback,
		ProtectionExternalReplayable, ProtectionLinkOnlyUnprotected,
		ProtectionExplicitlyUnprotected, ProtectionBlocked,
		ProtectionUnavailable:
		return true
	default:
		return false
	}
}

func validRecoveryReferenceKind(value RecoveryReferenceKind) bool {
	switch value {
	case RecoveryExactRepresentation, RecoveryExactReversible,
		RecoveryExternalLocator, RecoveryUserRecipe:
		return true
	default:
		return false
	}
}

func validRecoveryClaim(value RecoveryClaim) bool {
	switch value {
	case RecoveryClaimRestoreVerified, RecoveryClaimExternalReplayable,
		RecoveryClaimLinkOnlyUnprotected, RecoveryClaimUnavailable:
		return true
	default:
		return false
	}
}

func validateProtectionPair(mode ProtectionMode, outcome ProtectionOutcome) error {
	if !validProtectionMode(mode) {
		return fmt.Errorf("invalid protection mode %q", mode)
	}
	if !validProtectionOutcome(outcome) {
		return fmt.Errorf("invalid protection outcome %q", outcome)
	}
	switch mode {
	case ProtectionStoreExact, ProtectionStoreExactWithExternalFallback:
		if outcome != ProtectionExactProtected && outcome != ProtectionExactFallback && outcome != ProtectionBlocked {
			return fmt.Errorf("protection mode %q requires EXACT_PROTECTED, EXACT_FALLBACK, or BLOCKED outcome", mode)
		}
	case ProtectionLinkOnly:
		if outcome != ProtectionExternalReplayable && outcome != ProtectionLinkOnlyUnprotected && outcome != ProtectionExplicitlyUnprotected && outcome != ProtectionBlocked {
			return errors.New("LINK_ONLY requires EXTERNAL_REPLAYABLE, LINK_ONLY_UNPROTECTED, EXPLICITLY_UNPROTECTED, or BLOCKED outcome")
		}
	case ProtectionMetadataOnly:
		if outcome != ProtectionUnavailable && outcome != ProtectionExplicitlyUnprotected && outcome != ProtectionBlocked {
			return errors.New("METADATA_ONLY requires UNAVAILABLE, EXPLICITLY_UNPROTECTED, or BLOCKED outcome")
		}
	}
	return nil
}

func validateExpectedLength(name string, value *int64) error {
	if value != nil && *value < 0 {
		return fmt.Errorf("%s cannot be negative", name)
	}
	return nil
}

func optionalID(name, value string) error {
	if value == "" {
		return nil
	}
	return requireID(name, value)
}

func optionalText(name, value string) error {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return requireText(name, value)
}

// prepareProtectionRecord validates and normalizes one current protection
// projection before it is inserted or revised. The durable signed snapshot
// remains immutable; this row is only the latest operational projection.
func (tx *Tx) prepareProtectionRecord(ctx context.Context, record *ProtectionRecord) error {
	if record == nil {
		return errors.New("protection record is required")
	}
	for name, value := range map[string]string{
		"protection record id": record.ID,
		"workspace id":         record.WorkspaceID,
		"subject ref":          record.SubjectRef,
	} {
		if err := requireID(name, value); err != nil {
			return err
		}
	}
	if err := validateProtectionPair(record.Mode, record.Outcome); err != nil {
		return err
	}
	if err := validateExpectedLength("protection expected logical length", record.ExpectedLogicalLength); err != nil {
		return err
	}
	if err := optionalID("local representation id", record.LocalRepresentationID); err != nil {
		return err
	}
	switch record.Mode {
	case ProtectionStoreExact, ProtectionStoreExactWithExternalFallback:
		if (record.Outcome == ProtectionExactProtected || record.Outcome == ProtectionExactFallback) && record.LocalRepresentationID == "" {
			return fmt.Errorf("protection outcome %q requires a local representation id", record.Outcome)
		}
		if (record.Outcome == ProtectionExactProtected || record.Outcome == ProtectionExactFallback) && strings.TrimSpace(record.LastVerificationRef) == "" {
			return fmt.Errorf("protection outcome %q requires verification evidence", record.Outcome)
		}
	case ProtectionLinkOnly, ProtectionMetadataOnly:
		if record.LocalRepresentationID != "" {
			return fmt.Errorf("protection mode %q cannot carry a local representation id", record.Mode)
		}
		if record.Outcome == ProtectionExternalReplayable {
			return errors.New("EXTERNAL_REPLAYABLE protection requires an atomic qualified recovery closure")
		}
	}
	if record.LocalRepresentationID != "" {
		var representationContentID string
		var representationLength int64
		if err := tx.tx.QueryRowContext(ctx, `
SELECT content_id, decoded_length FROM representations
WHERE workspace_id = ? AND representation_id = ?`,
			record.WorkspaceID, record.LocalRepresentationID).Scan(
			&representationContentID, &representationLength); err != nil {
			return rowError("protection local representation", err)
		}
		if record.ExpectedContentID == "" {
			record.ExpectedContentID = representationContentID
		} else if record.ExpectedContentID != representationContentID {
			return errors.New("protection expected content identity differs from local representation")
		}
		if record.ExpectedLogicalLength == nil {
			record.ExpectedLogicalLength = int64PtrValue(representationLength)
		} else if *record.ExpectedLogicalLength != representationLength {
			return errors.New("protection expected logical length differs from local representation")
		}
	}
	for name, value := range map[string]string{
		"policy decision ref":   record.PolicyDecisionRef,
		"last verification ref": record.LastVerificationRef,
	} {
		if err := optionalText(name, value); err != nil {
			return err
		}
	}
	if record.Revision == 0 {
		record.Revision = 1
	}
	if record.Revision < 1 {
		return errors.New("protection record revision must be positive")
	}
	metadata, err := normalizeJSON(record.Metadata)
	if err != nil {
		return fmt.Errorf("protection record metadata: %w", err)
	}
	record.Metadata = metadata
	record.CreatedAt = recordTime(record.CreatedAt, tx.now)
	if record.UpdatedAt.IsZero() {
		record.UpdatedAt = record.CreatedAt
	} else {
		record.UpdatedAt = record.UpdatedAt.UTC()
	}
	return nil
}

// InsertProtectionRecord adds the current explicit protection fact for a
// subject. LocalRepresentationID is an optional primary hint; additional
// local alternatives are represented by exact RecoveryReference rows. The
// subject uniqueness constraint intentionally makes accidental duplicate
// policy rows fail closed; use UpsertProtectionRecord for a new observation
// of an existing stable subject.
func (tx *Tx) InsertProtectionRecord(ctx context.Context, record *ProtectionRecord) error {
	if err := tx.prepareProtectionRecord(ctx, record); err != nil {
		return err
	}
	if err := insertOne(ctx, tx.tx, `
INSERT INTO protection_records(
    protection_record_id, workspace_id, subject_ref, mode, outcome,
    expected_content_id, expected_logical_length, local_representation_id,
    policy_decision_ref, last_verification_ref, last_verified_at_ns,
    revision, metadata_json, created_at_ns, updated_at_ns
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT DO NOTHING`,
		record.ID, record.WorkspaceID, record.SubjectRef, record.Mode, record.Outcome,
		nullableString(record.ExpectedContentID), nullableInt64(record.ExpectedLogicalLength),
		nullableString(record.LocalRepresentationID), record.PolicyDecisionRef,
		record.LastVerificationRef, nullableTime(record.LastVerifiedAt), record.Revision,
		string(record.Metadata), record.CreatedAt.UnixNano(), record.UpdatedAt.UnixNano()); err != nil {
		return fmt.Errorf("insert protection record: %w", err)
	}
	return nil
}

// UpsertProtectionRecord revises the current protection projection for a
// stable subject in this transaction. Existing record IDs are retained and
// their revision is advanced; signed snapshot/recovery facts remain separate
// immutable history. The returned record is populated with the actual
// protection_record_id and revision so new recovery references can attach to
// the current projection safely.
func (tx *Tx) UpsertProtectionRecord(ctx context.Context, record *ProtectionRecord) (ProtectionRecord, error) {
	if err := tx.prepareProtectionRecord(ctx, record); err != nil {
		return ProtectionRecord{}, err
	}
	var currentID string
	var currentRevision int64
	var currentCreated int64
	err := tx.tx.QueryRowContext(ctx, `
SELECT protection_record_id, revision, created_at_ns
FROM protection_records
WHERE workspace_id = ? AND subject_ref = ?`, record.WorkspaceID, record.SubjectRef).Scan(
		&currentID, &currentRevision, &currentCreated)
	if errors.Is(err, sql.ErrNoRows) {
		if err := insertOne(ctx, tx.tx, `
INSERT INTO protection_records(
    protection_record_id, workspace_id, subject_ref, mode, outcome,
    expected_content_id, expected_logical_length, local_representation_id,
    policy_decision_ref, last_verification_ref, last_verified_at_ns,
    revision, metadata_json, created_at_ns, updated_at_ns
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT DO NOTHING`,
			record.ID, record.WorkspaceID, record.SubjectRef, record.Mode, record.Outcome,
			nullableString(record.ExpectedContentID), nullableInt64(record.ExpectedLogicalLength),
			nullableString(record.LocalRepresentationID), record.PolicyDecisionRef,
			record.LastVerificationRef, nullableTime(record.LastVerifiedAt), record.Revision,
			string(record.Metadata), record.CreatedAt.UnixNano(), record.UpdatedAt.UnixNano()); err != nil {
			return ProtectionRecord{}, fmt.Errorf("insert protection record: %w", err)
		}
		return *record, nil
	}
	if err != nil {
		return ProtectionRecord{}, fmt.Errorf("read current protection record: %w", err)
	}
	record.ID = currentID
	record.Revision = currentRevision + 1
	record.CreatedAt = time.Unix(0, currentCreated).UTC()
	if _, err := tx.tx.ExecContext(ctx, `
UPDATE protection_records SET
    mode = ?, outcome = ?, expected_content_id = ?, expected_logical_length = ?,
    local_representation_id = ?, policy_decision_ref = ?, last_verification_ref = ?,
    last_verified_at_ns = ?, revision = ?, metadata_json = ?, updated_at_ns = ?
WHERE workspace_id = ? AND protection_record_id = ?`,
		record.Mode, record.Outcome, nullableString(record.ExpectedContentID),
		nullableInt64(record.ExpectedLogicalLength), nullableString(record.LocalRepresentationID),
		record.PolicyDecisionRef, record.LastVerificationRef, nullableTime(record.LastVerifiedAt),
		record.Revision, string(record.Metadata), record.UpdatedAt.UnixNano(),
		record.WorkspaceID, record.ID); err != nil {
		return ProtectionRecord{}, fmt.Errorf("update protection record: %w", err)
	}
	return *record, nil
}

func (s *Store) InsertProtectionRecord(ctx context.Context, record *ProtectionRecord) error {
	return s.Update(ctx, func(tx *Tx) error { return tx.InsertProtectionRecord(ctx, record) })
}

// UpsertProtectionRecord is the store-level convenience form of the
// transaction-scoped current-projection revision operation.
func (s *Store) UpsertProtectionRecord(ctx context.Context, record *ProtectionRecord) (ProtectionRecord, error) {
	var result ProtectionRecord
	err := s.Update(ctx, func(tx *Tx) error {
		var err error
		result, err = tx.UpsertProtectionRecord(ctx, record)
		return err
	})
	return result, err
}

func (s *Store) GetProtectionRecord(ctx context.Context, workspaceID, recordID string) (ProtectionRecord, error) {
	if err := requireID("workspace id", workspaceID); err != nil {
		return ProtectionRecord{}, err
	}
	if err := requireID("protection record id", recordID); err != nil {
		return ProtectionRecord{}, err
	}
	return scanProtectionRecord(s.db.QueryRowContext(ctx, protectionRecordSelect+`
WHERE workspace_id = ? AND protection_record_id = ?`, workspaceID, recordID))
}

func (s *Store) GetProtectionRecordBySubject(ctx context.Context, workspaceID, subjectRef string) (ProtectionRecord, error) {
	if err := requireID("workspace id", workspaceID); err != nil {
		return ProtectionRecord{}, err
	}
	if err := requireID("subject ref", subjectRef); err != nil {
		return ProtectionRecord{}, err
	}
	return scanProtectionRecord(s.db.QueryRowContext(ctx, protectionRecordSelect+`
WHERE workspace_id = ? AND subject_ref = ?`, workspaceID, subjectRef))
}

func (s *Store) ListProtectionRecords(ctx context.Context, workspaceID string) ([]ProtectionRecord, error) {
	if err := requireID("workspace id", workspaceID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, protectionRecordSelect+`
WHERE workspace_id = ? ORDER BY subject_ref, protection_record_id`, workspaceID)
	if err != nil {
		return nil, fmt.Errorf("list protection records: %w", err)
	}
	defer rows.Close()
	var records []ProtectionRecord
	for rows.Next() {
		record, err := scanProtectionRecord(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate protection records: %w", err)
	}
	return records, nil
}

const protectionRecordSelect = `
SELECT protection_record_id, workspace_id, subject_ref, mode, outcome,
       expected_content_id, expected_logical_length, local_representation_id,
       policy_decision_ref, last_verification_ref, last_verified_at_ns,
       revision, metadata_json, created_at_ns, updated_at_ns
FROM protection_records `

func scanProtectionRecord(scanner rowScanner) (ProtectionRecord, error) {
	var record ProtectionRecord
	var expectedContent sql.NullString
	var expectedLength, lastVerified sql.NullInt64
	var localRepresentation sql.NullString
	var metadata string
	var created, updated int64
	if err := scanner.Scan(
		&record.ID, &record.WorkspaceID, &record.SubjectRef, &record.Mode, &record.Outcome,
		&expectedContent, &expectedLength, &localRepresentation,
		&record.PolicyDecisionRef, &record.LastVerificationRef, &lastVerified,
		&record.Revision, &metadata, &created, &updated,
	); err != nil {
		return record, rowError("protection record", err)
	}
	if expectedContent.Valid {
		record.ExpectedContentID = expectedContent.String
	}
	record.ExpectedLogicalLength = int64Pointer(expectedLength)
	if localRepresentation.Valid {
		record.LocalRepresentationID = localRepresentation.String
	}
	record.LastVerifiedAt = nullableTimePointer(lastVerified)
	record.Metadata = json.RawMessage(metadata)
	record.CreatedAt = time.Unix(0, created).UTC()
	record.UpdatedAt = time.Unix(0, updated).UTC()
	return record, nil
}

// InsertRecoveryReference adds an ordered alternative for a protection
// record. References are immutable route descriptions; validation updates
// should be represented by a new reference revision in a later API.
func (tx *Tx) InsertRecoveryReference(ctx context.Context, record *RecoveryReference) error {
	if record == nil {
		return errors.New("recovery reference is required")
	}
	for name, value := range map[string]string{
		"recovery reference id": record.ID,
		"workspace id":          record.WorkspaceID,
		"protection record id":  record.ProtectionRecordID,
		"subject ref":           record.SubjectRef,
	} {
		if err := requireID(name, value); err != nil {
			return err
		}
	}
	if !validRecoveryReferenceKind(record.Kind) {
		return fmt.Errorf("invalid recovery reference kind %q", record.Kind)
	}
	if !validRecoveryClaim(record.Claim) {
		return fmt.Errorf("invalid recovery claim %q", record.Claim)
	}
	if record.Priority < 0 {
		return errors.New("recovery reference priority cannot be negative")
	}
	if err := validateExpectedLength("recovery expected logical length", record.ExpectedLogicalLength); err != nil {
		return err
	}
	if err := optionalID("representation id", record.RepresentationID); err != nil {
		return err
	}
	if err := optionalID("external binding id", record.ExternalBindingID); err != nil {
		return err
	}
	switch record.Kind {
	case RecoveryExactRepresentation, RecoveryExactReversible:
		if record.RepresentationID == "" {
			return fmt.Errorf("%s recovery reference requires a representation id", record.Kind)
		}
		if record.ExternalBindingID != "" {
			return fmt.Errorf("%s recovery reference cannot also carry an external binding", record.Kind)
		}
		if record.Claim != RecoveryClaimRestoreVerified {
			return fmt.Errorf("%s recovery reference must use RESTORE_VERIFIED claim", record.Kind)
		}
		if record.Kind == RecoveryExactReversible && strings.TrimSpace(record.CodecProfileRef) == "" {
			return errors.New("exact reversible recovery reference requires a codec profile ref")
		}
	case RecoveryExternalLocator:
		if record.ExternalBindingID == "" {
			return errors.New("external locator recovery reference requires an external binding id")
		}
		if record.RepresentationID != "" {
			return errors.New("external locator recovery reference cannot also carry a representation")
		}
		if record.Claim != RecoveryClaimExternalReplayable && record.Claim != RecoveryClaimLinkOnlyUnprotected {
			return errors.New("external locator recovery reference cannot claim RESTORE_VERIFIED")
		}
	case RecoveryUserRecipe:
		if record.RepresentationID != "" && record.ExternalBindingID != "" {
			return errors.New("user recipe recovery reference cannot carry both local and external routes")
		}
	}
	var protectionSubject string
	if err := tx.tx.QueryRowContext(ctx, `
SELECT subject_ref FROM protection_records
WHERE workspace_id = ? AND protection_record_id = ?`,
		record.WorkspaceID, record.ProtectionRecordID).Scan(&protectionSubject); err != nil {
		return rowError("recovery protection record", err)
	}
	if protectionSubject != record.SubjectRef {
		return errors.New("recovery reference subject differs from protection record subject")
	}
	if record.ExternalBindingID != "" {
		var bindingSubject string
		if err := tx.tx.QueryRowContext(ctx, `
SELECT subject_ref FROM external_bindings
WHERE workspace_id = ? AND external_binding_id = ?`,
			record.WorkspaceID, record.ExternalBindingID).Scan(&bindingSubject); err != nil {
			return rowError("recovery external binding", err)
		}
		if bindingSubject != record.SubjectRef {
			return errors.New("recovery external binding subject differs from protection subject")
		}
	}
	if record.RepresentationID != "" {
		var representationContentID string
		var representationLength int64
		if err := tx.tx.QueryRowContext(ctx, `
SELECT content_id, decoded_length FROM representations
WHERE workspace_id = ? AND representation_id = ?`,
			record.WorkspaceID, record.RepresentationID).Scan(
			&representationContentID, &representationLength); err != nil {
			return rowError("recovery representation", err)
		}
		if record.ExpectedContentID == "" {
			record.ExpectedContentID = representationContentID
		} else if record.ExpectedContentID != representationContentID {
			return errors.New("recovery expected content identity differs from representation")
		}
		if record.ExpectedLogicalLength == nil {
			record.ExpectedLogicalLength = int64PtrValue(representationLength)
		} else if *record.ExpectedLogicalLength != representationLength {
			return errors.New("recovery expected logical length differs from representation")
		}
	}
	for name, value := range map[string]string{
		"codec profile ref":     record.CodecProfileRef,
		"policy ref":            record.PolicyRef,
		"rights evidence ref":   record.RightsEvidenceRef,
		"credential ref":        record.CredentialRef,
		"operator decision ref": record.OperatorDecisionRef,
		"record digest":         record.RecordDigest,
	} {
		if err := optionalText(name, value); err != nil {
			return err
		}
	}
	if strings.TrimSpace(record.Status) == "" {
		record.Status = "UNKNOWN"
	}
	recipe, err := normalizeJSON(record.Recipe)
	if err != nil {
		return fmt.Errorf("recovery recipe: %w", err)
	}
	verification, err := normalizeJSON(record.Verification)
	if err != nil {
		return fmt.Errorf("recovery verification: %w", err)
	}
	metadata, err := normalizeJSON(record.Metadata)
	if err != nil {
		return fmt.Errorf("recovery reference metadata: %w", err)
	}
	record.Recipe, record.Verification, record.Metadata = recipe, verification, metadata
	if record.Claim == RecoveryClaimExternalReplayable {
		var evidence struct {
			Verified bool `json:"verified"`
		}
		if strings.ToUpper(strings.TrimSpace(record.Status)) != "VERIFIED" || json.Unmarshal(verification, &evidence) != nil || !evidence.Verified {
			return errors.New("EXTERNAL_REPLAYABLE requires VERIFIED status and explicit verification evidence")
		}
		var qualified int
		if err := tx.tx.QueryRowContext(ctx, `
SELECT COUNT(*) FROM external_locators
WHERE workspace_id = ? AND binding_id = ?
  AND availability = 'AVAILABLE' AND validation_status = 'VERIFIED'`,
			record.WorkspaceID, record.ExternalBindingID).Scan(&qualified); err != nil {
			return fmt.Errorf("count verified external locators: %w", err)
		}
		if qualified == 0 {
			return errors.New("EXTERNAL_REPLAYABLE requires at least one available, verified locator")
		}
	}
	record.CreatedAt = recordTime(record.CreatedAt, tx.now)
	if record.UpdatedAt.IsZero() {
		record.UpdatedAt = record.CreatedAt
	} else {
		record.UpdatedAt = record.UpdatedAt.UTC()
	}
	if err := insertOne(ctx, tx.tx, `
INSERT INTO recovery_references(
    recovery_reference_id, workspace_id, protection_record_id, subject_ref,
    kind, priority, claim, expected_content_id, expected_logical_length,
    representation_id, external_binding_id, codec_profile_ref, recipe_json,
    verification_json, status, last_validated_at_ns, expires_at_ns,
    policy_ref, rights_evidence_ref, credential_ref, operator_decision_ref,
    record_digest, metadata_json, created_at_ns, updated_at_ns
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT DO NOTHING`,
		record.ID, record.WorkspaceID, record.ProtectionRecordID, record.SubjectRef,
		record.Kind, record.Priority, record.Claim, nullableString(record.ExpectedContentID),
		nullableInt64(record.ExpectedLogicalLength), nullableString(record.RepresentationID),
		nullableString(record.ExternalBindingID), record.CodecProfileRef, string(recipe),
		string(verification), record.Status, nullableTime(record.LastValidatedAt),
		nullableTime(record.ExpiresAt), record.PolicyRef, record.RightsEvidenceRef,
		record.CredentialRef, record.OperatorDecisionRef, record.RecordDigest,
		string(metadata), record.CreatedAt.UnixNano(), record.UpdatedAt.UnixNano()); err != nil {
		return fmt.Errorf("insert recovery reference: %w", err)
	}
	return nil
}

func (s *Store) InsertRecoveryReference(ctx context.Context, record *RecoveryReference) error {
	return s.Update(ctx, func(tx *Tx) error { return tx.InsertRecoveryReference(ctx, record) })
}

func (s *Store) GetRecoveryReference(ctx context.Context, workspaceID, referenceID string) (RecoveryReference, error) {
	if err := requireID("workspace id", workspaceID); err != nil {
		return RecoveryReference{}, err
	}
	if err := requireID("recovery reference id", referenceID); err != nil {
		return RecoveryReference{}, err
	}
	return scanRecoveryReference(s.db.QueryRowContext(ctx, recoveryReferenceSelect+`
WHERE workspace_id = ? AND recovery_reference_id = ?`, workspaceID, referenceID))
}

func (s *Store) ListRecoveryReferences(ctx context.Context, workspaceID, protectionRecordID string) ([]RecoveryReference, error) {
	if err := requireID("workspace id", workspaceID); err != nil {
		return nil, err
	}
	if err := optionalID("protection record id", protectionRecordID); err != nil {
		return nil, err
	}
	query := recoveryReferenceSelect + `WHERE workspace_id = ?`
	args := []any{workspaceID}
	if protectionRecordID != "" {
		query += ` AND protection_record_id = ?`
		args = append(args, protectionRecordID)
	}
	query += ` ORDER BY priority, recovery_reference_id`
	return scanRecoveryReferences(s.db.QueryContext(ctx, query, args...))
}

func (s *Store) ListRecoveryReferencesBySubject(ctx context.Context, workspaceID, subjectRef string) ([]RecoveryReference, error) {
	if err := requireID("workspace id", workspaceID); err != nil {
		return nil, err
	}
	if err := requireID("subject ref", subjectRef); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, recoveryReferenceSelect+`
WHERE workspace_id = ? AND subject_ref = ? ORDER BY priority, recovery_reference_id`, workspaceID, subjectRef)
	if err != nil {
		return nil, fmt.Errorf("list recovery references by subject: %w", err)
	}
	return scanRecoveryReferencesRows(rows)
}

func (s *Store) ListRecoveryReferencesByProtectionRecord(ctx context.Context, workspaceID, protectionRecordID string) ([]RecoveryReference, error) {
	return s.ListRecoveryReferences(ctx, workspaceID, protectionRecordID)
}

const recoveryReferenceSelect = `
SELECT recovery_reference_id, workspace_id, protection_record_id, subject_ref,
       kind, priority, claim, expected_content_id, expected_logical_length,
       representation_id, external_binding_id, codec_profile_ref, recipe_json,
       verification_json, status, last_validated_at_ns, expires_at_ns,
       policy_ref, rights_evidence_ref, credential_ref, operator_decision_ref,
       record_digest, metadata_json, created_at_ns, updated_at_ns
FROM recovery_references `

func scanRecoveryReferences(query *sql.Rows, err error) ([]RecoveryReference, error) {
	if err != nil {
		return nil, fmt.Errorf("list recovery references: %w", err)
	}
	return scanRecoveryReferencesRows(query)
}

func scanRecoveryReferencesRows(rows *sql.Rows) ([]RecoveryReference, error) {
	defer rows.Close()
	var records []RecoveryReference
	for rows.Next() {
		record, err := scanRecoveryReference(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate recovery references: %w", err)
	}
	return records, nil
}

func scanRecoveryReference(scanner rowScanner) (RecoveryReference, error) {
	var record RecoveryReference
	var expectedContent sql.NullString
	var expectedLength, validated, expires sql.NullInt64
	var representation, binding sql.NullString
	var recipe, verification, metadata string
	var created, updated int64
	if err := scanner.Scan(
		&record.ID, &record.WorkspaceID, &record.ProtectionRecordID, &record.SubjectRef,
		&record.Kind, &record.Priority, &record.Claim, &expectedContent,
		&expectedLength, &representation, &binding, &record.CodecProfileRef,
		&recipe, &verification, &record.Status, &validated, &expires,
		&record.PolicyRef, &record.RightsEvidenceRef, &record.CredentialRef,
		&record.OperatorDecisionRef, &record.RecordDigest, &metadata, &created, &updated,
	); err != nil {
		return record, rowError("recovery reference", err)
	}
	if expectedContent.Valid {
		record.ExpectedContentID = expectedContent.String
	}
	record.ExpectedLogicalLength = int64Pointer(expectedLength)
	if representation.Valid {
		record.RepresentationID = representation.String
	}
	if binding.Valid {
		record.ExternalBindingID = binding.String
	}
	record.Recipe = json.RawMessage(recipe)
	record.Verification = json.RawMessage(verification)
	record.Metadata = json.RawMessage(metadata)
	record.LastValidatedAt = nullableTimePointer(validated)
	record.ExpiresAt = nullableTimePointer(expires)
	record.CreatedAt = time.Unix(0, created).UTC()
	record.UpdatedAt = time.Unix(0, updated).UTC()
	return record, nil
}

// InsertExternalBinding adds an immutable provider identity. A new provider
// revision should use a new binding ID; this keeps old recovery records
// replayable even when a remote account or branch changes.
func (tx *Tx) InsertExternalBinding(ctx context.Context, record *ExternalBinding) error {
	if record == nil {
		return errors.New("external binding is required")
	}
	for name, value := range map[string]string{
		"external binding id": record.ID,
		"workspace id":        record.WorkspaceID,
		"subject ref":         record.SubjectRef,
		"provider kind":       record.ProviderKind,
		"stable identity":     record.StableIdentity,
		"binding digest":      record.BindingDigest,
	} {
		if err := requireText(name, value); err != nil {
			return err
		}
	}
	if err := requireID("external binding id", record.ID); err != nil {
		return err
	}
	if err := requireID("workspace id", record.WorkspaceID); err != nil {
		return err
	}
	if err := requireID("subject ref", record.SubjectRef); err != nil {
		return err
	}
	if record.Revision == 0 {
		record.Revision = 1
	}
	if record.Revision < 1 {
		return errors.New("external binding revision must be positive")
	}
	metadata, err := normalizeJSON(record.Metadata)
	if err != nil {
		return fmt.Errorf("external binding metadata: %w", err)
	}
	record.Metadata = metadata
	record.CreatedAt = recordTime(record.CreatedAt, tx.now)
	if err := insertOne(ctx, tx.tx, `
INSERT INTO external_bindings(
    external_binding_id, workspace_id, subject_ref, provider_kind,
    stable_identity, revision, binding_digest, credential_ref,
    rights_evidence_ref, metadata_json, created_at_ns
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT DO NOTHING`,
		record.ID, record.WorkspaceID, record.SubjectRef, record.ProviderKind,
		record.StableIdentity, record.Revision, record.BindingDigest,
		record.CredentialRef, record.RightsEvidenceRef, string(metadata),
		record.CreatedAt.UnixNano()); err != nil {
		return fmt.Errorf("insert external binding: %w", err)
	}
	return nil
}

func (s *Store) InsertExternalBinding(ctx context.Context, record *ExternalBinding) error {
	return s.Update(ctx, func(tx *Tx) error { return tx.InsertExternalBinding(ctx, record) })
}

func (tx *Tx) InsertSourceBinding(ctx context.Context, record *SourceBinding) error {
	return tx.InsertExternalBinding(ctx, record)
}

func (s *Store) InsertSourceBinding(ctx context.Context, record *SourceBinding) error {
	return s.InsertExternalBinding(ctx, record)
}

func (s *Store) GetExternalBinding(ctx context.Context, workspaceID, bindingID string) (ExternalBinding, error) {
	if err := requireID("workspace id", workspaceID); err != nil {
		return ExternalBinding{}, err
	}
	if err := requireID("external binding id", bindingID); err != nil {
		return ExternalBinding{}, err
	}
	return scanExternalBinding(s.db.QueryRowContext(ctx, externalBindingSelect+`
WHERE workspace_id = ? AND external_binding_id = ?`, workspaceID, bindingID))
}

func (s *Store) GetSourceBinding(ctx context.Context, workspaceID, bindingID string) (SourceBinding, error) {
	return s.GetExternalBinding(ctx, workspaceID, bindingID)
}

func (s *Store) ListExternalBindings(ctx context.Context, workspaceID, subjectRef string) ([]ExternalBinding, error) {
	if err := requireID("workspace id", workspaceID); err != nil {
		return nil, err
	}
	if err := optionalID("subject ref", subjectRef); err != nil {
		return nil, err
	}
	query := externalBindingSelect + `WHERE workspace_id = ?`
	args := []any{workspaceID}
	if subjectRef != "" {
		query += ` AND subject_ref = ?`
		args = append(args, subjectRef)
	}
	query += ` ORDER BY subject_ref, provider_kind, revision, external_binding_id`
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list external bindings: %w", err)
	}
	defer rows.Close()
	var records []ExternalBinding
	for rows.Next() {
		record, err := scanExternalBinding(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate external bindings: %w", err)
	}
	return records, nil
}

const externalBindingSelect = `
SELECT external_binding_id, workspace_id, subject_ref, provider_kind,
       stable_identity, revision, binding_digest, credential_ref,
       rights_evidence_ref, metadata_json, created_at_ns
FROM external_bindings `

func scanExternalBinding(scanner rowScanner) (ExternalBinding, error) {
	var record ExternalBinding
	var metadata string
	var created int64
	if err := scanner.Scan(
		&record.ID, &record.WorkspaceID, &record.SubjectRef, &record.ProviderKind,
		&record.StableIdentity, &record.Revision, &record.BindingDigest,
		&record.CredentialRef, &record.RightsEvidenceRef, &metadata, &created,
	); err != nil {
		return record, rowError("external binding", err)
	}
	record.Metadata = json.RawMessage(metadata)
	record.CreatedAt = time.Unix(0, created).UTC()
	return record, nil
}

// InsertExternalLocator adds one immutable locator to a binding.
func (tx *Tx) InsertExternalLocator(ctx context.Context, record *ExternalLocator) error {
	if record == nil {
		return errors.New("external locator is required")
	}
	for name, value := range map[string]string{
		"external locator id": record.ID,
		"workspace id":        record.WorkspaceID,
		"binding id":          record.BindingID,
		"locator kind":        record.Kind,
		"locator":             record.Locator,
	} {
		if err := requireText(name, value); err != nil {
			return err
		}
	}
	if err := requireID("external locator id", record.ID); err != nil {
		return err
	}
	if err := requireID("workspace id", record.WorkspaceID); err != nil {
		return err
	}
	if err := requireID("binding id", record.BindingID); err != nil {
		return err
	}
	if record.Priority < 0 {
		return errors.New("external locator priority cannot be negative")
	}
	if err := validateExpectedLength("external locator expected logical length", record.ExpectedLogicalLength); err != nil {
		return err
	}
	if record.Revision == 0 {
		record.Revision = 1
	}
	if record.Revision < 1 {
		return errors.New("external locator revision must be positive")
	}
	if strings.TrimSpace(record.DisplayLocator) == "" {
		record.DisplayLocator = record.Locator
	}
	if strings.TrimSpace(record.Availability) == "" {
		record.Availability = "UNKNOWN"
	}
	if strings.TrimSpace(record.ValidationStatus) == "" {
		record.ValidationStatus = "UNVALIDATED"
	}
	metadata, err := normalizeJSON(record.Metadata)
	if err != nil {
		return fmt.Errorf("external locator metadata: %w", err)
	}
	record.Metadata = metadata
	record.CreatedAt = recordTime(record.CreatedAt, tx.now)
	if err := insertOne(ctx, tx.tx, `
INSERT INTO external_locators(
    external_locator_id, workspace_id, binding_id, revision, priority,
    kind, locator, display_locator, expected_content_id,
    expected_logical_length, credential_ref, rights_evidence_ref,
    availability, validation_status, expires_at_ns, last_validated_at_ns,
    validation_ref, metadata_json, created_at_ns
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT DO NOTHING`,
		record.ID, record.WorkspaceID, record.BindingID, record.Revision,
		record.Priority, record.Kind, record.Locator, record.DisplayLocator,
		nullableString(record.ExpectedContentID), nullableInt64(record.ExpectedLogicalLength),
		record.CredentialRef, record.RightsEvidenceRef, record.Availability,
		record.ValidationStatus, nullableTime(record.ExpiresAt),
		nullableTime(record.LastValidatedAt), record.ValidationRef, string(metadata),
		record.CreatedAt.UnixNano()); err != nil {
		return fmt.Errorf("insert external locator: %w", err)
	}
	return nil
}

func (s *Store) InsertExternalLocator(ctx context.Context, record *ExternalLocator) error {
	return s.Update(ctx, func(tx *Tx) error { return tx.InsertExternalLocator(ctx, record) })
}

func (s *Store) GetExternalLocator(ctx context.Context, workspaceID, locatorID string) (ExternalLocator, error) {
	if err := requireID("workspace id", workspaceID); err != nil {
		return ExternalLocator{}, err
	}
	if err := requireID("external locator id", locatorID); err != nil {
		return ExternalLocator{}, err
	}
	return scanExternalLocator(s.db.QueryRowContext(ctx, externalLocatorSelect+`
WHERE workspace_id = ? AND external_locator_id = ?`, workspaceID, locatorID))
}

func (s *Store) ListExternalLocators(ctx context.Context, workspaceID, bindingID string) ([]ExternalLocator, error) {
	if err := requireID("workspace id", workspaceID); err != nil {
		return nil, err
	}
	if err := requireID("binding id", bindingID); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, externalLocatorSelect+`
WHERE workspace_id = ? AND binding_id = ? ORDER BY priority, revision, external_locator_id`, workspaceID, bindingID)
	if err != nil {
		return nil, fmt.Errorf("list external locators: %w", err)
	}
	defer rows.Close()
	var records []ExternalLocator
	for rows.Next() {
		record, err := scanExternalLocator(rows)
		if err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate external locators: %w", err)
	}
	return records, nil
}

func (s *Store) ListExternalLocatorsByBinding(ctx context.Context, workspaceID, bindingID string) ([]ExternalLocator, error) {
	return s.ListExternalLocators(ctx, workspaceID, bindingID)
}

const externalLocatorSelect = `
SELECT external_locator_id, workspace_id, binding_id, revision, priority,
       kind, locator, display_locator, expected_content_id,
       expected_logical_length, credential_ref, rights_evidence_ref,
       availability, validation_status, expires_at_ns, last_validated_at_ns,
       validation_ref, metadata_json, created_at_ns
FROM external_locators `

func scanExternalLocator(scanner rowScanner) (ExternalLocator, error) {
	var record ExternalLocator
	var expectedContent sql.NullString
	var expectedLength, expires, validated sql.NullInt64
	var metadata string
	var created int64
	if err := scanner.Scan(
		&record.ID, &record.WorkspaceID, &record.BindingID, &record.Revision,
		&record.Priority, &record.Kind, &record.Locator, &record.DisplayLocator,
		&expectedContent, &expectedLength, &record.CredentialRef,
		&record.RightsEvidenceRef, &record.Availability, &record.ValidationStatus,
		&expires, &validated, &record.ValidationRef, &metadata, &created,
	); err != nil {
		return record, rowError("external locator", err)
	}
	if expectedContent.Valid {
		record.ExpectedContentID = expectedContent.String
	}
	record.ExpectedLogicalLength = int64Pointer(expectedLength)
	record.ExpiresAt = nullableTimePointer(expires)
	record.LastValidatedAt = nullableTimePointer(validated)
	record.Metadata = json.RawMessage(metadata)
	record.CreatedAt = time.Unix(0, created).UTC()
	return record, nil
}

func nullableTimePointer(value sql.NullInt64) *time.Time {
	if !value.Valid {
		return nil
	}
	t := time.Unix(0, value.Int64).UTC()
	return &t
}

func int64PtrValue(value int64) *int64 {
	copy := value
	return &copy
}
