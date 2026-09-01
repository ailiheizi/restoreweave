package exact

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sort"
	"strings"
	"time"

	"github.com/ailiheizi/restoreweave/server/internal/repository"
	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
)

const (
	PortableFactBundleSchemaV1 = "org.restoreweave.portable-fact-bundle.v1"
	PortableFactRecordSchemaV1 = "org.restoreweave.portable-fact-record.v1"
	PortableFactBundleSchemaV2 = "org.restoreweave.portable-fact-bundle.v2"
	PortableFactRecordSchemaV2 = "org.restoreweave.portable-fact-record.v2"
)

type PortableFactClosureEnvelope struct {
	Schema  string                    `json:"schema"`
	Closure PortableFactClosureRecord `json:"closure"`
	Bundle  json.RawMessage           `json:"bundle"`
}

type portableFactBundle struct {
	Schema      string                   `json:"schema"`
	WorkspaceID string                   `json:"workspace_id"`
	SnapshotRef string                   `json:"snapshot_ref"`
	Records     []portableFactRecord     `json:"records"`
	Attachments []portableFactAttachment `json:"attachments"`
}

type portableFactRecord struct {
	Schema              string          `json:"schema"`
	RecordKind          string          `json:"record_kind"`
	RecordID            string          `json:"record_id"`
	WorkspaceID         string          `json:"workspace_id"`
	SnapshotRef         string          `json:"snapshot_ref"`
	StableSubjectRef    string          `json:"stable_subject_ref"`
	Revision            int64           `json:"revision"`
	PredecessorRecordID string          `json:"predecessor_record_id,omitempty"`
	PayloadDigest       string          `json:"payload_digest"`
	PayloadLength       int64           `json:"payload_length"`
	Provenance          json.RawMessage `json:"provenance"`
	Payload             json.RawMessage `json:"payload"`
}

type portableFactAttachment struct {
	AttachmentID  string `json:"attachment_id"`
	Purpose       string `json:"purpose"`
	MediaType     string `json:"media_type"`
	ContentID     string `json:"content_id"`
	LogicalLength int64  `json:"logical_length"`
	ReaderProfile string `json:"reader_profile"`
	RepositoryID  string `json:"repository_id"`
	// body is an in-process placement input only. It is deliberately omitted
	// from the signed bundle; ContentID/LogicalLength are the authenticated
	// portable attachment identity.
	body []byte `json:"-"`
}

type subjectMappingPayload struct {
	WorkspaceID                string             `json:"workspace_id"`
	NamespaceRootID            string             `json:"namespace_root_id"`
	NamespaceEntryID           string             `json:"namespace_entry_id"`
	StableSubjectRef           string             `json:"stable_subject_ref,omitempty"`
	SourceID                   string             `json:"source_id"`
	ParentSubjectRef           string             `json:"parent_subject_ref,omitempty"`
	RawPath                    []byte             `json:"raw_path"`
	RawName                    []byte             `json:"raw_name"`
	DisplayName                string             `json:"display_name"`
	EntryType                  string             `json:"entry_type"`
	ContentID                  string             `json:"content_id,omitempty"`
	LogicalLength              *int64             `json:"logical_length,omitempty"`
	FileVersionID              string             `json:"file_version_id,omitempty"`
	SelectedRepresentationRefs []string           `json:"selected_representation_refs"`
	MetadataBefore             json.RawMessage    `json:"metadata_before,omitempty"`
	MetadataAfter              json.RawMessage    `json:"metadata_after,omitempty"`
	Protection                 ManifestProtection `json:"protection"`
}

type descriptionPortablePayload struct {
	ID                    string                 `json:"id"`
	WorkspaceID           string                 `json:"workspace_id"`
	SubjectRef            string                 `json:"subject_ref"`
	Kind                  sqlite.DescriptionKind `json:"kind"`
	Title                 string                 `json:"title"`
	Language              string                 `json:"language"`
	BodyDigest            string                 `json:"body_digest"`
	SourceRef             string                 `json:"source_ref"`
	ProducerProfile       string                 `json:"producer_profile"`
	ConfigDigest          string                 `json:"config_digest,omitempty"`
	ProducerProfileDigest string                 `json:"producer_profile_digest,omitempty"`
	Confidence            *float64               `json:"confidence,omitempty"`
	Coverage              *float64               `json:"coverage,omitempty"`
	Visibility            string                 `json:"visibility"`
	Accepted              bool                   `json:"accepted"`
	Revision              int64                  `json:"revision"`
	PredecessorID         string                 `json:"predecessor_id,omitempty"`
	Metadata              json.RawMessage        `json:"metadata"`
	CreatedAt             time.Time              `json:"created_at"`
	UpdatedAt             time.Time              `json:"updated_at"`
	BodyAttachmentID      string                 `json:"body_attachment_id"`
}

type artifactPortablePayload struct {
	ID               string                        `json:"id"`
	WorkspaceID      string                        `json:"workspace_id"`
	SubjectRef       string                        `json:"subject_ref"`
	SnapshotRef      string                        `json:"snapshot_ref"`
	RouteDigest      string                        `json:"route_digest"`
	Stage            string                        `json:"stage"`
	CapabilityID     string                        `json:"capability_id"`
	SchemaRef        string                        `json:"schema_ref"`
	State            sqlite.ProcessorArtifactState `json:"state"`
	AuthorityClass   string                        `json:"authority_class"`
	LifecycleClass   string                        `json:"lifecycle_class"`
	MediaType        string                        `json:"media_type"`
	ByteLength       int64                         `json:"byte_length"`
	Digest           string                        `json:"digest"`
	AttemptID        string                        `json:"attempt_id"`
	FenceToken       int64                         `json:"fence_token"`
	ProducerDigest   string                        `json:"producer_digest"`
	Envelope         json.RawMessage               `json:"envelope"`
	CreatedAt        time.Time                     `json:"created_at"`
	UpdatedAt        time.Time                     `json:"updated_at"`
	BodyAttachmentID string                        `json:"body_attachment_id"`
}

type portableSemanticSourceSpan struct {
	StartByte int `json:"start_byte"`
	EndByte   int `json:"end_byte"`
}

var errPortableFactConflict = errors.New("conflicting portable fact closure")

