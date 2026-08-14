package command

const (
	OpStatusGet          = "status.get"
	OpCapabilityList     = "capability.list"
	OpSnapshotList       = "snapshot.list"
	OpSnapshotDiff       = "snapshot.diff"
	OpSnapshotVerify     = "snapshot.verify"
	OpPlanIngest         = "plan.ingest"
	OpPlanRevise         = "plan.revise"
	OpPlanAbandon        = "plan.abandon"
	OpPlanRestore        = "plan.restore"
	OpPlanGet            = "plan.get"
	OpPlanApply          = "plan.apply"
	OpJobEvents          = "job.events"
	OpJobCancel          = "job.cancel"
	OpRecoveryExport     = "recovery.export"
	OpNamespaceList      = "namespace.list"
	OpNamespaceResolve   = "namespace.resolve"
	OpNamespaceStat      = "namespace.stat"
	OpNamespaceReadlink  = "namespace.readlink"
	OpRepresentationList = "representation.list"
	OpContentOpen        = "content.open"
	OpContentRead        = "content.read"
	OpContentClose       = "content.close"
	OpAnnotationList     = "annotation.list"
	OpAnnotationUpsert   = "annotation.upsert"
	OpAnnotationDelete   = "annotation.delete"
	OpAnnotationExport   = "annotation.export"
	OpAnnotationImport   = "annotation.import"
	OpSearchQuery        = "search.query"
	OpGatewayMount       = "gateway.mount"
	OpGatewayUnmount     = "gateway.unmount"
	OpAudioList          = "audio.list"
	OpBooksList          = "books.list"
)

func KnownOperations() []string {
	return []string{
		OpStatusGet,
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
		OpGatewayMount,
		OpGatewayUnmount,
		OpAudioList,
		OpBooksList,
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
