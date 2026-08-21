package sqlite

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
)

type migration struct {
	version int
	name    string
	sql     string
}

var migrations = []migration{
	{
		version: 1,
		name:    "operational_catalog",
		sql: `
CREATE TABLE workspaces (
    workspace_id TEXT PRIMARY KEY,
    name TEXT NOT NULL,
    metadata_json TEXT NOT NULL CHECK (json_valid(metadata_json)),
    revision INTEGER NOT NULL CHECK (revision >= 1),
    created_at_ns INTEGER NOT NULL,
    updated_at_ns INTEGER NOT NULL
) STRICT;

CREATE TABLE sources (
    source_id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    stable_key TEXT NOT NULL,
    kind TEXT NOT NULL,
    locator TEXT NOT NULL,
    identity_fingerprint TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('ACTIVE', 'DECOMMISSIONED', 'LOST', 'QUARANTINED')),
    metadata_json TEXT NOT NULL CHECK (json_valid(metadata_json)),
    revision INTEGER NOT NULL CHECK (revision >= 1),
    created_at_ns INTEGER NOT NULL,
    updated_at_ns INTEGER NOT NULL,
    FOREIGN KEY (workspace_id) REFERENCES workspaces(workspace_id) ON DELETE RESTRICT,
    UNIQUE (workspace_id, stable_key),
    UNIQUE (workspace_id, source_id)
) STRICT;

CREATE TABLE scan_generations (
    scan_generation_id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    source_id TEXT NOT NULL,
    generation INTEGER NOT NULL CHECK (generation >= 1),
    parent_scan_generation_id TEXT,
    capture_set_id TEXT NOT NULL,
    capture_set_digest TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('RUNNING', 'COMPLETE', 'INCOMPLETE', 'FAILED', 'CANCELLED')),
    full_traversal INTEGER NOT NULL CHECK (full_traversal IN (0, 1)),
    summary_json TEXT NOT NULL CHECK (json_valid(summary_json)),
    started_at_ns INTEGER NOT NULL,
    finished_at_ns INTEGER,
    FOREIGN KEY (workspace_id, source_id) REFERENCES sources(workspace_id, source_id) ON DELETE RESTRICT,
    FOREIGN KEY (workspace_id, source_id, parent_scan_generation_id)
        REFERENCES scan_generations(workspace_id, source_id, scan_generation_id) ON DELETE RESTRICT,
    UNIQUE (workspace_id, source_id, generation),
    UNIQUE (workspace_id, scan_generation_id),
    UNIQUE (workspace_id, source_id, scan_generation_id),
    CHECK ((state = 'RUNNING' AND finished_at_ns IS NULL) OR (state <> 'RUNNING' AND finished_at_ns IS NOT NULL)),
    CHECK (state <> 'COMPLETE' OR full_traversal = 1)
) STRICT;

CREATE TABLE observations (
    observation_id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    source_id TEXT NOT NULL,
    scan_generation_id TEXT NOT NULL,
    path_key BLOB NOT NULL,
    raw_path BLOB NOT NULL,
    display_path TEXT NOT NULL,
    entry_type TEXT NOT NULL CHECK (entry_type IN ('REGULAR_FILE', 'DIRECTORY', 'SYMLINK', 'FIFO', 'SOCKET', 'DEVICE', 'SPECIAL')),
    content_id TEXT NOT NULL,
    file_version_id TEXT NOT NULL,
    stat_digest TEXT NOT NULL,
    logical_size INTEGER CHECK (logical_size IS NULL OR logical_size >= 0),
    allocated_size INTEGER CHECK (allocated_size IS NULL OR allocated_size >= 0),
    read_state TEXT NOT NULL,
    metadata_json TEXT NOT NULL CHECK (json_valid(metadata_json)),
    observed_at_ns INTEGER NOT NULL,
    FOREIGN KEY (workspace_id, source_id, scan_generation_id)
        REFERENCES scan_generations(workspace_id, source_id, scan_generation_id) ON DELETE RESTRICT,
    UNIQUE (workspace_id, observation_id),
    UNIQUE (workspace_id, scan_generation_id, observation_id),
    UNIQUE (scan_generation_id, path_key)
) STRICT;

CREATE TRIGGER observations_require_running_scan
BEFORE INSERT ON observations
WHEN (SELECT state FROM scan_generations WHERE scan_generation_id = NEW.scan_generation_id) <> 'RUNNING'
BEGIN
    SELECT RAISE(ABORT, 'observations require a running scan generation');
END;

CREATE TRIGGER observations_no_update
BEFORE UPDATE ON observations BEGIN
    SELECT RAISE(ABORT, 'observations are immutable');
END;

CREATE TRIGGER observations_no_delete
BEFORE DELETE ON observations BEGIN
    SELECT RAISE(ABORT, 'observations are immutable');
END;

CREATE TABLE detection_evidence (
    detection_evidence_id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    observation_id TEXT NOT NULL,
    detector_id TEXT NOT NULL,
    detector_digest TEXT NOT NULL,
    evidence_kind TEXT NOT NULL,
    candidate_format TEXT NOT NULL,
    candidate_mime TEXT NOT NULL,
    confidence REAL CHECK (confidence IS NULL OR (confidence >= 0.0 AND confidence <= 1.0)),
    execution_class TEXT NOT NULL CHECK (execution_class IN ('BYTE_DETERMINISTIC', 'SEEDED_STOCHASTIC', 'OPAQUE_NONDETERMINISTIC')),
    evidence_json TEXT NOT NULL CHECK (json_valid(evidence_json)),
    evidence_digest TEXT NOT NULL,
    sandbox_policy_hash TEXT NOT NULL,
    started_at_ns INTEGER NOT NULL,
    finished_at_ns INTEGER NOT NULL,
    FOREIGN KEY (workspace_id, observation_id) REFERENCES observations(workspace_id, observation_id) ON DELETE RESTRICT,
    UNIQUE (observation_id, evidence_digest),
    CHECK (finished_at_ns >= started_at_ns)
) STRICT;

CREATE TRIGGER detection_evidence_no_update
BEFORE UPDATE ON detection_evidence BEGIN
    SELECT RAISE(ABORT, 'detection evidence is immutable');
END;

CREATE TRIGGER detection_evidence_no_delete
BEFORE DELETE ON detection_evidence BEGIN
    SELECT RAISE(ABORT, 'detection evidence is immutable');
END;

CREATE TABLE plans (
    plan_id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    scan_generation_id TEXT,
    kind TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('DRAFT', 'READY', 'COMMITTED', 'SUPERSEDED', 'REJECTED')),
    policy_revision TEXT NOT NULL,
    plan_json TEXT NOT NULL CHECK (json_valid(plan_json)),
    plan_digest TEXT NOT NULL,
    created_at_ns INTEGER NOT NULL,
    FOREIGN KEY (workspace_id) REFERENCES workspaces(workspace_id) ON DELETE RESTRICT,
    FOREIGN KEY (workspace_id, scan_generation_id)
        REFERENCES scan_generations(workspace_id, scan_generation_id) ON DELETE RESTRICT,
    UNIQUE (workspace_id, plan_digest),
    UNIQUE (workspace_id, plan_id)
) STRICT;

CREATE TRIGGER plans_no_update
BEFORE UPDATE ON plans BEGIN
    SELECT RAISE(ABORT, 'plans are immutable; publish a new plan');
END;

CREATE TRIGGER plans_no_delete
BEFORE DELETE ON plans BEGIN
    SELECT RAISE(ABORT, 'plans are immutable');
END;

CREATE TABLE jobs (
    job_id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    plan_id TEXT,
    kind TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('QUEUED', 'RUNNING', 'WAITING_APPROVAL', 'SUCCEEDED', 'FAILED', 'CANCELLED', 'NEEDS_RECONCILIATION')),
    input_json TEXT NOT NULL CHECK (json_valid(input_json)),
    checkpoint_json TEXT NOT NULL CHECK (json_valid(checkpoint_json)),
    result_json TEXT NOT NULL CHECK (json_valid(result_json)),
    error_code TEXT NOT NULL,
    attempt INTEGER NOT NULL CHECK (attempt >= 0),
    max_attempts INTEGER NOT NULL CHECK (max_attempts >= 1),
    lease_owner TEXT NOT NULL,
    lease_token TEXT NOT NULL,
    fencing_token INTEGER NOT NULL CHECK (fencing_token >= 0),
    lease_until_ns INTEGER,
    cancellation_asked INTEGER NOT NULL CHECK (cancellation_asked IN (0, 1)),
    revision INTEGER NOT NULL CHECK (revision >= 1),
    created_at_ns INTEGER NOT NULL,
    updated_at_ns INTEGER NOT NULL,
    FOREIGN KEY (workspace_id) REFERENCES workspaces(workspace_id) ON DELETE RESTRICT,
    FOREIGN KEY (workspace_id, plan_id) REFERENCES plans(workspace_id, plan_id) ON DELETE RESTRICT,
    UNIQUE (workspace_id, job_id)
) STRICT;

CREATE INDEX jobs_claimable_idx ON jobs(workspace_id, state, lease_until_ns, created_at_ns);

CREATE TABLE audit_events (
    audit_sequence INTEGER PRIMARY KEY AUTOINCREMENT,
    audit_event_id TEXT NOT NULL UNIQUE,
    workspace_id TEXT NOT NULL,
    actor TEXT NOT NULL,
    action TEXT NOT NULL,
    target_type TEXT NOT NULL,
    target_id TEXT NOT NULL,
    request_id TEXT NOT NULL,
    attempt INTEGER NOT NULL CHECK (attempt >= 0),
    source TEXT NOT NULL,
    policy_ref TEXT NOT NULL,
    approval_ref TEXT NOT NULL,
    fencing_token INTEGER NOT NULL CHECK (fencing_token >= 0),
    outcome TEXT NOT NULL,
    details_json TEXT NOT NULL CHECK (json_valid(details_json)),
    previous_event_digest TEXT NOT NULL,
    event_digest TEXT NOT NULL UNIQUE,
    occurred_at_ns INTEGER NOT NULL,
    FOREIGN KEY (workspace_id) REFERENCES workspaces(workspace_id) ON DELETE RESTRICT
) STRICT;

CREATE INDEX audit_events_workspace_sequence_idx ON audit_events(workspace_id, audit_sequence);

CREATE TRIGGER audit_events_no_update
BEFORE UPDATE ON audit_events BEGIN
    SELECT RAISE(ABORT, 'audit events are append-only');
END;

CREATE TRIGGER audit_events_no_delete
BEFORE DELETE ON audit_events BEGIN
    SELECT RAISE(ABORT, 'audit events are append-only');
END;

CREATE TABLE idempotency_records (
    workspace_id TEXT NOT NULL,
    scope TEXT NOT NULL,
    idempotency_key TEXT NOT NULL,
    request_hash TEXT NOT NULL,
    resource_type TEXT NOT NULL,
    resource_id TEXT NOT NULL,
    response_json TEXT NOT NULL CHECK (json_valid(response_json)),
    created_at_ns INTEGER NOT NULL,
    PRIMARY KEY (workspace_id, scope, idempotency_key),
    FOREIGN KEY (workspace_id) REFERENCES workspaces(workspace_id) ON DELETE RESTRICT
) WITHOUT ROWID, STRICT;

PRAGMA user_version = 1;
`,
	},
	{
		version: 2,
		name:    "logical_snapshot_namespace",
		sql: `
CREATE TABLE namespace_roots (
    namespace_root_id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    source_id TEXT NOT NULL,
    scan_generation_id TEXT NOT NULL,
    snapshot_ref TEXT NOT NULL,
    name TEXT NOT NULL,
    root_path_key BLOB NOT NULL,
    filesystem_semantics TEXT NOT NULL,
    authority_digest TEXT NOT NULL,
    metadata_json TEXT NOT NULL CHECK (json_valid(metadata_json)),
    created_at_ns INTEGER NOT NULL,
    FOREIGN KEY (workspace_id, source_id, scan_generation_id)
        REFERENCES scan_generations(workspace_id, source_id, scan_generation_id) ON DELETE RESTRICT,
    UNIQUE (workspace_id, namespace_root_id),
    UNIQUE (scan_generation_id, root_path_key)
) STRICT;

CREATE TRIGGER namespace_roots_require_running_scan
BEFORE INSERT ON namespace_roots
WHEN (SELECT state FROM scan_generations WHERE scan_generation_id = NEW.scan_generation_id) <> 'RUNNING'
BEGIN
    SELECT RAISE(ABORT, 'namespace roots require a running scan generation');
END;

CREATE TRIGGER namespace_roots_no_update
BEFORE UPDATE ON namespace_roots BEGIN
    SELECT RAISE(ABORT, 'namespace roots are immutable');
END;

CREATE TRIGGER namespace_roots_no_delete
BEFORE DELETE ON namespace_roots BEGIN
    SELECT RAISE(ABORT, 'namespace roots are immutable');
END;

CREATE TABLE representations (
    representation_id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    content_id TEXT NOT NULL,
    decoded_length INTEGER NOT NULL CHECK (decoded_length >= 0),
    ownership_mode TEXT NOT NULL CHECK (ownership_mode IN ('RESTOREWEAVE_PACKS', 'ENGINE_MANAGED_OBJECTS', 'INLINE')),
    codec_profile_ref TEXT NOT NULL,
    access_mode TEXT NOT NULL CHECK (access_mode IN ('RANDOM_ACCESS_NATIVE', 'RANDOM_ACCESS_CHECKPOINTED', 'SEQUENTIAL_STREAM', 'WHOLE_OBJECT_ONLY')),
    minimum_readable_unit INTEGER NOT NULL CHECK (minimum_readable_unit >= 0),
    seek_checkpoint_interval INTEGER NOT NULL CHECK (seek_checkpoint_interval >= 0),
    whole_read_required_to_verify INTEGER NOT NULL CHECK (whole_read_required_to_verify IN (0, 1)),
    record_digest TEXT NOT NULL,
    metadata_json TEXT NOT NULL CHECK (json_valid(metadata_json)),
    created_at_ns INTEGER NOT NULL,
    FOREIGN KEY (workspace_id) REFERENCES workspaces(workspace_id) ON DELETE RESTRICT,
    UNIQUE (workspace_id, representation_id)
) STRICT;

CREATE TRIGGER representations_no_update
BEFORE UPDATE ON representations BEGIN
    SELECT RAISE(ABORT, 'representations are immutable');
END;

CREATE TRIGGER representations_no_delete
BEFORE DELETE ON representations BEGIN
    SELECT RAISE(ABORT, 'representations are immutable');
END;

CREATE TABLE file_versions (
    file_version_id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    scan_generation_id TEXT NOT NULL,
    observation_id TEXT NOT NULL,
    asset_id TEXT NOT NULL,
    content_id TEXT NOT NULL,
    logical_size INTEGER NOT NULL CHECK (logical_size >= 0),
    hashing_profile TEXT NOT NULL,
    authoritative_representation_id TEXT NOT NULL,
    extent_set_digest TEXT NOT NULL,
    hardlink_group_id TEXT NOT NULL,
    sparse_evidence_json TEXT NOT NULL CHECK (json_valid(sparse_evidence_json)),
    verification_ref TEXT NOT NULL,
    record_digest TEXT NOT NULL,
    created_at_ns INTEGER NOT NULL,
    FOREIGN KEY (workspace_id, scan_generation_id)
        REFERENCES scan_generations(workspace_id, scan_generation_id) ON DELETE RESTRICT,
    FOREIGN KEY (workspace_id, scan_generation_id, observation_id)
        REFERENCES observations(workspace_id, scan_generation_id, observation_id) ON DELETE RESTRICT,
    FOREIGN KEY (workspace_id, authoritative_representation_id)
        REFERENCES representations(workspace_id, representation_id) ON DELETE RESTRICT,
    UNIQUE (workspace_id, file_version_id)
) STRICT;

CREATE TRIGGER file_versions_no_update
BEFORE UPDATE ON file_versions BEGIN
    SELECT RAISE(ABORT, 'file versions are immutable');
END;

CREATE TRIGGER file_versions_no_delete
BEFORE DELETE ON file_versions BEGIN
    SELECT RAISE(ABORT, 'file versions are immutable');
END;

CREATE TABLE namespace_entries (
    namespace_entry_id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    namespace_root_id TEXT NOT NULL,
    parent_entry_id TEXT,
    observation_id TEXT,
    raw_name BLOB NOT NULL,
    display_name TEXT NOT NULL,
    full_path_key BLOB NOT NULL,
    entry_type TEXT NOT NULL CHECK (entry_type IN ('REGULAR_FILE', 'DIRECTORY', 'SYMLINK', 'FIFO', 'SOCKET', 'DEVICE', 'SPECIAL')),
    metadata_json TEXT NOT NULL CHECK (json_valid(metadata_json)),
    content_id TEXT NOT NULL,
    file_version_id TEXT,
    symlink_target_raw BLOB,
    symlink_target_display TEXT NOT NULL,
    hardlink_group_id TEXT NOT NULL,
    logical_size INTEGER CHECK (logical_size IS NULL OR logical_size >= 0),
    allocated_size INTEGER CHECK (allocated_size IS NULL OR allocated_size >= 0),
    created_at_ns INTEGER NOT NULL,
    FOREIGN KEY (workspace_id, namespace_root_id)
        REFERENCES namespace_roots(workspace_id, namespace_root_id) ON DELETE RESTRICT,
    FOREIGN KEY (workspace_id, namespace_root_id, parent_entry_id)
        REFERENCES namespace_entries(workspace_id, namespace_root_id, namespace_entry_id) ON DELETE RESTRICT,
    FOREIGN KEY (workspace_id, observation_id)
        REFERENCES observations(workspace_id, observation_id) ON DELETE RESTRICT,
    FOREIGN KEY (workspace_id, file_version_id)
        REFERENCES file_versions(workspace_id, file_version_id) ON DELETE RESTRICT,
    UNIQUE (workspace_id, namespace_entry_id),
    UNIQUE (workspace_id, namespace_root_id, namespace_entry_id),
    UNIQUE (namespace_root_id, full_path_key),
    CHECK (parent_entry_id IS NULL OR parent_entry_id <> namespace_entry_id),
    CHECK ((entry_type = 'SYMLINK' AND symlink_target_raw IS NOT NULL) OR
           (entry_type <> 'SYMLINK' AND symlink_target_raw IS NULL)),
    CHECK ((entry_type = 'REGULAR_FILE' AND file_version_id IS NOT NULL) OR
           (entry_type <> 'REGULAR_FILE' AND file_version_id IS NULL))
) STRICT;

CREATE INDEX namespace_entries_parent_idx
    ON namespace_entries(workspace_id, namespace_root_id, parent_entry_id, raw_name);

CREATE TRIGGER namespace_entries_parent_is_directory
BEFORE INSERT ON namespace_entries
WHEN NEW.parent_entry_id IS NOT NULL AND
     (SELECT entry_type FROM namespace_entries
      WHERE workspace_id = NEW.workspace_id
        AND namespace_root_id = NEW.namespace_root_id
        AND namespace_entry_id = NEW.parent_entry_id) <> 'DIRECTORY'
BEGIN
    SELECT RAISE(ABORT, 'namespace parent must be a directory');
END;

CREATE TRIGGER namespace_entries_no_update
BEFORE UPDATE ON namespace_entries BEGIN
    SELECT RAISE(ABORT, 'namespace entries are immutable');
END;

CREATE TRIGGER namespace_entries_no_delete
BEFORE DELETE ON namespace_entries BEGIN
    SELECT RAISE(ABORT, 'namespace entries are immutable');
END;

CREATE TABLE content_extents (
    content_extent_id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    file_version_id TEXT NOT NULL,
    ordinal INTEGER NOT NULL CHECK (ordinal >= 0),
    logical_offset INTEGER NOT NULL CHECK (logical_offset >= 0),
    logical_length INTEGER NOT NULL CHECK (logical_length > 0),
    extent_kind TEXT NOT NULL CHECK (extent_kind IN ('DATA', 'HOLE')),
    representation_id TEXT,
    representation_offset INTEGER NOT NULL CHECK (representation_offset >= 0),
    extent_digest TEXT NOT NULL,
    metadata_json TEXT NOT NULL CHECK (json_valid(metadata_json)),
    created_at_ns INTEGER NOT NULL,
    FOREIGN KEY (workspace_id, file_version_id)
        REFERENCES file_versions(workspace_id, file_version_id) ON DELETE RESTRICT,
    FOREIGN KEY (workspace_id, representation_id)
        REFERENCES representations(workspace_id, representation_id) ON DELETE RESTRICT,
    UNIQUE (file_version_id, ordinal),
    CHECK ((extent_kind = 'DATA' AND representation_id IS NOT NULL) OR
           (extent_kind = 'HOLE' AND representation_id IS NULL AND representation_offset = 0))
) STRICT;

CREATE INDEX content_extents_version_offset_idx
    ON content_extents(workspace_id, file_version_id, logical_offset);

CREATE TRIGGER content_extents_no_update
BEFORE UPDATE ON content_extents BEGIN
    SELECT RAISE(ABORT, 'content extents are immutable');
END;

CREATE TRIGGER content_extents_no_delete
BEFORE DELETE ON content_extents BEGIN
    SELECT RAISE(ABORT, 'content extents are immutable');
END;

-- Native pack/object coordinates are rebuildable adapter observations. This
-- projection is never logical identity or sole recovery authority.
CREATE TABLE physical_locator_projections (
    physical_locator_id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    representation_id TEXT NOT NULL,
    content_id TEXT NOT NULL,
    ownership_mode TEXT NOT NULL CHECK (ownership_mode IN ('RESTOREWEAVE_PACKS', 'INLINE')),
    locator_kind TEXT NOT NULL CHECK (locator_kind IN ('PACK_RANGE', 'OBJECT', 'INLINE')),
    backend_id TEXT NOT NULL,
    repository_id TEXT NOT NULL,
    placement_generation INTEGER NOT NULL CHECK (placement_generation >= 0),
    container_ref TEXT NOT NULL,
    byte_offset INTEGER,
    byte_length INTEGER,
    encoded_length INTEGER,
    encoded_digest TEXT NOT NULL,
    authority_ref TEXT NOT NULL,
    reader_profile_ref TEXT NOT NULL,
    locator_json TEXT NOT NULL CHECK (json_valid(locator_json)),
    created_at_ns INTEGER NOT NULL,
    FOREIGN KEY (workspace_id, representation_id)
        REFERENCES representations(workspace_id, representation_id) ON DELETE RESTRICT,
    UNIQUE (workspace_id, physical_locator_id),
    CHECK ((byte_offset IS NULL AND byte_length IS NULL) OR
           (byte_offset >= 0 AND byte_length > 0)),
    CHECK (encoded_length IS NULL OR encoded_length >= 0)
) STRICT;

CREATE INDEX physical_locators_representation_idx
    ON physical_locator_projections(workspace_id, representation_id, placement_generation);
CREATE INDEX physical_locators_container_range_idx
    ON physical_locator_projections(workspace_id, repository_id, container_ref, byte_offset);

CREATE TRIGGER physical_locators_no_update
BEFORE UPDATE ON physical_locator_projections BEGIN
    SELECT RAISE(ABORT, 'physical locators are immutable placement projections');
END;

CREATE TRIGGER physical_locators_no_delete
BEFORE DELETE ON physical_locator_projections BEGIN
    SELECT RAISE(ABORT, 'physical locators are immutable placement projections');
END;

-- Engine-managed repositories remain opaque. This projection resolves a
-- representation through a signed engine receipt and source-relative path,
-- never through Restic-private pack, chunk, index, or blob coordinates.
CREATE TABLE engine_read_refs (
    engine_read_ref_id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    representation_id TEXT NOT NULL,
    repository_id TEXT NOT NULL,
    engine_snapshot_ref TEXT NOT NULL,
    engine_receipt_ref TEXT NOT NULL,
    engine_path_key BLOB NOT NULL,
    placement_checkpoint_id TEXT NOT NULL,
    placement_checkpoint_digest TEXT NOT NULL,
    reader_profile_ref TEXT NOT NULL,
    metadata_json TEXT NOT NULL CHECK (json_valid(metadata_json)),
    created_at_ns INTEGER NOT NULL,
    FOREIGN KEY (workspace_id, representation_id)
        REFERENCES representations(workspace_id, representation_id) ON DELETE RESTRICT,
    UNIQUE (workspace_id, engine_read_ref_id),
    UNIQUE (workspace_id, representation_id, repository_id, engine_snapshot_ref, engine_path_key)
) STRICT;

CREATE INDEX engine_read_refs_representation_idx
    ON engine_read_refs(workspace_id, representation_id, repository_id);

CREATE TRIGGER engine_read_refs_no_update
BEFORE UPDATE ON engine_read_refs BEGIN
    SELECT RAISE(ABORT, 'engine read refs are immutable projections');
END;

CREATE TRIGGER engine_read_refs_no_delete
BEFORE DELETE ON engine_read_refs BEGIN
    SELECT RAISE(ABORT, 'engine read refs are immutable projections');
END;

PRAGMA user_version = 2;
`,
	},
	{
		version: 3,
		name:    "capture_bindings_and_publications",
		sql: `
CREATE TABLE capture_root_bindings (
    binding_id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    source_id TEXT NOT NULL,
    scan_generation_id TEXT NOT NULL,
    capture_mode TEXT NOT NULL CHECK (capture_mode = 'ROOTED_FD'),
    profile TEXT NOT NULL,
    display_path TEXT NOT NULL,
    device_id INTEGER NOT NULL CHECK (device_id >= 0),
    inode INTEGER NOT NULL CHECK (inode >= 0),
    consistency_claim TEXT NOT NULL,
    identity_digest TEXT NOT NULL,
    bound_at_ns INTEGER NOT NULL,
    record_json TEXT NOT NULL CHECK (json_valid(record_json)),
    FOREIGN KEY (workspace_id, source_id, scan_generation_id)
        REFERENCES scan_generations(workspace_id, source_id, scan_generation_id) ON DELETE RESTRICT,
    UNIQUE (workspace_id, binding_id),
    UNIQUE (workspace_id, scan_generation_id)
) STRICT;

CREATE TRIGGER capture_root_bindings_no_update
BEFORE UPDATE ON capture_root_bindings BEGIN
    SELECT RAISE(ABORT, 'capture root bindings are immutable');
END;

CREATE TRIGGER capture_root_bindings_no_delete
BEFORE DELETE ON capture_root_bindings BEGIN
    SELECT RAISE(ABORT, 'capture root bindings are immutable');
END;

CREATE TABLE publications (
    publication_id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    snapshot_ref TEXT NOT NULL,
    scan_generation_id TEXT NOT NULL,
    binding_id TEXT NOT NULL,
    namespace_root_id TEXT NOT NULL,
    manifest_digest TEXT NOT NULL,
    committed_at_ns INTEGER NOT NULL,
    metadata_json TEXT NOT NULL CHECK (json_valid(metadata_json)),
    FOREIGN KEY (workspace_id) REFERENCES workspaces(workspace_id) ON DELETE RESTRICT,
    FOREIGN KEY (workspace_id, scan_generation_id)
        REFERENCES scan_generations(workspace_id, scan_generation_id) ON DELETE RESTRICT,
    FOREIGN KEY (workspace_id, binding_id)
        REFERENCES capture_root_bindings(workspace_id, binding_id) ON DELETE RESTRICT,
    FOREIGN KEY (workspace_id, namespace_root_id)
        REFERENCES namespace_roots(workspace_id, namespace_root_id) ON DELETE RESTRICT,
    UNIQUE (workspace_id, publication_id),
    UNIQUE (workspace_id, snapshot_ref)
) STRICT;

CREATE TRIGGER publications_no_update
BEFORE UPDATE ON publications BEGIN
    SELECT RAISE(ABORT, 'publications are immutable');
END;

CREATE TRIGGER publications_no_delete
BEFORE DELETE ON publications BEGIN
    SELECT RAISE(ABORT, 'publications are immutable');
END;

PRAGMA user_version = 3;
`,
	},
	{
		version: 4,
		name:    "annotations_and_index_generations",
		sql: `
CREATE TABLE annotations (
    annotation_id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    subject_ref TEXT NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN ('TAG', 'NOTE')),
    body TEXT NOT NULL,
    body_digest TEXT NOT NULL,
    revision INTEGER NOT NULL CHECK (revision >= 1),
    predecessor_revision INTEGER NOT NULL CHECK (predecessor_revision >= 0),
    tombstoned INTEGER NOT NULL CHECK (tombstoned IN (0, 1)),
    created_at_ns INTEGER NOT NULL,
    updated_at_ns INTEGER NOT NULL,
    FOREIGN KEY (workspace_id) REFERENCES workspaces(workspace_id) ON DELETE RESTRICT,
    UNIQUE (workspace_id, annotation_id)
) STRICT;

CREATE UNIQUE INDEX annotations_live_tag_idx
    ON annotations(workspace_id, subject_ref, kind, body)
    WHERE tombstoned = 0 AND kind = 'TAG';

CREATE INDEX annotations_subject_idx
    ON annotations(workspace_id, subject_ref, kind, tombstoned);

CREATE TABLE index_generations (
    generation_id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    snapshot_ref TEXT NOT NULL,
    namespace_root_id TEXT NOT NULL,
    db_path TEXT NOT NULL,
    created_at_ns INTEGER NOT NULL,
    FOREIGN KEY (workspace_id) REFERENCES workspaces(workspace_id) ON DELETE RESTRICT,
    UNIQUE (workspace_id, generation_id)
) STRICT;

PRAGMA user_version = 4;
`,
	},
	{
		version: 5,
		name:    "processor_artifacts",
		sql: `
CREATE TABLE processor_artifacts (
    artifact_id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    subject_ref TEXT NOT NULL,
    snapshot_ref TEXT NOT NULL,
    route_digest TEXT NOT NULL,
    stage TEXT NOT NULL CHECK (stage IN (
        'CLASSIFY_LEARNED', 'PARSE', 'EXTRACT', 'ENRICH',
        'FINGERPRINT', 'TRANSFORM', 'VALIDATE', 'INDEX_PREPARE'
    )),
    capability_id TEXT NOT NULL,
    schema_ref TEXT NOT NULL,
    state TEXT NOT NULL CHECK (state IN ('POLICY_ADMITTED', 'REJECTED')),
    authority_class TEXT NOT NULL,
    lifecycle_class TEXT NOT NULL,
    media_type TEXT NOT NULL,
    byte_length INTEGER NOT NULL CHECK (byte_length >= 0),
    digest TEXT NOT NULL,
    body TEXT NOT NULL,
    attempt_id TEXT NOT NULL,
    fence_token INTEGER NOT NULL CHECK (fence_token >= 1),
    producer_digest TEXT NOT NULL,
    envelope_json TEXT NOT NULL CHECK (json_valid(envelope_json)),
    created_at_ns INTEGER NOT NULL,
    updated_at_ns INTEGER NOT NULL,
    FOREIGN KEY (workspace_id) REFERENCES workspaces(workspace_id) ON DELETE RESTRICT,
    UNIQUE (workspace_id, artifact_id)
) STRICT;

CREATE INDEX processor_artifacts_subject_idx
    ON processor_artifacts(workspace_id, snapshot_ref, subject_ref, state);

CREATE UNIQUE INDEX processor_artifacts_live_idx
    ON processor_artifacts(workspace_id, snapshot_ref, subject_ref, stage, capability_id)
    WHERE state = 'POLICY_ADMITTED';

PRAGMA user_version = 5;
`,
	},
	{
		version: 6,
		name:    "annotation_progress",
		sql: `
CREATE TABLE annotations_v6 (
    annotation_id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    subject_ref TEXT NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN ('TAG', 'NOTE', 'PROGRESS')),
    body TEXT NOT NULL,
    body_digest TEXT NOT NULL,
    revision INTEGER NOT NULL CHECK (revision >= 1),
    predecessor_revision INTEGER NOT NULL CHECK (predecessor_revision >= 0),
    tombstoned INTEGER NOT NULL CHECK (tombstoned IN (0, 1)),
    created_at_ns INTEGER NOT NULL,
    updated_at_ns INTEGER NOT NULL,
    FOREIGN KEY (workspace_id) REFERENCES workspaces(workspace_id) ON DELETE RESTRICT,
    UNIQUE (workspace_id, annotation_id)
) STRICT;

INSERT INTO annotations_v6 SELECT * FROM annotations;
DROP TABLE annotations;
ALTER TABLE annotations_v6 RENAME TO annotations;

CREATE UNIQUE INDEX annotations_live_tag_idx
    ON annotations(workspace_id, subject_ref, kind, body)
    WHERE tombstoned = 0 AND kind = 'TAG';

CREATE UNIQUE INDEX annotations_live_progress_idx
    ON annotations(workspace_id, subject_ref)
    WHERE tombstoned = 0 AND kind = 'PROGRESS';

CREATE INDEX annotations_subject_idx
    ON annotations(workspace_id, subject_ref, kind, tombstoned);

PRAGMA user_version = 6;
`,
	},
	{
		version: 7,
		name:    "index_generation_dimension",
		sql: `
ALTER TABLE index_generations ADD COLUMN dimension TEXT NOT NULL DEFAULT 'lexical-metadata-fts';

CREATE INDEX index_generations_dimension_idx
    ON index_generations(workspace_id, dimension, created_at_ns);

PRAGMA user_version = 7;
`,
	},
	{
		version: 8,
		name:    "protection_recovery_external_bindings",
		sql: `
CREATE TABLE protection_records (
    protection_record_id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    subject_ref TEXT NOT NULL,
    mode TEXT NOT NULL CHECK (mode IN (
        'STORE_EXACT', 'STORE_EXACT_WITH_EXTERNAL_FALLBACK',
        'LINK_ONLY', 'METADATA_ONLY'
    )),
    outcome TEXT NOT NULL CHECK (outcome IN (
        'EXACT_PROTECTED', 'EXTERNAL_REPLAYABLE',
        'LINK_ONLY_UNPROTECTED', 'UNAVAILABLE'
    )),
    expected_content_id TEXT,
    expected_logical_length INTEGER CHECK (
        expected_logical_length IS NULL OR expected_logical_length >= 0
    ),
    local_representation_id TEXT,
    policy_decision_ref TEXT NOT NULL,
    last_verification_ref TEXT NOT NULL,
    last_verified_at_ns INTEGER,
    revision INTEGER NOT NULL CHECK (revision >= 1),
    metadata_json TEXT NOT NULL CHECK (json_valid(metadata_json)),
    created_at_ns INTEGER NOT NULL,
    updated_at_ns INTEGER NOT NULL,
    FOREIGN KEY (workspace_id) REFERENCES workspaces(workspace_id) ON DELETE RESTRICT,
    FOREIGN KEY (workspace_id, local_representation_id)
        REFERENCES representations(workspace_id, representation_id) ON DELETE RESTRICT,
    UNIQUE (workspace_id, protection_record_id),
    UNIQUE (workspace_id, subject_ref)
) STRICT;

CREATE INDEX protection_records_outcome_idx
    ON protection_records(workspace_id, outcome, subject_ref);

CREATE TABLE external_bindings (
    external_binding_id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    subject_ref TEXT NOT NULL,
    provider_kind TEXT NOT NULL,
    stable_identity TEXT NOT NULL,
    revision INTEGER NOT NULL CHECK (revision >= 1),
    binding_digest TEXT NOT NULL,
    credential_ref TEXT NOT NULL,
    rights_evidence_ref TEXT NOT NULL,
    metadata_json TEXT NOT NULL CHECK (json_valid(metadata_json)),
    created_at_ns INTEGER NOT NULL,
    FOREIGN KEY (workspace_id) REFERENCES workspaces(workspace_id) ON DELETE RESTRICT,
    UNIQUE (workspace_id, external_binding_id),
    UNIQUE (workspace_id, subject_ref, stable_identity, revision)
) STRICT;

CREATE INDEX external_bindings_subject_idx
    ON external_bindings(workspace_id, subject_ref, provider_kind);

CREATE TABLE recovery_references (
    recovery_reference_id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    protection_record_id TEXT NOT NULL,
    subject_ref TEXT NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN (
        'EXACT_REPRESENTATION', 'EXACT_REVERSIBLE',
        'EXTERNAL_LOCATOR', 'USER_RECIPE'
    )),
    priority INTEGER NOT NULL CHECK (priority >= 0),
    claim TEXT NOT NULL CHECK (claim IN (
        'RESTORE_VERIFIED', 'EXTERNAL_REPLAYABLE',
        'LINK_ONLY_UNPROTECTED', 'UNAVAILABLE'
    )),
    expected_content_id TEXT,
    expected_logical_length INTEGER CHECK (
        expected_logical_length IS NULL OR expected_logical_length >= 0
    ),
    representation_id TEXT,
    external_binding_id TEXT,
    codec_profile_ref TEXT NOT NULL,
    recipe_json TEXT NOT NULL CHECK (json_valid(recipe_json)),
    verification_json TEXT NOT NULL CHECK (json_valid(verification_json)),
    status TEXT NOT NULL,
    last_validated_at_ns INTEGER,
    expires_at_ns INTEGER,
    policy_ref TEXT NOT NULL,
    rights_evidence_ref TEXT NOT NULL,
    credential_ref TEXT NOT NULL,
    operator_decision_ref TEXT NOT NULL,
    record_digest TEXT NOT NULL,
    metadata_json TEXT NOT NULL CHECK (json_valid(metadata_json)),
    created_at_ns INTEGER NOT NULL,
    updated_at_ns INTEGER NOT NULL,
    FOREIGN KEY (workspace_id, protection_record_id)
        REFERENCES protection_records(workspace_id, protection_record_id) ON DELETE RESTRICT,
    FOREIGN KEY (workspace_id, representation_id)
        REFERENCES representations(workspace_id, representation_id) ON DELETE RESTRICT,
    FOREIGN KEY (workspace_id, external_binding_id)
        REFERENCES external_bindings(workspace_id, external_binding_id) ON DELETE RESTRICT,
    UNIQUE (workspace_id, recovery_reference_id),
    UNIQUE (workspace_id, protection_record_id, priority, recovery_reference_id),
    CHECK (
        (kind IN ('EXACT_REPRESENTATION', 'EXACT_REVERSIBLE') AND representation_id IS NOT NULL)
        OR (kind = 'EXTERNAL_LOCATOR' AND external_binding_id IS NOT NULL)
        OR (kind = 'USER_RECIPE')
    )
) STRICT;

CREATE INDEX recovery_references_subject_idx
    ON recovery_references(workspace_id, subject_ref, priority, recovery_reference_id);

CREATE INDEX recovery_references_binding_idx
    ON recovery_references(workspace_id, external_binding_id, priority);

CREATE TRIGGER recovery_references_no_update
BEFORE UPDATE ON recovery_references BEGIN
    SELECT RAISE(ABORT, 'recovery references are immutable; publish a new revision');
END;

CREATE TRIGGER recovery_references_no_delete
BEFORE DELETE ON recovery_references BEGIN
    SELECT RAISE(ABORT, 'recovery references are immutable');
END;

CREATE TABLE external_locators (
    external_locator_id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    binding_id TEXT NOT NULL,
    revision INTEGER NOT NULL CHECK (revision >= 1),
    priority INTEGER NOT NULL CHECK (priority >= 0),
    kind TEXT NOT NULL,
    locator TEXT NOT NULL,
    display_locator TEXT NOT NULL,
    expected_content_id TEXT,
    expected_logical_length INTEGER CHECK (
        expected_logical_length IS NULL OR expected_logical_length >= 0
    ),
    credential_ref TEXT NOT NULL,
    rights_evidence_ref TEXT NOT NULL,
    availability TEXT NOT NULL,
    validation_status TEXT NOT NULL,
    expires_at_ns INTEGER,
    last_validated_at_ns INTEGER,
    validation_ref TEXT NOT NULL,
    metadata_json TEXT NOT NULL CHECK (json_valid(metadata_json)),
    created_at_ns INTEGER NOT NULL,
    FOREIGN KEY (workspace_id, binding_id)
        REFERENCES external_bindings(workspace_id, external_binding_id) ON DELETE RESTRICT,
    UNIQUE (workspace_id, external_locator_id),
    UNIQUE (workspace_id, binding_id, revision, priority, external_locator_id)
) STRICT;

CREATE INDEX external_locators_binding_idx
    ON external_locators(workspace_id, binding_id, priority, external_locator_id);

CREATE TRIGGER external_bindings_no_update
BEFORE UPDATE ON external_bindings BEGIN
    SELECT RAISE(ABORT, 'external bindings are immutable; publish a new revision');
END;

CREATE TRIGGER external_bindings_no_delete
BEFORE DELETE ON external_bindings BEGIN
    SELECT RAISE(ABORT, 'external bindings are immutable');
END;

CREATE TRIGGER external_locators_no_update
BEFORE UPDATE ON external_locators BEGIN
    SELECT RAISE(ABORT, 'external locators are immutable; publish a new revision');
END;

CREATE TRIGGER external_locators_no_delete
BEFORE DELETE ON external_locators BEGIN
    SELECT RAISE(ABORT, 'external locators are immutable');
END;

PRAGMA user_version = 8;
`,
	},
	{
		version: 9,
		name:    "metadata_facts_and_description_segments",
		sql: `
CREATE TABLE metadata_facts (
    metadata_fact_id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    subject_ref TEXT NOT NULL,
    namespace TEXT NOT NULL,
    fact_key TEXT NOT NULL,
    value_json TEXT NOT NULL CHECK (json_valid(value_json)),
    value_type TEXT NOT NULL,
    authority_class TEXT NOT NULL,
    source_ref TEXT NOT NULL,
    confidence REAL CHECK (confidence IS NULL OR (confidence >= 0.0 AND confidence <= 1.0)),
    revision INTEGER NOT NULL CHECK (revision >= 1),
    created_at_ns INTEGER NOT NULL,
    FOREIGN KEY (workspace_id) REFERENCES workspaces(workspace_id) ON DELETE RESTRICT,
    UNIQUE (workspace_id, metadata_fact_id),
    UNIQUE (workspace_id, subject_ref, namespace, fact_key, revision)
) STRICT;

CREATE INDEX metadata_facts_subject_idx
    ON metadata_facts(workspace_id, subject_ref, namespace, fact_key, revision);

CREATE TABLE description_documents (
    description_document_id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    subject_ref TEXT NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN (
        'USER', 'IMPORTED', 'EXTRACTED', 'AI_SUMMARY', 'AI_ANALYSIS'
    )),
    title TEXT NOT NULL,
    language TEXT NOT NULL,
    body TEXT NOT NULL,
    body_digest TEXT NOT NULL,
    source_ref TEXT NOT NULL,
    producer_profile TEXT NOT NULL,
    confidence REAL CHECK (confidence IS NULL OR (confidence >= 0.0 AND confidence <= 1.0)),
    coverage REAL CHECK (coverage IS NULL OR (coverage >= 0.0 AND coverage <= 1.0)),
    visibility TEXT NOT NULL,
    accepted INTEGER NOT NULL CHECK (accepted IN (0, 1)),
    revision INTEGER NOT NULL CHECK (revision >= 1),
    predecessor_id TEXT,
    metadata_json TEXT NOT NULL CHECK (json_valid(metadata_json)),
    created_at_ns INTEGER NOT NULL,
    updated_at_ns INTEGER NOT NULL,
    FOREIGN KEY (workspace_id) REFERENCES workspaces(workspace_id) ON DELETE RESTRICT,
    FOREIGN KEY (workspace_id, predecessor_id)
        REFERENCES description_documents(workspace_id, description_document_id) ON DELETE RESTRICT,
    UNIQUE (workspace_id, description_document_id),
    UNIQUE (workspace_id, subject_ref, revision, description_document_id)
) STRICT;

CREATE INDEX description_documents_subject_idx
    ON description_documents(workspace_id, subject_ref, kind, revision);

CREATE TABLE semantic_segments (
    semantic_segment_id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    description_document_id TEXT NOT NULL,
    subject_ref TEXT NOT NULL,
    ordinal INTEGER NOT NULL CHECK (ordinal >= 0),
    text TEXT NOT NULL,
    text_digest TEXT NOT NULL,
    language TEXT NOT NULL,
    section TEXT NOT NULL,
    source_span_json TEXT NOT NULL CHECK (json_valid(source_span_json)),
    metadata_json TEXT NOT NULL CHECK (json_valid(metadata_json)),
    created_at_ns INTEGER NOT NULL,
    FOREIGN KEY (workspace_id, description_document_id)
        REFERENCES description_documents(workspace_id, description_document_id) ON DELETE RESTRICT,
    UNIQUE (workspace_id, semantic_segment_id),
    UNIQUE (description_document_id, ordinal)
) STRICT;

CREATE INDEX semantic_segments_subject_idx
    ON semantic_segments(workspace_id, subject_ref, description_document_id, ordinal);

CREATE TRIGGER metadata_facts_no_update
BEFORE UPDATE ON metadata_facts BEGIN
    SELECT RAISE(ABORT, 'metadata facts are immutable; publish a new revision');
END;

CREATE TRIGGER metadata_facts_no_delete
BEFORE DELETE ON metadata_facts BEGIN
    SELECT RAISE(ABORT, 'metadata facts are immutable');
END;

CREATE TRIGGER description_documents_no_update
BEFORE UPDATE ON description_documents BEGIN
    SELECT RAISE(ABORT, 'description documents are immutable; publish a new revision');
END;

CREATE TRIGGER description_documents_no_delete
BEFORE DELETE ON description_documents BEGIN
    SELECT RAISE(ABORT, 'description documents are immutable');
END;

CREATE TRIGGER semantic_segments_no_update
BEFORE UPDATE ON semantic_segments BEGIN
    SELECT RAISE(ABORT, 'semantic segments are immutable');
END;

CREATE TRIGGER semantic_segments_no_delete
BEFORE DELETE ON semantic_segments BEGIN
    SELECT RAISE(ABORT, 'semantic segments are immutable');
END;

PRAGMA user_version = 9;
`,
	},
	{
		version: 10,
		name:    "protection_outcomes",
		sql: `
-- SQLite cannot alter a CHECK constraint in place. Rebuild the two tables
-- which are coupled by the protection-record foreign key, preserving all
-- v8/v9 rows while widening the visible outcome vocabulary.
DROP INDEX recovery_references_subject_idx;
DROP INDEX recovery_references_binding_idx;
DROP TRIGGER recovery_references_no_update;
DROP TRIGGER recovery_references_no_delete;
ALTER TABLE recovery_references RENAME TO recovery_references_v8;

DROP INDEX protection_records_outcome_idx;
ALTER TABLE protection_records RENAME TO protection_records_v8;

CREATE TABLE protection_records (
    protection_record_id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    subject_ref TEXT NOT NULL,
    mode TEXT NOT NULL CHECK (mode IN (
        'STORE_EXACT', 'STORE_EXACT_WITH_EXTERNAL_FALLBACK',
        'LINK_ONLY', 'METADATA_ONLY'
    )),
    outcome TEXT NOT NULL CHECK (outcome IN (
        'EXACT_PROTECTED', 'EXACT_FALLBACK', 'EXTERNAL_REPLAYABLE',
        'LINK_ONLY_UNPROTECTED', 'EXPLICITLY_UNPROTECTED', 'BLOCKED',
        'UNAVAILABLE'
    )),
    expected_content_id TEXT,
    expected_logical_length INTEGER CHECK (
        expected_logical_length IS NULL OR expected_logical_length >= 0
    ),
    local_representation_id TEXT,
    policy_decision_ref TEXT NOT NULL,
    last_verification_ref TEXT NOT NULL,
    last_verified_at_ns INTEGER,
    revision INTEGER NOT NULL CHECK (revision >= 1),
    metadata_json TEXT NOT NULL CHECK (json_valid(metadata_json)),
    created_at_ns INTEGER NOT NULL,
    updated_at_ns INTEGER NOT NULL,
    FOREIGN KEY (workspace_id) REFERENCES workspaces(workspace_id) ON DELETE RESTRICT,
    FOREIGN KEY (workspace_id, local_representation_id)
        REFERENCES representations(workspace_id, representation_id) ON DELETE RESTRICT,
    UNIQUE (workspace_id, protection_record_id),
    UNIQUE (workspace_id, subject_ref)
) STRICT;

INSERT INTO protection_records(
    protection_record_id, workspace_id, subject_ref, mode, outcome,
    expected_content_id, expected_logical_length, local_representation_id,
    policy_decision_ref, last_verification_ref, last_verified_at_ns,
    revision, metadata_json, created_at_ns, updated_at_ns
)
SELECT protection_record_id, workspace_id, subject_ref, mode, outcome,
       expected_content_id, expected_logical_length, local_representation_id,
       policy_decision_ref, last_verification_ref, last_verified_at_ns,
       revision, metadata_json, created_at_ns, updated_at_ns
FROM protection_records_v8;

CREATE INDEX protection_records_outcome_idx
    ON protection_records(workspace_id, outcome, subject_ref);

CREATE TABLE recovery_references (
    recovery_reference_id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    protection_record_id TEXT NOT NULL,
    subject_ref TEXT NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN (
        'EXACT_REPRESENTATION', 'EXACT_REVERSIBLE',
        'EXTERNAL_LOCATOR', 'USER_RECIPE'
    )),
    priority INTEGER NOT NULL CHECK (priority >= 0),
    claim TEXT NOT NULL CHECK (claim IN (
        'RESTORE_VERIFIED', 'EXTERNAL_REPLAYABLE',
        'LINK_ONLY_UNPROTECTED', 'UNAVAILABLE'
    )),
    expected_content_id TEXT,
    expected_logical_length INTEGER CHECK (
        expected_logical_length IS NULL OR expected_logical_length >= 0
    ),
    representation_id TEXT,
    external_binding_id TEXT,
    codec_profile_ref TEXT NOT NULL,
    recipe_json TEXT NOT NULL CHECK (json_valid(recipe_json)),
    verification_json TEXT NOT NULL CHECK (json_valid(verification_json)),
    status TEXT NOT NULL,
    last_validated_at_ns INTEGER,
    expires_at_ns INTEGER,
    policy_ref TEXT NOT NULL,
    rights_evidence_ref TEXT NOT NULL,
    credential_ref TEXT NOT NULL,
    operator_decision_ref TEXT NOT NULL,
    record_digest TEXT NOT NULL,
    metadata_json TEXT NOT NULL CHECK (json_valid(metadata_json)),
    created_at_ns INTEGER NOT NULL,
    updated_at_ns INTEGER NOT NULL,
    FOREIGN KEY (workspace_id, protection_record_id)
        REFERENCES protection_records(workspace_id, protection_record_id) ON DELETE RESTRICT,
    FOREIGN KEY (workspace_id, representation_id)
        REFERENCES representations(workspace_id, representation_id) ON DELETE RESTRICT,
    FOREIGN KEY (workspace_id, external_binding_id)
        REFERENCES external_bindings(workspace_id, external_binding_id) ON DELETE RESTRICT,
    UNIQUE (workspace_id, recovery_reference_id),
    UNIQUE (workspace_id, protection_record_id, priority, recovery_reference_id),
    CHECK (
        (kind IN ('EXACT_REPRESENTATION', 'EXACT_REVERSIBLE') AND representation_id IS NOT NULL)
        OR (kind = 'EXTERNAL_LOCATOR' AND external_binding_id IS NOT NULL)
        OR (kind = 'USER_RECIPE')
    )
) STRICT;

INSERT INTO recovery_references(
    recovery_reference_id, workspace_id, protection_record_id, subject_ref,
    kind, priority, claim, expected_content_id, expected_logical_length,
    representation_id, external_binding_id, codec_profile_ref, recipe_json,
    verification_json, status, last_validated_at_ns, expires_at_ns,
    policy_ref, rights_evidence_ref, credential_ref, operator_decision_ref,
    record_digest, metadata_json, created_at_ns, updated_at_ns
)
SELECT recovery_reference_id, workspace_id, protection_record_id, subject_ref,
       kind, priority, claim, expected_content_id, expected_logical_length,
       representation_id, external_binding_id, codec_profile_ref, recipe_json,
       verification_json, status, last_validated_at_ns, expires_at_ns,
       policy_ref, rights_evidence_ref, credential_ref, operator_decision_ref,
       record_digest, metadata_json, created_at_ns, updated_at_ns
FROM recovery_references_v8;

CREATE INDEX recovery_references_subject_idx
    ON recovery_references(workspace_id, subject_ref, priority, recovery_reference_id);

CREATE INDEX recovery_references_binding_idx
    ON recovery_references(workspace_id, external_binding_id, priority);

CREATE TRIGGER recovery_references_no_update
BEFORE UPDATE ON recovery_references BEGIN
    SELECT RAISE(ABORT, 'recovery references are immutable; publish a new revision');
END;

CREATE TRIGGER recovery_references_no_delete
BEFORE DELETE ON recovery_references BEGIN
    SELECT RAISE(ABORT, 'recovery references are immutable');
END;

DROP TABLE recovery_references_v8;
DROP TABLE protection_records_v8;

PRAGMA user_version = 10;
`,
	},
	{
		version: 11,
		name:    "description_single_successor",
		sql: `
CREATE UNIQUE INDEX description_documents_predecessor_idx
    ON description_documents(workspace_id, predecessor_id)
    WHERE predecessor_id IS NOT NULL;

PRAGMA user_version = 11;
`,
	},
	{
		version: 12,
		name:    "single_job_per_plan_kind",
		sql: `
CREATE UNIQUE INDEX jobs_plan_kind_idx
    ON jobs(workspace_id, plan_id, kind)
    WHERE plan_id IS NOT NULL;

PRAGMA user_version = 12;
`,
	},
	{
		version: 13,
		name:    "publication_execution_key",
		sql: `
-- Publications created before Phase 2 have no execution key. Keep those
-- rows readable while making new plan-bound publications workspace-unique.
ALTER TABLE publications ADD COLUMN plan_digest TEXT NOT NULL DEFAULT '';

CREATE UNIQUE INDEX publications_plan_digest_idx
    ON publications(workspace_id, plan_digest)
    WHERE plan_digest <> '';

PRAGMA user_version = 13;
`,
	},
	{
		version: 14,
		name:    "processor_attempts",
		sql: `
CREATE TABLE processor_attempts (
    attempt_id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    subject_ref TEXT NOT NULL,
    snapshot_ref TEXT NOT NULL,
    route_digest TEXT NOT NULL,
    route_json TEXT NOT NULL CHECK (json_valid(route_json)),
    stage TEXT NOT NULL CHECK (stage IN (
        'CLASSIFY_LEARNED', 'PARSE', 'EXTRACT', 'ENRICH',
        'FINGERPRINT', 'TRANSFORM', 'VALIDATE', 'INDEX_PREPARE'
    )),
    capability_id TEXT NOT NULL,
    status TEXT NOT NULL CHECK (status IN ('SUCCEEDED', 'INAPPLICABLE', 'FAILED', 'CANCELLED')),
    reason_code TEXT NOT NULL,
    reason TEXT NOT NULL,
    provenance_json TEXT NOT NULL CHECK (json_valid(provenance_json)),
    fence_token INTEGER NOT NULL CHECK (fence_token >= 1),
    processor_digest TEXT NOT NULL,
    created_at_ns INTEGER NOT NULL,
    finished_at_ns INTEGER NOT NULL CHECK (finished_at_ns >= created_at_ns),
    FOREIGN KEY (workspace_id) REFERENCES workspaces(workspace_id) ON DELETE RESTRICT,
    UNIQUE (workspace_id, attempt_id)
) STRICT;

CREATE INDEX processor_attempts_subject_idx
    ON processor_attempts(workspace_id, snapshot_ref, subject_ref, created_at_ns, attempt_id);

CREATE INDEX processor_attempts_capability_idx
    ON processor_attempts(workspace_id, snapshot_ref, capability_id, status, created_at_ns);

CREATE TRIGGER processor_attempts_no_update
BEFORE UPDATE ON processor_attempts BEGIN
    SELECT RAISE(ABORT, 'processor attempts are append-only');
END;

CREATE TRIGGER processor_attempts_no_delete
BEFORE DELETE ON processor_attempts BEGIN
    SELECT RAISE(ABORT, 'processor attempts are append-only');
END;

PRAGMA user_version = 14;
`,
	},
	{
		version: 15,
		name:    "metadata_only_namespace_entries",
		sql: `
-- A metadata-only resolution has an authenticated namespace observation but
-- no content identity or file version. Keep the entry in the published
-- namespace while allowing its file-version projection to be absent.
DROP INDEX namespace_entries_parent_idx;
DROP TRIGGER namespace_entries_parent_is_directory;
DROP TRIGGER namespace_entries_no_update;
DROP TRIGGER namespace_entries_no_delete;
ALTER TABLE namespace_entries RENAME TO namespace_entries_v14;

CREATE TABLE namespace_entries (
    namespace_entry_id TEXT PRIMARY KEY,
    workspace_id TEXT NOT NULL,
    namespace_root_id TEXT NOT NULL,
    parent_entry_id TEXT,
    observation_id TEXT,
    raw_name BLOB NOT NULL,
    display_name TEXT NOT NULL,
    full_path_key BLOB NOT NULL,
    entry_type TEXT NOT NULL CHECK (entry_type IN ('REGULAR_FILE', 'DIRECTORY', 'SYMLINK', 'FIFO', 'SOCKET', 'DEVICE', 'SPECIAL')),
    metadata_json TEXT NOT NULL CHECK (json_valid(metadata_json)),
    content_id TEXT NOT NULL,
    file_version_id TEXT,
    symlink_target_raw BLOB,
    symlink_target_display TEXT NOT NULL,
    hardlink_group_id TEXT NOT NULL,
    logical_size INTEGER CHECK (logical_size IS NULL OR logical_size >= 0),
    allocated_size INTEGER CHECK (allocated_size IS NULL OR allocated_size >= 0),
    created_at_ns INTEGER NOT NULL,
    FOREIGN KEY (workspace_id, namespace_root_id)
        REFERENCES namespace_roots(workspace_id, namespace_root_id) ON DELETE RESTRICT,
    FOREIGN KEY (workspace_id, namespace_root_id, parent_entry_id)
        REFERENCES namespace_entries(workspace_id, namespace_root_id, namespace_entry_id) ON DELETE RESTRICT,
    FOREIGN KEY (workspace_id, observation_id)
        REFERENCES observations(workspace_id, observation_id) ON DELETE RESTRICT,
    FOREIGN KEY (workspace_id, file_version_id)
        REFERENCES file_versions(workspace_id, file_version_id) ON DELETE RESTRICT,
    UNIQUE (workspace_id, namespace_entry_id),
    UNIQUE (workspace_id, namespace_root_id, namespace_entry_id),
    UNIQUE (namespace_root_id, full_path_key),
    CHECK (parent_entry_id IS NULL OR parent_entry_id <> namespace_entry_id),
    CHECK ((entry_type = 'SYMLINK' AND symlink_target_raw IS NOT NULL) OR
           (entry_type <> 'SYMLINK' AND symlink_target_raw IS NULL)),
    CHECK (
        (entry_type = 'REGULAR_FILE' AND (
            (file_version_id IS NULL AND content_id = '') OR
            (file_version_id IS NOT NULL AND content_id <> '')
        )) OR
        (entry_type <> 'REGULAR_FILE' AND file_version_id IS NULL)
    )
) STRICT;

INSERT INTO namespace_entries(
    namespace_entry_id, workspace_id, namespace_root_id, parent_entry_id,
    observation_id, raw_name, display_name, full_path_key, entry_type,
    metadata_json, content_id, file_version_id, symlink_target_raw,
    symlink_target_display, hardlink_group_id, logical_size,
    allocated_size, created_at_ns
)
SELECT namespace_entry_id, workspace_id, namespace_root_id, parent_entry_id,
       observation_id, raw_name, display_name, full_path_key, entry_type,
       metadata_json, content_id, file_version_id, symlink_target_raw,
       symlink_target_display, hardlink_group_id, logical_size,
       allocated_size, created_at_ns
FROM namespace_entries_v14;

DROP TABLE namespace_entries_v14;

CREATE INDEX namespace_entries_parent_idx
    ON namespace_entries(workspace_id, namespace_root_id, parent_entry_id, raw_name);

CREATE TRIGGER namespace_entries_parent_is_directory
BEFORE INSERT ON namespace_entries
WHEN NEW.parent_entry_id IS NOT NULL
 AND (SELECT entry_type FROM namespace_entries
      WHERE workspace_id = NEW.workspace_id
        AND namespace_root_id = NEW.namespace_root_id
        AND namespace_entry_id = NEW.parent_entry_id) <> 'DIRECTORY'
BEGIN
    SELECT RAISE(ABORT, 'namespace parent must be a directory');
END;

CREATE TRIGGER namespace_entries_no_update
BEFORE UPDATE ON namespace_entries BEGIN
    SELECT RAISE(ABORT, 'namespace entries are immutable; publish a new snapshot');
END;

CREATE TRIGGER namespace_entries_no_delete
BEFORE DELETE ON namespace_entries BEGIN
    SELECT RAISE(ABORT, 'namespace entries are immutable');
END;

PRAGMA user_version = 15;
`,
	},
	{
		version: 16,
		name:    "qualified_recovery_claim_guards",
		sql: `
CREATE TRIGGER protection_records_exact_requires_local_representation
BEFORE INSERT ON protection_records
WHEN NEW.outcome IN ('EXACT_PROTECTED', 'EXACT_FALLBACK')
 AND (NEW.local_representation_id IS NULL OR NEW.last_verification_ref = '')
BEGIN
    SELECT RAISE(ABORT, 'exact protection requires local representation and verification evidence');
END;

CREATE TRIGGER protection_records_external_replay_requires_bundle
BEFORE INSERT ON protection_records
WHEN NEW.outcome = 'EXTERNAL_REPLAYABLE'
BEGIN
    SELECT RAISE(ABORT, 'external replayable protection requires an atomic qualified recovery closure');
END;

CREATE TRIGGER recovery_references_route_exclusive
BEFORE INSERT ON recovery_references
WHEN (NEW.kind IN ('EXACT_REPRESENTATION', 'EXACT_REVERSIBLE') AND NEW.external_binding_id IS NOT NULL)
  OR (NEW.kind = 'EXTERNAL_LOCATOR' AND NEW.representation_id IS NOT NULL)
  OR (NEW.kind = 'USER_RECIPE' AND NEW.representation_id IS NOT NULL AND NEW.external_binding_id IS NOT NULL)
BEGIN
    SELECT RAISE(ABORT, 'recovery reference routes must be unambiguous');
END;

CREATE TRIGGER recovery_references_external_replay_verified
BEFORE INSERT ON recovery_references
WHEN NEW.claim = 'EXTERNAL_REPLAYABLE'
 AND (
      NEW.kind <> 'EXTERNAL_LOCATOR'
      OR NEW.status <> 'VERIFIED'
      OR COALESCE(json_extract(NEW.verification_json, '$.verified'), 0) <> 1
      OR NOT EXISTS (
          SELECT 1 FROM external_locators locator
          WHERE locator.workspace_id = NEW.workspace_id
            AND locator.binding_id = NEW.external_binding_id
            AND locator.availability = 'AVAILABLE'
            AND locator.validation_status = 'VERIFIED'
      )
 )
BEGIN
    SELECT RAISE(ABORT, 'external replay claim lacks verified reacquisition evidence');
END;

CREATE TRIGGER processor_artifacts_require_terminal_attempt
BEFORE INSERT ON processor_artifacts
WHEN NOT EXISTS (
    SELECT 1 FROM processor_attempts attempt
    WHERE attempt.workspace_id = NEW.workspace_id
      AND attempt.attempt_id = NEW.attempt_id
      AND attempt.subject_ref = NEW.subject_ref
      AND attempt.snapshot_ref = NEW.snapshot_ref
      AND attempt.route_digest = NEW.route_digest
      AND attempt.stage = NEW.stage
      AND attempt.capability_id = NEW.capability_id
      AND attempt.fence_token = NEW.fence_token
      AND attempt.status = 'SUCCEEDED'
)
BEGIN
    SELECT RAISE(ABORT, 'processor artifact requires its succeeded terminal attempt');
END;

PRAGMA user_version = 16;
`,
	},
	{
		version: 17,
		name:    "immutable_annotation_revisions",
		sql: `
CREATE TABLE annotation_revisions (
    annotation_revision_id TEXT PRIMARY KEY,
    annotation_id TEXT NOT NULL,
    workspace_id TEXT NOT NULL,
    subject_ref TEXT NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN ('TAG', 'NOTE', 'PROGRESS')),
    body TEXT NOT NULL,
    body_digest TEXT NOT NULL,
    revision INTEGER NOT NULL CHECK (revision >= 1),
    predecessor_revision_id TEXT,
    tombstoned INTEGER NOT NULL CHECK (tombstoned IN (0, 1)),
    history_complete INTEGER NOT NULL CHECK (history_complete IN (0, 1)),
    created_at_ns INTEGER NOT NULL,
    FOREIGN KEY (workspace_id, annotation_id)
        REFERENCES annotations(workspace_id, annotation_id) ON DELETE RESTRICT,
    UNIQUE (workspace_id, annotation_id, revision),
    UNIQUE (workspace_id, annotation_revision_id)
) STRICT;

INSERT INTO annotation_revisions(
    annotation_revision_id, annotation_id, workspace_id, subject_ref, kind,
    body, body_digest, revision, predecessor_revision_id, tombstoned,
    history_complete, created_at_ns
)
SELECT annotation_id || '@' || revision, annotation_id, workspace_id,
       subject_ref, kind, body, body_digest, revision, NULL, tombstoned,
       CASE WHEN revision = 1 THEN 1 ELSE 0 END, updated_at_ns
FROM annotations;

CREATE INDEX annotation_revisions_subject_idx
    ON annotation_revisions(workspace_id, subject_ref, annotation_id, revision);

CREATE TRIGGER annotation_revisions_no_update
BEFORE UPDATE ON annotation_revisions BEGIN
    SELECT RAISE(ABORT, 'annotation revisions are immutable');
END;

CREATE TRIGGER annotation_revisions_no_delete
BEFORE DELETE ON annotation_revisions BEGIN
    SELECT RAISE(ABORT, 'annotation revisions are immutable');
END;

PRAGMA user_version = 17;
`,
	},
	{
		version: 18,
		name:    "publication_fences",
		sql: `
CREATE TABLE publication_fences (
    publication_domain TEXT PRIMARY KEY,
    owner TEXT NOT NULL,
    lease_token TEXT NOT NULL,
    fencing_token INTEGER NOT NULL CHECK (fencing_token >= 1),
    lease_until_ns INTEGER NOT NULL,
    updated_at_ns INTEGER NOT NULL
) STRICT;

PRAGMA user_version = 18;
`,
	},
	{
		version: 19,
		name:    "saved_views_and_export_manifests",
		sql: `
CREATE TABLE saved_views (
    view_id TEXT PRIMARY KEY,
    name TEXT NOT NULL UNIQUE,
    query TEXT NOT NULL,
    fields_json TEXT NOT NULL DEFAULT '[]',
    scope TEXT NOT NULL DEFAULT '',
    sort TEXT NOT NULL DEFAULT '',
    output_names TEXT NOT NULL DEFAULT '',
    required_json TEXT NOT NULL DEFAULT '[]',
    when_missing TEXT NOT NULL DEFAULT '',
    revision INTEGER NOT NULL DEFAULT 1,
    created_at_ns INTEGER NOT NULL,
    updated_at_ns INTEGER NOT NULL
) STRICT;

CREATE TABLE export_manifests (
    manifest_id TEXT PRIMARY KEY,
    manifest_digest TEXT NOT NULL UNIQUE,
    view_id TEXT NOT NULL DEFAULT '',
    representation TEXT NOT NULL DEFAULT 'exact',
    target TEXT NOT NULL DEFAULT '',
    subject_count INTEGER NOT NULL,
    created_at_ns INTEGER NOT NULL,
    items_json TEXT NOT NULL
) STRICT;

PRAGMA user_version = 19;
`,
	},
}