func (s *Service) publishPortableFactClosure(ctx context.Context, workspaceID, snapshotRef, parentDigest string) (retErr error) {
	if !s.signedPublicationEnabled() {
		return nil
	}
	if s.SigningIdentity == nil || s.TrustAnchor == nil || strings.TrimSpace(s.PublicationDomain) == "" {
		return errors.New("portable fact closure requires signing identity, trust anchor, and publication domain")
	}
	driver, err := s.publicationRecordDriver()
	if err != nil {
		return err
	}
	s.publicationMu.Lock()
	defer s.publicationMu.Unlock()
	lease, err := s.acquirePublicationFence(ctx)
	if err != nil {
		return err
	}
	ctx = lease.context()
	defer func() {
		if releaseErr := lease.release(); releaseErr != nil {
			retErr = errors.Join(retErr, fmt.Errorf("%w: %w: release portable fact publication lease: %v", ErrNeedsReconciliation, ErrPublicationLeaseRelease, releaseErr))
		}
	}()
	publications, err := s.committedPublications(ctx)
	if err != nil {
		return err
	}
	var parent committedPublication
	for _, candidate := range publications {
		if candidate.CommitDigest == parentDigest {
			parent = candidate
			break
		}
	}
	if parent.CommitDigest == "" {
		return errors.New("portable fact closure parent is not a committed publication")
	}
	if parent.Commit.TargetIdentity != driver.RepositoryIdentity() || parent.Commit.PublicationDomain != s.PublicationDomain || parent.Commit.SnapshotRef != snapshotRef || parent.Commit.ManifestDigest != parent.Manifest.ManifestDigest {
		return errors.New("portable fact closure parent binding mismatch")
	}
	bundle, attachments, err := s.buildPortableFactBundleUnplaced(ctx, workspaceID, parent.Manifest, driver.RepositoryIdentity())
	if err != nil {
		return err
	}
	bundleBytes, err := CanonicalJSON(bundle)
	if err != nil {
		return err
	}
	bundleDigest := DigestBytes(bundleBytes)
	existing, err := listPortableFactClosures(ctx, s.Repo, driver, *s.TrustAnchor, s.PublicationDomain, parentDigest)
	if err != nil {
		return err
	}
	sequence := uint64(1)
	predecessor := ""
	if len(existing) > 0 {
		latest := existing[len(existing)-1]
		if latest.Closure.BundleDigest == bundleDigest {
			return nil
		}
		sequence = latest.Closure.ClosureSequence + 1
		predecessor, err = latest.Closure.Digest()
		if err != nil {
			return err
		}
	}
	for _, attachment := range attachments {
		if err := s.validatePublicationFence(ctx, lease); err != nil {
			return err
		}
		if _, err := placePortableAttachmentWithReadback(ctx, s.Repo, attachment); err != nil {
			return fmt.Errorf("portable fact attachment %s: %w", attachment.AttachmentID, err)
		}
		if err := s.validatePublicationFence(ctx, lease); err != nil {
			return err
		}
	}
	processorDigest, err := s.admittedProcessorAttemptDigest(ctx, workspaceID, snapshotRef, parentDigest)
	if err != nil {
		return err
	}
	closureSchema, envelopeSchema, err := portableFactClosureSchemas(bundle.Schema)
	if err != nil {
		return err
	}
	readerDependencies := portableFactReaderDependenciesForSchema(s.Repo, closureSchema)
	closure, err := SignPortableFactClosure(*s.SigningIdentity, PortableFactClosureRecord{
		Schema: closureSchema, SignatureDomain: RecoverySignatureDomainV1, RecordKind: PortableFactClosureKind,
		WorkspaceID: workspaceID, PublicationID: parent.Commit.PublicationID, PublicationDomain: s.PublicationDomain,
		SnapshotRef: snapshotRef, ManifestDigest: parent.Commit.ManifestDigest, ParentCommitDigest: parentDigest,
		ParentGeneration: parent.Commit.Generation, ClosureSequence: sequence, PredecessorClosureDigest: predecessor, BundleSchema: bundle.Schema,
		BundleDigest: bundleDigest, BundleLength: int64(len(bundleBytes)), RecordCount: int64(len(bundle.Records)),
		AttachmentCount: int64(len(bundle.Attachments)), ProcessorAttemptDigest: processorDigest,
		TargetIdentity: driver.RepositoryIdentity(), WriterIdentity: s.SigningIdentity.WriterIdentity, KeyID: s.SigningIdentity.KeyID,
		FenceToken: parent.Commit.FenceToken, RequiredReaderDependencies: readerDependencies,
		CanonicalizationProfile: "encoding/json-compact-v1", CriticalExtensions: []string{}, OptionalExtensions: json.RawMessage(`{}`), SignedAt: s.now(),
	})
	if err != nil {
		return err
	}
	payload, err := CanonicalJSON(PortableFactClosureEnvelope{Schema: envelopeSchema, Closure: closure, Bundle: bundleBytes})
	if err != nil {
		return err
	}
	if err := s.validatePublicationFence(ctx, lease); err != nil {
		return err
	}
	receipt, err := driver.PlaceRecord(ctx, repository.RecordPortableFactClosure, bytes.NewReader(payload))
	if err != nil {
		return s.reconcileUnknownChildOutcome(ctx, driver, repository.RecordPortableFactClosure, payload, parent.Commit.PublicationID, snapshotRef, parentDigest, parent.Commit.PlanDigest, fmt.Errorf("place portable fact closure: %w", err))
	}
	if err := driver.VerifyRecord(ctx, receipt); err != nil {
		return s.reconcileUnknownChildOutcome(ctx, driver, repository.RecordPortableFactClosure, payload, parent.Commit.PublicationID, snapshotRef, parentDigest, parent.Commit.PlanDigest, fmt.Errorf("verify portable fact closure: %w", err))
	}
	if err := s.validatePublicationFence(ctx, lease); err != nil {
		return s.reconcileUnknownChildOutcome(ctx, driver, repository.RecordPortableFactClosure, payload, parent.Commit.PublicationID, snapshotRef, parentDigest, parent.Commit.PlanDigest, err)
	}
	return nil
}

// PublishPortableFactClosure explicitly reconciles the current catalog facts
// into the signed post-publication child. It is safe to call repeatedly for
// the same complete state.
func (s *Service) PublishPortableFactClosure(ctx context.Context, workspaceID, snapshotRef, parentDigest string) error {
	return s.publishPortableFactClosure(ctx, workspaceID, snapshotRef, parentDigest)
}

const (
	PortableFactClosureEnvelopeSchemaV1 = "org.restoreweave.portable-fact-closure-envelope.v1"
	PortableFactClosureEnvelopeSchemaV2 = "org.restoreweave.portable-fact-closure-envelope.v2"
)

func (s *Service) admittedProcessorAttemptDigest(ctx context.Context, workspaceID, snapshotRef, parentDigest string) (string, error) {
	artifacts, err := s.Store.ListAdmittedArtifacts(ctx, workspaceID, snapshotRef)
	if err != nil {
		return "", err
	}
	if len(artifacts) == 0 {
		return "", nil
	}
	entries, err := s.processorAttemptClosureEntries(ctx, snapshotRef)
	if err != nil {
		return "", err
	}
	for index := len(entries) - 1; index >= 0; index-- {
		entry := entries[index]
		if entry.Envelope.Closure.ParentCommitDigest != parentDigest {
			continue
		}
		if entry.Envelope.Closure.WorkspaceID != workspaceID {
			return "", errors.New("processor attempt closure workspace binding mismatch")
		}
		// The portable-fact child binds the full signed envelope object, not the
		// closure field alone. The catalog-free reader resolves this object
		// digest and re-verifies its envelope before accepting artifacts.
		return entry.ObjectDigest, nil
	}
	return "", errors.New("admitted processor artifacts have no authenticated processor-attempt child")
}

func placePortableAttachmentWithReadback(ctx context.Context, repo repository.Driver, attachment portableFactAttachment) (repository.Receipt, error) {
	if !validExactContentID(attachment.ContentID) || attachment.LogicalLength < 0 ||
		int64(len(attachment.body)) != attachment.LogicalLength || DigestBytes(attachment.body) != attachment.ContentID {
		return repository.Receipt{}, errors.New("portable attachment body identity is invalid")
	}
	return placeExactWithReadback(ctx, repo, attachment.ContentID, attachment.LogicalLength, bytes.NewReader(attachment.body))
}

// buildPortableFactBundle is retained for package-local callers that need a
// materialized fixture. The publication path uses buildPortableFactBundleUnplaced
// and performs attachment placement under its active publication lease.
func (s *Service) buildPortableFactBundle(ctx context.Context, workspaceID string, manifest Manifest, repositoryID string) (portableFactBundle, []portableFactAttachment, error) {
	bundle, attachments, err := s.buildPortableFactBundleUnplaced(ctx, workspaceID, manifest, repositoryID)
	if err != nil {
		return bundle, attachments, err
	}
	for _, attachment := range attachments {
		if _, err := placePortableAttachmentWithReadback(ctx, s.Repo, attachment); err != nil {
			return portableFactBundle{}, nil, err
		}
	}
	return bundle, attachments, nil
}

