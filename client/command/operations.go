package command

const (
	OpStatusGet            = "status.get"
	OpDoctorCheck          = "doctor.check"
	OpCapabilityList       = "capability.list"
	OpSnapshotList         = "snapshot.list"
	OpSnapshotDiff         = "snapshot.diff"
	OpSnapshotVerify       = "snapshot.verify"
	OpPlanIngest           = "plan.ingest"
	OpPlanRevise           = "plan.revise"
	OpPlanAbandon          = "plan.abandon"
	OpPlanRestore          = "plan.restore"
	OpPlanGet              = "plan.get"
	OpPlanApply            = "plan.apply"
	OpJobEvents            = "job.events"
	OpJobCancel            = "job.cancel"
	OpRecoveryExport       = "recovery.export"
	OpRecoveryAnchorExport = "recovery.anchor.export"
	OpNamespaceList        = "namespace.list"
	OpNamespaceResolve     = "namespace.resolve"
	OpNamespaceStat        = "namespace.stat"
	OpNamespaceReadlink    = "namespace.readlink"
	OpRepresentationList   = "representation.list"
	OpContentOpen          = "content.open"
	OpContentRead          = "content.read"
	OpContentClose         = "content.close"
	OpAnnotationList       = "annotation.list"
	OpAnnotationUpsert     = "annotation.upsert"
	OpAnnotationDelete     = "annotation.delete"
	OpAnnotationExport     = "annotation.export"
	OpAnnotationImport     = "annotation.import"
	OpSearchQuery          = "search.query"
	OpAudioList            = "audio.list"
	OpBooksList            = "books.list"
	OpDescriptionList      = "description.list"
	OpDescriptionGet       = "description.get"
	OpDescriptionCreate    = "description.create"
	OpRecoveryImport       = "recovery.import"
	OpRecoveryTokenExport  = "recovery.token.export"
	OpViewSave             = "view.save"
	OpViewGet              = "view.get"
	OpViewEvaluate         = "view.evaluate"
	OpViewList             = "view.list"
	OpExportPlan           = "export.plan"
	OpExportApply          = "export.apply"
	OpExportVerify         = "export.verify"
)

func KnownOperations() []string {
	return []string{
		OpStatusGet,
		OpDoctorCheck,
		OpCapabilityList,
		OpSnapshotList,
		OpSnapshotDiff,
		OpSnapshotVerify,
		OpPlanIngest,
		OpPlanRevise,
		OpPlanAbandon,
		OpPlanRestore,
		OpPlanGet,
		OpPlanApply,
		OpJobEvents,
		OpJobCancel,
		OpRecoveryExport,
		OpRecoveryAnchorExport,
		OpNamespaceList,
		OpNamespaceResolve,
		OpNamespaceStat,
		OpNamespaceReadlink,
		OpRepresentationList,
		OpContentOpen,
		OpContentRead,
		OpContentClose,
		OpAnnotationList,
		OpAnnotationUpsert,
		OpAnnotationDelete,
		OpAnnotationExport,
		OpAnnotationImport,
		OpSearchQuery,
		OpAudioList,
		OpBooksList,
		OpDescriptionList,
		OpDescriptionGet,
		OpDescriptionCreate,
		OpRecoveryImport,
		OpRecoveryTokenExport,
		OpViewSave,
		OpViewGet,
		OpViewEvaluate,
		OpViewList,
		OpExportPlan,
		OpExportApply,
		OpExportVerify,
	}
}

func IsKnown(operation string) bool {
	for _, candidate := range KnownOperations() {
		if candidate == operation {
			return true
		}
	}
	return false
}