func (s *Store) migrate(ctx context.Context) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin schema migration: %w", err)
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `
CREATE TABLE IF NOT EXISTS schema_migrations (
    version INTEGER PRIMARY KEY,
    name TEXT NOT NULL,
    checksum TEXT NOT NULL,
    applied_at_ns INTEGER NOT NULL
) STRICT`); err != nil {
		return fmt.Errorf("create schema migration ledger: %w", err)
	}

	applied, err := readAppliedMigrations(ctx, tx)
	if err != nil {
		return err
	}
	latest := migrations[len(migrations)-1].version
	for version := range applied {
		if version > latest {
			return fmt.Errorf("%w: database=%d binary=%d", ErrSchemaTooNew, version, latest)
		}
	}

	ordered := append([]migration(nil), migrations...)
	sort.Slice(ordered, func(i, j int) bool { return ordered[i].version < ordered[j].version })
	for _, item := range ordered {
		checksum := migrationChecksum(item)
		if existing, ok := applied[item.version]; ok {
			if existing != checksum {
				return fmt.Errorf("%w: version %d", ErrMigrationDrift, item.version)
			}
			continue
		}
		if _, err := tx.ExecContext(ctx, item.sql); err != nil {
			return fmt.Errorf("apply migration %d (%s): %w", item.version, item.name, err)
		}
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO schema_migrations(version, name, checksum, applied_at_ns) VALUES (?, ?, ?, ?)`,
			item.version, item.name, checksum, s.now().UTC().UnixNano()); err != nil {
			return fmt.Errorf("record migration %d: %w", item.version, err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit schema migration: %w", err)
	}
	return nil
}

func readAppliedMigrations(ctx context.Context, tx *sql.Tx) (map[int]string, error) {
	rows, err := tx.QueryContext(ctx, `SELECT version, checksum FROM schema_migrations ORDER BY version`)
	if err != nil {
		return nil, fmt.Errorf("read applied migrations: %w", err)
	}
	defer rows.Close()

	applied := make(map[int]string)
	for rows.Next() {
		var version int
		var checksum string
		if err := rows.Scan(&version, &checksum); err != nil {
			return nil, fmt.Errorf("scan applied migration: %w", err)
		}
		if _, duplicate := applied[version]; duplicate {
			return nil, fmt.Errorf("duplicate migration version %d", version)
		}
		applied[version] = checksum
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate applied migrations: %w", err)
	}
	return applied, nil
}

func migrationChecksum(item migration) string {
	digest := sha256.Sum256([]byte(fmt.Sprintf("%d\n%s\n%s", item.version, item.name, item.sql)))
	return hex.EncodeToString(digest[:])
}

func (s *Store) SchemaVersion(ctx context.Context) (int, error) {
	var version sql.NullInt64
	if err := s.db.QueryRowContext(ctx, `SELECT MAX(version) FROM schema_migrations`).Scan(&version); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return 0, nil
		}
		return 0, fmt.Errorf("read schema version: %w", err)
	}
	if !version.Valid {
		return 0, nil
	}
	return int(version.Int64), nil
}