func (s *Service) buildPortableFactBundleUnplaced(ctx context.Context, workspaceID string, manifest Manifest, repositoryID string) (portableFactBundle, []portableFactAttachment, error) {
	snapshotRef := manifest.SnapshotRef
	root, err := s.Store.GetNamespaceRootBySnapshotRef(ctx, snapshotRef)
	if err != nil {
		return portableFactBundle{}, nil, err
	}
	if root.WorkspaceID != workspaceID {
		return portableFactBundle{}, nil, errors.New("portable fact namespace root workspace mismatch")
	}
	nodes, err := s.Store.ListNamespaceSubtree(ctx, workspaceID, root.ID, "")
	if err != nil {
		return portableFactBundle{}, nil, err
	}
	subjects := make(map[string]sqlite.NamespaceEntry, len(nodes))
	stableByRef := make(map[string]string, len(nodes)*2)
	for _, node := range nodes {
		subjects[node.Entry.ID] = node.Entry
		if node.Entry.SubjectRef != "" {
			subjects[node.Entry.SubjectRef] = node.Entry
			stableByRef[node.Entry.SubjectRef] = node.Entry.SubjectRef
		}
		stableByRef[node.Entry.ID] = node.Entry.SubjectRef
	}
	manifestByPath := make(map[string]ManifestEntry, len(manifest.Entries))
	for _, entry := range manifest.Entries {
		key := string(entry.RawPath)
		if key == "" {
			key = "."
		}
		if _, exists := manifestByPath[key]; exists {
			return portableFactBundle{}, nil, errors.New("portable manifest contains duplicate raw paths")
		}
		manifestByPath[key] = entry
	}
	bundle := portableFactBundle{Schema: PortableFactBundleSchemaV2, WorkspaceID: workspaceID, SnapshotRef: snapshotRef, Records: make([]portableFactRecord, 0), Attachments: make([]portableFactAttachment, 0)}
	attachments := make([]portableFactAttachment, 0)
	recordSchema := PortableFactRecordSchemaV2
	add := func(kind, id, subject string, revision int64, predecessor string, payload any, provenance any) error {
		payloadBytes, err := CanonicalJSON(payload)
		if err != nil {
			return err
		}
		provenanceBytes, err := CanonicalJSON(provenance)
		if err != nil {
			return err
		}
		bundle.Records = append(bundle.Records, portableFactRecord{Schema: recordSchema, RecordKind: kind, RecordID: id, WorkspaceID: workspaceID, SnapshotRef: snapshotRef, StableSubjectRef: subject, Revision: revision, PredecessorRecordID: predecessor, PayloadDigest: DigestBytes(payloadBytes), PayloadLength: int64(len(payloadBytes)), Provenance: provenanceBytes, Payload: payloadBytes})
		return nil
	}
	for _, node := range nodes {
		entry := node.Entry
		portableEntry, ok := manifestByPath[string(entry.FullPathKey)]
		if !ok {
			return bundle, nil, fmt.Errorf("portable subject %q has no manifest entry", entry.ID)
		}
		selected := make(map[string]struct{})
		if portableEntry.Protection.LocalRepresentationID != "" {
			selected[portableEntry.Protection.LocalRepresentationID] = struct{}{}
		}
		for _, reference := range portableEntry.Protection.RecoveryReferences {
			if reference.RepresentationID != "" {
				selected[reference.RepresentationID] = struct{}{}
			}
		}
		selectedRefs := make([]string, 0, len(selected))
		for ref := range selected {
			selectedRefs = append(selectedRefs, ref)
		}
		sort.Strings(selectedRefs)
		if strings.TrimSpace(entry.SubjectRef) == "" {
			return bundle, nil, fmt.Errorf("portable subject %q has no stable subject reference", entry.ID)
		}
		parentSubject := ""
		if entry.ParentID != "" && entry.ParentID != root.ID {
			parent, ok := subjects[entry.ParentID]
			if !ok || strings.TrimSpace(parent.SubjectRef) == "" {
				return bundle, nil, fmt.Errorf("portable subject %q has no stable parent reference", entry.ID)
			}
			parentSubject = parent.SubjectRef
		}
		mapping := subjectMappingPayload{
			WorkspaceID: workspaceID, NamespaceRootID: root.ID, NamespaceEntryID: entry.ID,
			StableSubjectRef: entry.SubjectRef, SourceID: root.SourceID, ParentSubjectRef: parentSubject,
			RawPath: append([]byte(nil), portableEntry.RawPath...), RawName: append([]byte(nil), portableEntry.RawName...),
			DisplayName: entry.DisplayName, EntryType: portableEntry.EntryType,
			ContentID: portableEntry.ContentID, LogicalLength: portableEntry.LogicalSize,
			FileVersionID: entry.FileVersionID, SelectedRepresentationRefs: selectedRefs,
			MetadataBefore: append(json.RawMessage(nil), portableEntry.MetadataBefore...),
			MetadataAfter:  append(json.RawMessage(nil), portableEntry.MetadataAfter...),
			Protection:     portableEntry.Protection,
		}
		if err := add("SUBJECT_MAPPING", entry.ID, entry.SubjectRef, 1, "", mapping, map[string]any{"namespace_root_id": root.ID, "entry_type": entry.EntryType}); err != nil {
			return bundle, nil, err
		}
		if portableEntry.Facts == nil {
			return bundle, nil, fmt.Errorf("portable subject %q has no captured facts", entry.ID)
		}
		for _, fact := range portableEntry.Facts.Facts {
			if err := add("METADATA_FACT", entry.ID+":capture:"+fact.Name, entry.SubjectRef, 1, "", fact, map[string]any{
				"source_profile": fact.SourceProfile, "authority": fact.Authority,
				"capture_time": fact.CapturedAt, "provenance_digest": fact.ProvenanceDigest,
			}); err != nil {
				return bundle, nil, err
			}
		}
	}
	facts, err := s.Store.ListMetadataFacts(ctx, workspaceID, "")
	if err != nil {
		return bundle, nil, err
	}
	for _, fact := range facts {
		stableSubject, ok := stableByRef[fact.SubjectRef]
		if !ok || stableSubject == "" {
			continue
		}
		fact.SubjectRef = stableSubject
		if err := add("METADATA_FACT", fact.ID, stableSubject, fact.Revision, "", fact, map[string]any{"authority": fact.AuthorityClass, "source_ref": fact.SourceRef}); err != nil {
			return bundle, nil, err
		}
	}
	annotations, err := s.Store.ListAnnotationRevisions(ctx, workspaceID, "")
	if err != nil {
		return bundle, nil, err
	}
	for _, annotation := range annotations {
		stableSubject, ok := stableByRef[annotation.SubjectRef]
		if !ok || stableSubject == "" {
			continue
		}
		annotation.SubjectRef = stableSubject
		if err := add("ANNOTATION_REVISION", annotation.ID, annotation.SubjectRef, annotation.Revision, annotation.PredecessorID, annotation, map[string]any{"created_at": annotation.CreatedAt}); err != nil {
			return bundle, nil, err
		}
	}
	docs, err := s.Store.ListDescriptionDocuments(ctx, workspaceID, "")
	if err != nil {
		return bundle, nil, err
	}
	for _, doc := range docs {
		stableSubject, ok := stableByRef[doc.SubjectRef]
		if !ok || stableSubject == "" {
			continue
		}
		doc.SubjectRef = stableSubject
		bodyID := "attachment:description:" + doc.ID
		bodyBytes := []byte(doc.Body)
		bodyDigest := DigestBytes(bodyBytes)
		if bodyDigest != doc.BodyDigest {
			return bundle, nil, errors.New("description body attachment does not match its durable digest")
		}
		attachments = append(attachments, portableFactAttachment{AttachmentID: bodyID, Purpose: "DESCRIPTION_BODY", MediaType: "text/plain; charset=utf-8", ContentID: bodyDigest, LogicalLength: int64(len(bodyBytes)), ReaderProfile: "utf8-v1", RepositoryID: repositoryID, body: bodyBytes})
		producerProfileDigest := doc.ProducerProfileDigest
		if strings.TrimSpace(doc.ConfigDigest) == "" || strings.TrimSpace(producerProfileDigest) == "" {
			return bundle, nil, errors.New("description revision lacks authenticated config or producer profile binding")
		}
		payload := descriptionPortablePayload{ID: doc.ID, WorkspaceID: doc.WorkspaceID, SubjectRef: doc.SubjectRef, Kind: doc.Kind, Title: doc.Title, Language: doc.Language, BodyDigest: doc.BodyDigest, SourceRef: doc.SourceRef, ProducerProfile: doc.ProducerProfile, ConfigDigest: doc.ConfigDigest, ProducerProfileDigest: producerProfileDigest, Confidence: doc.Confidence, Coverage: doc.Coverage, Visibility: doc.Visibility, Accepted: doc.Accepted, Revision: doc.Revision, PredecessorID: doc.PredecessorID, Metadata: doc.Metadata, CreatedAt: doc.CreatedAt, UpdatedAt: doc.UpdatedAt, BodyAttachmentID: bodyID}
		if err := add("DESCRIPTION_REVISION", doc.ID, doc.SubjectRef, doc.Revision, doc.PredecessorID, payload, map[string]any{"source_ref": doc.SourceRef, "producer_profile": doc.ProducerProfile, "config_digest": doc.ConfigDigest, "producer_profile_digest": producerProfileDigest}); err != nil {
			return bundle, nil, err
		}
		segments, e := s.Store.ListSemanticSegments(ctx, workspaceID, doc.ID)
		if e != nil {
			return bundle, nil, e
		}
		for _, segment := range segments {
			segment.SubjectRef = stableSubject
			if err := add("SEMANTIC_SEGMENT", segment.ID, segment.SubjectRef, segment.Ordinal+1, "", segment, map[string]any{"description_document_id": doc.ID, "document_revision": segment.DocumentRevision, "segmentation_profile_digest": segment.SegmentationProfileDigest}); err != nil {
				return bundle, nil, err
			}
		}
	}
	artifacts, err := s.Store.ListAdmittedArtifacts(ctx, workspaceID, snapshotRef)
	if err != nil {
		return bundle, nil, err
	}
	for _, artifact := range artifacts {
		stableSubject, ok := stableByRef[artifact.SubjectRef]
		if !ok || stableSubject == "" {
			return bundle, nil, errors.New("processor artifact subject is absent from snapshot")
		}
		artifact.SubjectRef = stableSubject
		bodyID := "attachment:artifact:" + artifact.ID
		bodyBytes := append([]byte(nil), artifact.Body...)
		bodyDigest := DigestBytes(bodyBytes)
		if bodyDigest != artifact.Digest || int64(len(bodyBytes)) != artifact.ByteLength {
			return bundle, nil, errors.New("processor artifact attachment does not match its durable digest")
		}
		attachments = append(attachments, portableFactAttachment{AttachmentID: bodyID, Purpose: "PROCESSOR_ARTIFACT_BODY", MediaType: artifact.MediaType, ContentID: bodyDigest, LogicalLength: int64(len(bodyBytes)), ReaderProfile: "artifact:" + artifact.SchemaRef, RepositoryID: repositoryID, body: bodyBytes})
		payload := artifactPortablePayload{ID: artifact.ID, WorkspaceID: artifact.WorkspaceID, SubjectRef: artifact.SubjectRef, SnapshotRef: artifact.SnapshotRef, RouteDigest: artifact.RouteDigest, Stage: artifact.Stage, CapabilityID: artifact.CapabilityID, SchemaRef: artifact.SchemaRef, State: artifact.State, AuthorityClass: artifact.AuthorityClass, LifecycleClass: artifact.LifecycleClass, MediaType: artifact.MediaType, ByteLength: artifact.ByteLength, Digest: artifact.Digest, AttemptID: artifact.AttemptID, FenceToken: artifact.FenceToken, ProducerDigest: artifact.ProducerDigest, Envelope: artifact.Envelope, CreatedAt: artifact.CreatedAt, UpdatedAt: artifact.UpdatedAt, BodyAttachmentID: bodyID}
		if err := add("PROCESSOR_ARTIFACT_DESCRIPTOR", artifact.ID, artifact.SubjectRef, 1, "", payload, map[string]any{"attempt_id": artifact.AttemptID, "producer_digest": artifact.ProducerDigest}); err != nil {
			return bundle, nil, err
		}
	}
	sort.Slice(bundle.Records, func(i, j int) bool {
		a, b := bundle.Records[i], bundle.Records[j]
		if a.RecordKind != b.RecordKind {
			return a.RecordKind < b.RecordKind
		}
		if a.RecordID != b.RecordID {
			return a.RecordID < b.RecordID
		}
		return a.Revision < b.Revision
	})
	sort.Slice(attachments, func(i, j int) bool { return attachments[i].AttachmentID < attachments[j].AttachmentID })
	bundle.Attachments = attachments
	return bundle, attachments, nil
}

func listPortableFactClosures(ctx context.Context, repo repository.Driver, driver repository.RecordDriver, anchor TrustAnchor, domain, parentDigest string) ([]PortableFactClosureEnvelope, error) {
	commits, err := listCommitMarkers(ctx, driver, anchor, domain)
	if err != nil {
		return nil, err
	}
	parents := make(map[string]committedPublication, len(commits))
	for i := range commits {
		preparedBytes, err := readRecord(ctx, driver, repository.RecordPreparedClosure, commits[i].Commit.PreparedObjectDigest)
		if err != nil {
			return nil, err
		}
		var prepared PreparedClosureEnvelope
		if err := decodeStrictRecord(preparedBytes, &prepared); err != nil {
			return nil, err
		}
		if err := validatePreparedEnvelope(driver, anchor, commits[i].Commit, prepared, int64(len(preparedBytes))); err != nil {
			return nil, err
		}
		commits[i].Prepared = prepared
		commits[i].Manifest = prepared.Manifest
		parents[commits[i].CommitDigest] = commits[i]
	}
	digests, err := driver.ListRecordDigests(ctx, repository.RecordPortableFactClosure)
	if err != nil {
		return nil, err
	}
	var result []PortableFactClosureEnvelope
	bySequence := make(map[uint64]string)
	for _, digest := range digests {
		payload, err := readRecord(ctx, driver, repository.RecordPortableFactClosure, digest)
		if err != nil {
			return nil, err
		}
		var envelope PortableFactClosureEnvelope
		if err := decodeStrictRecord(payload, &envelope); err != nil {
			return nil, err
		}
		if envelope.Schema != PortableFactClosureEnvelopeSchemaV1 && envelope.Schema != PortableFactClosureEnvelopeSchemaV2 || DigestBytes(payload) != digest {
			return nil, errors.New("portable fact closure envelope is invalid")
		}
		closureSchema := PortableFactClosureSchemaV1
		if envelope.Schema == PortableFactClosureEnvelopeSchemaV2 {
			closureSchema = PortableFactClosureSchemaV2
		}
		if envelope.Closure.Schema != closureSchema {
			return nil, errors.New("portable fact closure envelope and closure schema differ")
		}
		if err := envelope.Closure.Verify(anchor); err != nil {
			return nil, err
		}
		if envelope.Closure.BundleSchema != PortableFactBundleSchemaV1 && envelope.Closure.BundleSchema != PortableFactBundleSchemaV2 ||
			envelope.Closure.CanonicalizationProfile != "encoding/json-compact-v1" ||
			!sameStrings(envelope.Closure.RequiredReaderDependencies, portableFactReaderDependenciesForSchema(repo, closureSchema)) {
			return nil, errors.New("portable fact closure reader dependencies are unavailable")
		}
		expectedClosureSchema, _, schemaErr := portableFactClosureSchemas(envelope.Closure.BundleSchema)
		if schemaErr != nil || expectedClosureSchema != closureSchema {
			return nil, errors.New("portable fact closure bundle and closure schema differ")
		}
		if envelope.Closure.PublicationDomain != domain {
			continue
		}
		if envelope.Closure.ParentCommitDigest != parentDigest {
			continue
		}
		parent, ok := parents[envelope.Closure.ParentCommitDigest]
		if !ok || parent.Commit.PublicationID != envelope.Closure.PublicationID || parent.Commit.PublicationDomain != envelope.Closure.PublicationDomain || parent.Commit.SnapshotRef != envelope.Closure.SnapshotRef || parent.Commit.ManifestDigest != envelope.Closure.ManifestDigest || parent.Commit.Generation != envelope.Closure.ParentGeneration || parent.Commit.TargetIdentity != envelope.Closure.TargetIdentity || parent.Commit.FenceToken != envelope.Closure.FenceToken || envelope.Closure.SignedAt.Before(parent.Commit.SignedAt) {
			return nil, errors.New("portable fact closure parent binding mismatch")
		}
		bundle, err := validatePortableFactBundle(envelope.Bundle, envelope.Closure.WorkspaceID, envelope.Closure.SnapshotRef)
		if err != nil {
			return nil, err
		}
		if err := validatePortableFactRecords(bundle); err != nil {
			return nil, err
		}
		if DigestBytes(envelope.Bundle) != envelope.Closure.BundleDigest || int64(len(envelope.Bundle)) != envelope.Closure.BundleLength || int64(len(bundle.Records)) != envelope.Closure.RecordCount || int64(len(bundle.Attachments)) != envelope.Closure.AttachmentCount {
			return nil, errors.New("portable fact closure bundle binding mismatch")
		}
		if err := validatePortableFactRecordsAgainstManifest(bundle, parent.Manifest); err != nil {
			return nil, err
		}
		for _, attachment := range bundle.Attachments {
			if attachment.RepositoryID != driver.RepositoryIdentity() || !validExactContentID(attachment.ContentID) || attachment.LogicalLength < 0 {
				return nil, errors.New("portable fact attachment descriptor is invalid")
			}
			if err := verifyExactObjectReadback(ctx, repo, attachment.ContentID, attachment.LogicalLength); err != nil {
				return nil, fmt.Errorf("portable fact attachment %s: %w", attachment.AttachmentID, err)
			}
		}
		if err := validateProcessorAttachmentChild(ctx, driver, anchor, envelope.Closure, bundle); err != nil {
			return nil, err
		}
		if previous, exists := bySequence[envelope.Closure.ClosureSequence]; exists && previous != envelope.Closure.BundleDigest {
			return nil, errPortableFactConflict
		}
		bySequence[envelope.Closure.ClosureSequence] = envelope.Closure.BundleDigest
		result = append(result, envelope)
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Closure.ClosureSequence < result[j].Closure.ClosureSequence })
	for i, envelope := range result {
		if envelope.Closure.ClosureSequence != uint64(i+1) {
			return nil, errors.New("portable fact closure sequence is incomplete")
		}
		if i == 0 {
			if envelope.Closure.PredecessorClosureDigest != "" {
				return nil, errors.New("portable fact sequence one has a predecessor")
			}
			continue
		}
		previousDigest, err := result[i-1].Closure.Digest()
		if err != nil || envelope.Closure.PredecessorClosureDigest != previousDigest {
			return nil, errors.New("portable fact closure predecessor mismatch")
		}
		if envelope.Closure.SignedAt.Before(result[i-1].Closure.SignedAt) {
			return nil, errors.New("portable fact closure signed time is not monotonic")
		}
	}
	return result, nil
}

func portableFactReaderDependencies(repo repository.Driver) []string {
	return portableFactReaderDependenciesForSchema(repo, PortableFactClosureSchemaV2)
}

func portableFactReaderDependenciesForSchema(repo repository.Driver, closureSchema string) []string {
	profile := repository.DescribeProfile(repo)
	readerVersion := "v2"
	if closureSchema == PortableFactClosureSchemaV1 {
		readerVersion = "v1"
	}
	return []string{
		"canonicalization:encoding/json-compact-v1",
		"repository:" + profile.Repository + "/" + profile.Compression,
		"restoreweave-reader:portable-fact-" + readerVersion,
		"signature:ed25519-v1",
	}
}

func portableFactClosureSchemas(bundleSchema string) (closureSchema, envelopeSchema string, err error) {
	switch bundleSchema {
	case PortableFactBundleSchemaV1:
		return PortableFactClosureSchemaV1, PortableFactClosureEnvelopeSchemaV1, nil
	case PortableFactBundleSchemaV2:
		return PortableFactClosureSchemaV2, PortableFactClosureEnvelopeSchemaV2, nil
	default:
		return "", "", fmt.Errorf("%w: unsupported portable fact bundle schema %q", ErrRecoveryRecordInvalid, bundleSchema)
	}
}

func portableFactRecordSchema(bundleSchema string) (string, error) {
	switch bundleSchema {
	case PortableFactBundleSchemaV1:
		return PortableFactRecordSchemaV1, nil
	case PortableFactBundleSchemaV2:
		return PortableFactRecordSchemaV2, nil
	default:
		return "", fmt.Errorf("%w: unsupported portable fact bundle schema %q", ErrRecoveryRecordInvalid, bundleSchema)
	}
}

// verifyExactObjectReadback authenticates the bytes returned by the reader
// itself. Repository.Verify is useful backend evidence, but cannot substitute
// for hashing the stream that a clean reader will consume.
func verifyExactObjectReadback(ctx context.Context, repo repository.Driver, contentID string, expectedLength int64) error {
	if repo == nil {
		return errors.New("repository is required")
	}
	if !validExactContentID(contentID) || expectedLength < 0 {
		return errors.New("exact object identity is invalid")
	}
	body, err := repo.Open(ctx, contentID)
	if err != nil {
		return err
	}
	digest := sha256.New()
	length, readErr := io.Copy(digest, body)
	closeErr := body.Close()
	if readErr != nil {
		return readErr
	}
	if closeErr != nil {
		return closeErr
	}
	if length != expectedLength {
		return fmt.Errorf("length mismatch: got %d want %d", length, expectedLength)
	}
	got := repository.AlgorithmSHA256 + ":" + hex.EncodeToString(digest.Sum(nil))
	if got != contentID {
		return fmt.Errorf("digest mismatch: got %s want %s", got, contentID)
	}
	return nil
}

// ListPortableFactClosures reads the authenticated complete-state fact child
// without consulting SQLite. An empty result is an explicit incomplete-facts
// outcome, rather than a healthy empty catalog.
func (s *Service) ListPortableFactClosures(ctx context.Context, snapshotRef string) ([]PortableFactClosureEnvelope, error) {
	if s == nil || s.TrustAnchor == nil || strings.TrimSpace(s.PublicationDomain) == "" {
		return nil, errors.New("portable fact discovery requires trust anchor and publication domain")
	}
	driver, err := s.publicationRecordDriver()
	if err != nil {
		return nil, err
	}
	publications, err := s.committedPublications(ctx)
	if err != nil {
		return nil, err
	}
	var result []PortableFactClosureEnvelope
	for _, publication := range publications {
		if snapshotRef != "" && publication.Commit.SnapshotRef != snapshotRef {
			continue
		}
		closures, err := listPortableFactClosures(ctx, s.Repo, driver, *s.TrustAnchor, s.PublicationDomain, publication.CommitDigest)
		if err != nil {
			return nil, err
		}
		result = append(result, closures...)
	}
	if len(result) == 0 {
		return nil, errors.New("portable fact closure is unavailable")
	}
	return result, nil
}

func validatePortableFactRecords(bundle portableFactBundle) error {
	v2 := bundle.Schema == PortableFactBundleSchemaV2
	mappings := make(map[string]struct{})
	attachments := make(map[string]struct{}, len(bundle.Attachments))
	attachmentByID := make(map[string]portableFactAttachment, len(bundle.Attachments))
	usedAttachments := make(map[string]struct{}, len(bundle.Attachments))
	namespaceRootID := ""
	sourceID := ""
	for _, attachment := range bundle.Attachments {
		if _, exists := attachments[attachment.AttachmentID]; exists || strings.TrimSpace(attachment.AttachmentID) == "" ||
			strings.TrimSpace(attachment.Purpose) == "" || strings.TrimSpace(attachment.MediaType) == "" ||
			strings.TrimSpace(attachment.ReaderProfile) == "" || strings.TrimSpace(attachment.RepositoryID) == "" ||
			!validExactContentID(attachment.ContentID) || attachment.LogicalLength < 0 {
			return errors.New("portable fact attachment descriptor is invalid or duplicated")
		}
		attachments[attachment.AttachmentID] = struct{}{}
		attachmentByID[attachment.AttachmentID] = attachment
	}
	for _, record := range bundle.Records {
		if record.RecordKind == "SUBJECT_MAPPING" {
			var mapping subjectMappingPayload
			if err := decodeStrictRecord(record.Payload, &mapping); err != nil || mapping.WorkspaceID != bundle.WorkspaceID || mapping.NamespaceRootID == "" || mapping.SourceID == "" || len(mapping.RawPath) == 0 || len(mapping.RawName) == 0 || mapping.DisplayName == "" || mapping.EntryType == "" || mapping.Protection.Mode == "" || mapping.Protection.Outcome == "" || mapping.SelectedRepresentationRefs == nil {
				return errors.New("portable subject mapping is incomplete")
			}
			if record.RecordID != mapping.NamespaceEntryID || !sameStrings(mapping.SelectedRepresentationRefs, portableSelectedRepresentationRefs(mapping.Protection)) {
				return errors.New("portable subject mapping identity or representation set is invalid")
			}
			if v2 {
				if strings.TrimSpace(mapping.StableSubjectRef) == "" || mapping.StableSubjectRef != record.StableSubjectRef {
					return errors.New("portable v2 subject mapping stable identity is invalid")
				}
			} else if mapping.StableSubjectRef != "" || mapping.NamespaceEntryID != record.StableSubjectRef {
				return errors.New("portable v1 subject mapping identity is invalid")
			}
			if namespaceRootID == "" {
				namespaceRootID = mapping.NamespaceRootID
			} else if mapping.NamespaceRootID != namespaceRootID {
				return errors.New("portable subject mappings use multiple namespace roots")
			}
			if sourceID == "" {
				sourceID = mapping.SourceID
			} else if mapping.SourceID != sourceID {
				return errors.New("portable subject mappings use multiple sources")
			}
			if _, exists := mappings[record.StableSubjectRef]; exists {
				return errors.New("portable subject mapping is duplicated")
			}
			mappings[record.StableSubjectRef] = struct{}{}
		}
	}
	for _, record := range bundle.Records {
		if record.RecordKind == "SUBJECT_MAPPING" {
			continue
		}
		if _, exists := mappings[record.StableSubjectRef]; !exists {
			return fmt.Errorf("portable fact record subject %q has no mapping", record.StableSubjectRef)
		}
		switch record.RecordKind {
		case "METADATA_FACT":
			if portableCaptureFactRecord(record) {
				var value ManifestPortableFact
				if err := decodeStrictRecord(record.Payload, &value); err != nil || strings.TrimSpace(value.Name) == "" || !validPortableFactState(value.State) || strings.TrimSpace(value.SourceProfile) == "" || strings.TrimSpace(value.Authority) == "" || value.CapturedAt.IsZero() || strings.TrimSpace(value.CaptureTimeBasis) == "" || len(value.Value) == 0 || !json.Valid(value.Value) || value.ProvenanceDigest == "" {
					return errors.New("portable captured fact payload is invalid")
				}
				if err := validatePortableFactValue(value); err != nil {
					return fmt.Errorf("portable captured fact payload: %w", err)
				}
				break
			}
			var value sqlite.MetadataFact
			if err := decodeStrictRecord(record.Payload, &value); err != nil || value.ID != record.RecordID || value.WorkspaceID != bundle.WorkspaceID || value.WorkspaceID != record.WorkspaceID || value.SubjectRef != record.StableSubjectRef || value.Revision != record.Revision {
				return errors.New("portable metadata fact payload is invalid")
			}
		case "ANNOTATION_REVISION":
			var value sqlite.AnnotationRevision
			if err := decodeStrictRecord(record.Payload, &value); err != nil || value.ID != record.RecordID || value.WorkspaceID != bundle.WorkspaceID || value.SubjectRef != record.StableSubjectRef || value.Revision != record.Revision || value.PredecessorID != record.PredecessorRecordID || !validExactContentID(value.BodyDigest) || DigestBytes([]byte(value.Body)) != value.BodyDigest {
				return errors.New("portable annotation revision payload is invalid")
			}
		case "DESCRIPTION_REVISION":
			var value descriptionPortablePayload
			if err := decodeStrictRecord(record.Payload, &value); err != nil || value.ID != record.RecordID || value.WorkspaceID != bundle.WorkspaceID || value.SubjectRef != record.StableSubjectRef || value.Revision != record.Revision || value.PredecessorID != record.PredecessorRecordID || !validExactContentID(value.BodyDigest) || strings.TrimSpace(value.BodyAttachmentID) == "" || strings.TrimSpace(value.ConfigDigest) == "" || strings.TrimSpace(value.ProducerProfileDigest) == "" {
				return errors.New("portable description revision payload is invalid")
			}
		case "SEMANTIC_SEGMENT":
			var value sqlite.SemanticSegment
			if err := decodeStrictRecord(record.Payload, &value); err != nil || value.ID != record.RecordID || value.WorkspaceID != bundle.WorkspaceID || value.SubjectRef != record.StableSubjectRef || strings.TrimSpace(value.DocumentID) == "" || value.DocumentRevision < 1 || value.Ordinal < 0 || record.Revision != value.Ordinal+1 || strings.TrimSpace(value.Text) == "" || strings.TrimSpace(value.Language) == "" || !validExactContentID(value.TextDigest) || DigestBytes([]byte(value.Text)) != value.TextDigest || strings.TrimSpace(value.SegmentationProfileDigest) == "" {
				return errors.New("portable semantic segment payload is invalid")
			}
		case "PROCESSOR_ARTIFACT_DESCRIPTOR":
			var value artifactPortablePayload
			if err := decodeStrictRecord(record.Payload, &value); err != nil || value.ID != record.RecordID || value.WorkspaceID != bundle.WorkspaceID || value.SubjectRef != record.StableSubjectRef || strings.TrimSpace(value.SchemaRef) == "" || strings.TrimSpace(value.MediaType) == "" || !validExactContentID(value.Digest) || value.ByteLength < 0 || strings.TrimSpace(value.BodyAttachmentID) == "" {
				return errors.New("portable processor artifact descriptor is invalid")
			}
		default:
			return fmt.Errorf("unknown portable fact record kind %q", record.RecordKind)
		}
		if workspaceID, ok := payloadWorkspaceID(record.Payload); ok && workspaceID != bundle.WorkspaceID {
			return errors.New("portable fact payload crosses workspace boundary")
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(record.Payload, &fields); err != nil {
			return err
		}
		requiresBodyAttachment := record.RecordKind == "DESCRIPTION_REVISION" || record.RecordKind == "PROCESSOR_ARTIFACT_DESCRIPTOR"
		raw, hasBodyAttachment := fields["body_attachment_id"]
		if requiresBodyAttachment && !hasBodyAttachment {
			return fmt.Errorf("portable fact %s is missing its body attachment", record.RecordKind)
		}
		if hasBodyAttachment {
			var attachmentID string
			if err := json.Unmarshal(raw, &attachmentID); err != nil {
				return err
			}
			if strings.TrimSpace(attachmentID) == "" {
				return fmt.Errorf("portable fact %s has an empty body attachment", record.RecordKind)
			}
			if _, exists := attachments[attachmentID]; !exists {
				return fmt.Errorf("portable fact body attachment %q is missing", attachmentID)
			}
			if _, exists := usedAttachments[attachmentID]; exists {
				return fmt.Errorf("portable fact body attachment %q is referenced more than once", attachmentID)
			}
			usedAttachments[attachmentID] = struct{}{}
			attachment := attachmentByID[attachmentID]
			if record.RecordKind == "DESCRIPTION_REVISION" && attachment.Purpose != "DESCRIPTION_BODY" {
				return errors.New("description revision attachment purpose is invalid")
			}
			if record.RecordKind == "PROCESSOR_ARTIFACT_DESCRIPTOR" && attachment.Purpose != "PROCESSOR_ARTIFACT_BODY" {
				return errors.New("processor artifact attachment purpose is invalid")
			}
			if record.RecordKind == "DESCRIPTION_REVISION" {
				var description descriptionPortablePayload
				if err := decodeStrictRecord(record.Payload, &description); err != nil || attachment.ContentID != description.BodyDigest || attachment.MediaType != "text/plain; charset=utf-8" || attachment.ReaderProfile != "utf8-v1" {
					return errors.New("description body attachment digest is not bound")
				}
			}
			if record.RecordKind == "PROCESSOR_ARTIFACT_DESCRIPTOR" {
				var artifact artifactPortablePayload
				if err := decodeStrictRecord(record.Payload, &artifact); err != nil || attachment.ContentID != artifact.Digest || attachment.LogicalLength != artifact.ByteLength || attachment.MediaType != artifact.MediaType || attachment.ReaderProfile != "artifact:"+artifact.SchemaRef {
					return errors.New("processor artifact body attachment digest is not bound")
				}
			}
		}
	}
	if len(usedAttachments) != len(attachments) {
		return errors.New("portable fact bundle contains an unreferenced attachment")
	}
	if err := validatePortableRevisionChains(bundle); err != nil {
		return err
	}
	return validatePortableSemanticSegments(bundle)
}

func portableCaptureFactRecord(record portableFactRecord) bool {
	return record.RecordKind == "METADATA_FACT" && strings.Contains(record.RecordID, ":capture:")
}

func portableSelectedRepresentationRefs(protection ManifestProtection) []string {
	selected := make(map[string]struct{})
	if protection.LocalRepresentationID != "" {
		selected[protection.LocalRepresentationID] = struct{}{}
	}
	for _, reference := range protection.RecoveryReferences {
		if reference.RepresentationID != "" {
			selected[reference.RepresentationID] = struct{}{}
		}
	}
	refs := make([]string, 0, len(selected))
	for reference := range selected {
		refs = append(refs, reference)
	}
	sort.Strings(refs)
	return refs
}

func validatePortableSemanticSegments(bundle portableFactBundle) error {
	descriptions := make(map[string]descriptionPortablePayload)
	segmentsByDocument := make(map[string][]sqlite.SemanticSegment)
	for _, record := range bundle.Records {
		switch record.RecordKind {
		case "DESCRIPTION_REVISION":
			var description descriptionPortablePayload
			if err := decodeStrictRecord(record.Payload, &description); err != nil {
				return err
			}
			descriptions[record.RecordID] = description
		case "SEMANTIC_SEGMENT":
			var segment sqlite.SemanticSegment
			if err := decodeStrictRecord(record.Payload, &segment); err != nil {
				return err
			}
			if strings.TrimSpace(segment.DocumentID) == "" || segment.DocumentRevision < 1 || segment.Ordinal < 0 ||
				record.Revision != segment.Ordinal+1 || record.PredecessorRecordID != "" ||
				strings.TrimSpace(segment.Text) == "" || !validExactContentID(segment.TextDigest) ||
				segment.TextDigest != DigestBytes([]byte(segment.Text)) || strings.TrimSpace(segment.SegmentationProfileDigest) == "" {
				return errors.New("portable semantic segment identity, ordinal, or text digest is invalid")
			}
			var provenance struct {
				DescriptionDocumentID     string `json:"description_document_id"`
				DocumentRevision          int64  `json:"document_revision"`
				SegmentationProfileDigest string `json:"segmentation_profile_digest"`
			}
			if err := decodeStrictRecord(record.Provenance, &provenance); err != nil || provenance.DescriptionDocumentID != segment.DocumentID || provenance.DocumentRevision != segment.DocumentRevision || provenance.SegmentationProfileDigest != segment.SegmentationProfileDigest {
				return errors.New("portable semantic segment provenance is invalid")
			}
			var span portableSemanticSourceSpan
			if err := decodeStrictRecord(segment.SourceSpan, &span); err != nil || span.StartByte < 0 || span.EndByte <= span.StartByte || span.EndByte-span.StartByte != len([]byte(segment.Text)) {
				return errors.New("portable semantic segment source span is invalid")
			}
			segmentsByDocument[segment.DocumentID] = append(segmentsByDocument[segment.DocumentID], segment)
		}
	}
	for documentID, segments := range segmentsByDocument {
		description, exists := descriptions[documentID]
		if !exists {
			return fmt.Errorf("portable semantic segments reference missing description %q", documentID)
		}
		sort.Slice(segments, func(i, j int) bool { return segments[i].Ordinal < segments[j].Ordinal })
		var body strings.Builder
		previousEnd := 0
		profileDigest := ""
		for ordinal, segment := range segments {
			if segment.Ordinal != int64(ordinal) {
				return fmt.Errorf("portable semantic segments for description %q are not contiguous", documentID)
			}
			if segment.SubjectRef != description.SubjectRef || segment.WorkspaceID != description.WorkspaceID || segment.DocumentRevision != description.Revision {
				return fmt.Errorf("portable semantic segment for description %q crosses workspace or subject", documentID)
			}
			if profileDigest == "" {
				profileDigest = segment.SegmentationProfileDigest
			} else if profileDigest != segment.SegmentationProfileDigest {
				return fmt.Errorf("portable semantic segments for description %q use conflicting segmentation profiles", documentID)
			}
			var span portableSemanticSourceSpan
			if err := decodeStrictRecord(segment.SourceSpan, &span); err != nil || span.StartByte != previousEnd {
				return fmt.Errorf("portable semantic segments for description %q have a discontinuous source span", documentID)
			}
			previousEnd = span.EndByte
			body.WriteString(segment.Text)
		}
		if DigestBytes([]byte(body.String())) != description.BodyDigest {
			return fmt.Errorf("portable semantic segments for description %q do not reconstruct its body", documentID)
		}
	}
	return nil
}

type portableRevisionLink struct {
	recordID      string
	workspaceID   string
	subjectRef    string
	logicalID     string
	kind          string
	revision      int64
	predecessorID string
}

func validatePortableRevisionChains(bundle portableFactBundle) error {
	for _, recordKind := range []string{"ANNOTATION_REVISION", "DESCRIPTION_REVISION"} {
		byID := make(map[string]portableRevisionLink)
		successorByPredecessor := make(map[string]string)
		for _, record := range bundle.Records {
			if record.RecordKind != recordKind {
				continue
			}
			if _, exists := byID[record.RecordID]; exists {
				return fmt.Errorf("portable %s record ID %q is duplicated", strings.ToLower(recordKind), record.RecordID)
			}
			link := portableRevisionLink{
				recordID: record.RecordID, workspaceID: record.WorkspaceID,
				subjectRef: record.StableSubjectRef, revision: record.Revision,
			}
			switch recordKind {
			case "ANNOTATION_REVISION":
				var value sqlite.AnnotationRevision
				if err := decodeStrictRecord(record.Payload, &value); err != nil {
					return err
				}
				if strings.TrimSpace(value.AnnotationID) == "" {
					return errors.New("portable annotation revision lacks logical annotation identity")
				}
				link.logicalID = value.AnnotationID
				link.predecessorID = value.PredecessorID
			case "DESCRIPTION_REVISION":
				var value descriptionPortablePayload
				if err := decodeStrictRecord(record.Payload, &value); err != nil {
					return err
				}
				if strings.TrimSpace(value.ID) == "" || strings.TrimSpace(string(value.Kind)) == "" {
					return errors.New("portable description revision lacks logical identity")
				}
				link.kind = string(value.Kind)
				link.predecessorID = value.PredecessorID
			}
			byID[record.RecordID] = link
		}
		for _, link := range byID {
			if link.revision == 1 {
				if link.predecessorID != "" {
					return fmt.Errorf("portable %s revision 1 has a predecessor", strings.ToLower(recordKind))
				}
				continue
			}
			if link.revision < 1 || link.predecessorID == "" {
				return fmt.Errorf("portable %s revision %d lacks a predecessor", strings.ToLower(recordKind), link.revision)
			}
			predecessor, exists := byID[link.predecessorID]
			if !exists {
				return fmt.Errorf("portable %s revision %q has an orphan predecessor %q", strings.ToLower(recordKind), link.recordID, link.predecessorID)
			}
			if predecessor.revision != link.revision-1 {
				return fmt.Errorf("portable %s revision %q predecessor is revision %d, want %d", strings.ToLower(recordKind), link.recordID, predecessor.revision, link.revision-1)
			}
			if predecessor.workspaceID != link.workspaceID || predecessor.subjectRef != link.subjectRef {
				return fmt.Errorf("portable %s revision %q predecessor crosses workspace or subject", strings.ToLower(recordKind), link.recordID)
			}
			if recordKind == "ANNOTATION_REVISION" && predecessor.logicalID != link.logicalID {
				return fmt.Errorf("portable annotation revision %q predecessor belongs to another annotation", link.recordID)
			}
			if recordKind == "DESCRIPTION_REVISION" && predecessor.kind != link.kind {
				return fmt.Errorf("portable description revision %q predecessor belongs to another logical document", link.recordID)
			}
			if successor, exists := successorByPredecessor[link.predecessorID]; exists && successor != link.recordID {
				return fmt.Errorf("portable %s predecessor %q has multiple successors", strings.ToLower(recordKind), link.predecessorID)
			}
			successorByPredecessor[link.predecessorID] = link.recordID
		}
	}
	return nil
}

func validatePortableFactRecordsAgainstManifest(bundle portableFactBundle, manifest Manifest) error {
	if err := validatePortableFactRecords(bundle); err != nil {
		return err
	}
	if manifest.SnapshotRef != bundle.SnapshotRef {
		return errors.New("portable fact manifest snapshot binding mismatch")
	}
	v2 := bundle.Schema == PortableFactBundleSchemaV2

	expectedByPath := make(map[string]ManifestEntry, len(manifest.Entries))
	seenManifestPaths := make(map[string]struct{}, len(manifest.Entries))
	for _, entry := range manifest.Entries {
		key := portableRawPathKey(entry.RawPath)
		if _, exists := seenManifestPaths[key]; exists {
			return errors.New("portable manifest contains duplicate raw paths")
		}
		seenManifestPaths[key] = struct{}{}
		// The namespace root directory is provenance, not a mapped subject:
		// buildPortableFactBundle maps the subtree below the root, so the root
		// entry itself is intentionally absent from SUBJECT_MAPPING records.
		if key == "." && sqlite.NamespaceEntryType(entry.EntryType) == sqlite.EntryDirectory {
			continue
		}
		expectedByPath[key] = entry
	}

	seenLogical := make(map[string]struct{}, len(bundle.Records))
	mappingsByPath := make(map[string]subjectMappingPayload, len(expectedByPath))
	recordsByID := make(map[string][]portableFactRecord)
	for _, record := range bundle.Records {
		logicalKey := record.RecordKind + "\x00" + record.RecordID + "\x00" + fmt.Sprint(record.Revision)
		if _, exists := seenLogical[logicalKey]; exists {
			return errors.New("duplicate portable fact logical key")
		}
		seenLogical[logicalKey] = struct{}{}
		recordsByID[record.RecordKind+"\x00"+record.RecordID] = append(recordsByID[record.RecordKind+"\x00"+record.RecordID], record)
		if record.RecordKind == "DESCRIPTION_REVISION" {
			var description descriptionPortablePayload
			if err := decodeStrictRecord(record.Payload, &description); err != nil {
				return err
			}
			if strings.TrimSpace(description.ConfigDigest) == "" || description.ConfigDigest != manifest.ConfigDigest {
				return fmt.Errorf("portable description %q config binding differs from manifest", record.RecordID)
			}
		}
		if record.RecordKind != "SUBJECT_MAPPING" {
			continue
		}
		var mapping subjectMappingPayload
		if err := decodeStrictRecord(record.Payload, &mapping); err != nil {
			return err
		}
		pathKey := portableRawPathKey(mapping.RawPath)
		entry, exists := expectedByPath[pathKey]
		if !exists {
			return fmt.Errorf("portable subject mapping path %q is absent from manifest", pathKey)
		}
		if _, exists := mappingsByPath[pathKey]; exists {
			return fmt.Errorf("portable subject mapping path %q is duplicated", pathKey)
		}
		if record.RecordID != mapping.NamespaceEntryID || (v2 && (mapping.StableSubjectRef == "" || record.StableSubjectRef != mapping.StableSubjectRef)) || (!v2 && (mapping.StableSubjectRef != "" || record.StableSubjectRef != mapping.NamespaceEntryID)) {
			return errors.New("portable subject mapping outer identity is not bound")
		}
		if !bytes.Equal(mapping.RawPath, entry.RawPath) || !bytes.Equal(mapping.RawName, entry.RawName) ||
			mapping.EntryType != entry.EntryType || mapping.ContentID != entry.ContentID ||
			!sameOptionalInt64(mapping.LogicalLength, entry.LogicalSize) ||
			!bytes.Equal(mapping.MetadataBefore, entry.MetadataBefore) || !bytes.Equal(mapping.MetadataAfter, entry.MetadataAfter) {
			return fmt.Errorf("portable subject mapping for %q differs from manifest", pathKey)
		}
		if !sameCanonicalJSON(mapping.Protection, entry.Protection) {
			return fmt.Errorf("portable subject mapping protection for %q differs from manifest", pathKey)
		}
		mappingsByPath[pathKey] = mapping
	}
	if len(mappingsByPath) != len(expectedByPath) {
		return fmt.Errorf("portable subject mappings cover %d of %d manifest entries", len(mappingsByPath), len(expectedByPath))
	}
	for pathKey, mapping := range mappingsByPath {
		parentPath, nested := portableParentRawPath([]byte(pathKey))
		if !nested {
			if mapping.ParentSubjectRef != "" {
				return fmt.Errorf("portable root subject %q has a parent", pathKey)
			}
			continue
		}
		parent, exists := mappingsByPath[parentPath]
		wantParent := parent.NamespaceEntryID
		if v2 {
			wantParent = parent.StableSubjectRef
		}
		childSubject := mapping.NamespaceEntryID
		if v2 {
			childSubject = mapping.StableSubjectRef
		}
		if !exists || mapping.ParentSubjectRef == "" || mapping.ParentSubjectRef == childSubject || mapping.ParentSubjectRef != wantParent {
			return fmt.Errorf("portable subject mapping %q has an invalid parent reference", pathKey)
		}
	}

	expectedCaptureIDs := make(map[string]struct{})
	for pathKey, entry := range expectedByPath {
		mapping := mappingsByPath[pathKey]
		if entry.Facts == nil {
			return fmt.Errorf("portable manifest entry %q lacks captured facts", pathKey)
		}
		for _, fact := range entry.Facts.Facts {
			captureID := mapping.NamespaceEntryID + ":capture:" + fact.Name
			expectedCaptureIDs[captureID] = struct{}{}
			candidates := recordsByID["METADATA_FACT\x00"+captureID]
			if len(candidates) != 1 {
				return fmt.Errorf("portable captured fact %q is missing or duplicated", captureID)
			}
			record := candidates[0]
			wantSubject := mapping.NamespaceEntryID
			if v2 {
				wantSubject = mapping.StableSubjectRef
			}
			if record.StableSubjectRef != wantSubject || record.Revision != 1 || record.PredecessorRecordID != "" {
				return fmt.Errorf("portable captured fact %q identity is invalid", captureID)
			}
			wantPayload, err := CanonicalJSON(fact)
			if err != nil || !bytes.Equal(record.Payload, wantPayload) {
				return fmt.Errorf("portable captured fact %q payload differs from manifest", captureID)
			}
			wantProvenance, err := CanonicalJSON(map[string]any{
				"source_profile": fact.SourceProfile, "authority": fact.Authority,
				"capture_time": fact.CapturedAt, "provenance_digest": fact.ProvenanceDigest,
			})
			if err != nil || !bytes.Equal(record.Provenance, wantProvenance) {
				return fmt.Errorf("portable captured fact %q provenance differs from manifest", captureID)
			}
		}
	}
	for _, record := range bundle.Records {
		if record.RecordKind == "METADATA_FACT" && strings.Contains(record.RecordID, ":capture:") {
			if _, exists := expectedCaptureIDs[record.RecordID]; !exists {
				return fmt.Errorf("portable metadata fact %q is not an admitted capture fact", record.RecordID)
			}
		}
	}
	return nil
}

func portableRawPathKey(raw []byte) string {
	if len(raw) == 0 {
		return "."
	}
	return string(raw)
}

func portableParentRawPath(raw []byte) (string, bool) {
	index := bytes.LastIndexByte(raw, '/')
	if index < 0 {
		return "", false
	}
	if index == 0 {
		return ".", true
	}
	return string(raw[:index]), true
}

func sameOptionalInt64(left, right *int64) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func sameCanonicalJSON(left, right any) bool {
	leftBytes, leftErr := CanonicalJSON(left)
	rightBytes, rightErr := CanonicalJSON(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftBytes, rightBytes)
}

func payloadWorkspaceID(payload []byte) (string, bool) {
	var value map[string]json.RawMessage
	if err := json.Unmarshal(payload, &value); err != nil {
		return "", false
	}
	for _, key := range []string{"workspace_id", "WorkspaceID"} {
		if raw, ok := value[key]; ok {
			var id string
			if json.Unmarshal(raw, &id) == nil && id != "" {
				return id, true
			}
		}
	}
	for _, raw := range value {
		if id, ok := payloadWorkspaceID(raw); ok {
			return id, true
		}
	}
	return "", false
}

func validateProcessorAttachmentChild(ctx context.Context, driver repository.RecordDriver, anchor TrustAnchor, closure PortableFactClosureRecord, bundle portableFactBundle) error {
	hasArtifact := false
	for _, record := range bundle.Records {
		if record.RecordKind == "PROCESSOR_ARTIFACT_DESCRIPTOR" {
			hasArtifact = true
			break
		}
	}
	if !hasArtifact && closure.ProcessorAttemptDigest == "" {
		return nil
	}
	if closure.ProcessorAttemptDigest == "" {
		return errors.New("portable processor artifact facts lack processor-attempt child")
	}
	payload, err := readRecord(ctx, driver, repository.RecordProcessorAttemptClosure, closure.ProcessorAttemptDigest)
	if err != nil {
		return err
	}
	var envelope ProcessorAttemptClosureEnvelope
	if err := decodeStrictRecord(payload, &envelope); err != nil {
		return err
	}
	if err := envelope.Closure.Verify(anchor); err != nil {
		return err
	}
	if envelope.Schema != ProcessorAttemptClosureEnvelopeSchemaV1 ||
		envelope.Closure.ParentCommitDigest != closure.ParentCommitDigest ||
		envelope.Closure.WorkspaceID != closure.WorkspaceID ||
		envelope.Closure.PublicationID != closure.PublicationID ||
		envelope.Closure.PublicationDomain != closure.PublicationDomain ||
		envelope.Closure.SnapshotRef != closure.SnapshotRef ||
		envelope.Closure.ManifestDigest != closure.ManifestDigest ||
		envelope.Closure.TargetIdentity != closure.TargetIdentity ||
		envelope.Closure.FenceToken != closure.FenceToken ||
		DigestBytes(payload) != closure.ProcessorAttemptDigest {
		return errors.New("portable processor-attempt child binding mismatch")
	}
	attempts, err := validateProcessorAttemptBundle(envelope.Bundle, closure.WorkspaceID, closure.SnapshotRef)
	if err != nil || attempts.Schema != envelope.Closure.AttemptBundleSchema ||
		DigestBytes(envelope.Bundle) != envelope.Closure.AttemptBundleDigest ||
		int64(len(envelope.Bundle)) != envelope.Closure.AttemptBundleLength ||
		int64(len(attempts.Attempts)) != envelope.Closure.AttemptCount {
		return errors.New("portable processor-attempt bundle binding mismatch")
	}
	artifactAttempts := make(map[string]sqlite.ProcessorAttemptExport)
	for _, attempt := range attempts.Attempts {
		for _, artifactID := range attempt.ArtifactRefs {
			if _, exists := artifactAttempts[artifactID]; exists {
				return errors.New("portable processor artifact is referenced by multiple attempts")
			}
			artifactAttempts[artifactID] = attempt
		}
	}
	for _, record := range bundle.Records {
		if record.RecordKind != "PROCESSOR_ARTIFACT_DESCRIPTOR" {
			continue
		}
		var artifact artifactPortablePayload
		if err := decodeStrictRecord(record.Payload, &artifact); err != nil {
			return err
		}
		attempt, exists := artifactAttempts[artifact.ID]
		if !exists || attempt.AttemptID != artifact.AttemptID || attempt.SubjectRef != artifact.SubjectRef || attempt.FenceToken != artifact.FenceToken || attempt.ProcessorDigest != artifact.ProducerDigest {
			return errors.New("portable processor artifact is not admitted by its authenticated attempt")
		}
		delete(artifactAttempts, artifact.ID)
	}
	if len(artifactAttempts) != 0 {
		return errors.New("authenticated processor attempt names a missing portable artifact descriptor")
	}
	return nil
}

func validatePortableFactBundle(payload []byte, workspaceID, snapshotRef string) (portableFactBundle, error) {
	var bundle portableFactBundle
	if err := decodeStrictRecord(payload, &bundle); err != nil {
		return bundle, err
	}
	if bundle.Schema != PortableFactBundleSchemaV1 && bundle.Schema != PortableFactBundleSchemaV2 || bundle.WorkspaceID != workspaceID || bundle.SnapshotRef != snapshotRef || bundle.Records == nil || bundle.Attachments == nil {
		return bundle, errors.New("portable fact bundle binding is invalid")
	}
	recordSchema, err := portableFactRecordSchema(bundle.Schema)
	if err != nil {
		return bundle, err
	}
	canonical, err := CanonicalJSON(bundle)
	if err != nil || !bytes.Equal(payload, canonical) {
		return bundle, errors.New("portable fact bundle is not canonical")
	}
	previous := ""
	seen := make(map[string]string)
	for _, record := range bundle.Records {
		if record.Schema != recordSchema || record.WorkspaceID != workspaceID || record.SnapshotRef != snapshotRef || strings.TrimSpace(record.RecordID) == "" || strings.TrimSpace(record.StableSubjectRef) == "" || record.Revision < 1 || !validExactContentID(record.PayloadDigest) || record.PayloadLength < 0 || len(record.Payload) == 0 || len(record.Provenance) == 0 || !json.Valid(record.Payload) || !json.Valid(record.Provenance) || DigestBytes(record.Payload) != record.PayloadDigest || int64(len(record.Payload)) != record.PayloadLength {
			return bundle, errors.New("portable fact record is invalid")
		}
		key := record.RecordKind + "\x00" + record.RecordID + "\x00" + fmt.Sprint(record.Revision)
		if old, ok := seen[key]; ok && old != record.PayloadDigest {
			return bundle, errors.New("duplicate portable fact logical key has conflicting bytes")
		}
		seen[key] = record.PayloadDigest
		order := record.RecordKind + "\x00" + record.RecordID + "\x00" + fmt.Sprintf("%020d", record.Revision)
		if previous != "" && order <= previous {
			return bundle, errors.New("portable fact records are not sorted")
		}
		previous = order
	}
	return bundle, nil
}
