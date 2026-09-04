import { useEffect, useMemo, useRef, useState } from "react";
import type { FormEvent, ReactNode } from "react";
import { createRoot } from "react-dom/client";
import { AlertCircle, ArchiveRestore, ArrowLeft, Check, CheckCircle2, ChevronRight, Database, Download as DownloadIcon, FileSearch, FileText, Folder, HardDrive, Hash, Home, KeyRound, LoaderCircle, Pencil, Plus, RefreshCw, Save, Search, Server, Settings, ShieldCheck, Sparkles, Tag, Trash2, X } from "lucide-react";
import { abbreviateIdentity, identityBars } from "./identity";
import { createTranslator, resolveInitialLocale } from "./i18n";
import type { Locale, Translator } from "./i18n";
import "./styles.css";

type Result = { status?: string; data?: any; reasons?: Array<{ message?: string }> };
type SearchSegment = {
  description_document_id?: string;
  source_type?: string;
  source_id?: string;
  semantic_segment_id?: string;
  ordinal?: number;
  matched_text?: string;
  kind?: string;
  producer?: string;
  accepted?: boolean;
  language?: string;
};
type SearchHit = { subject_ref?: string; entry_id?: string; name?: string; path?: string; entry_type?: string; content_id?: string; logical_size?: number; segments?: SearchSegment[]; index_status?: { lexical?: string; semantic?: string; tags?: string } };
type AnnotationRecord = { annotation_id?: string; subject_ref?: string; kind?: string; body?: string; tombstoned?: boolean; revision?: number; updated_at?: string };
type SnapshotSummary = { snapshot_ref?: string; workspace_id?: string; namespace_root_id?: string };
type SourceScan = { scan_ref?: string; generation?: number; state?: string; full_traversal?: boolean; started_at?: string; finished_at?: string; entries?: number; regular_files?: number; directories?: number; symlinks?: number; special_files?: number; bytes_hashed?: number; failed_entries?: number; unstable_entries?: number; detection_failures?: number };
type SourceSummary = { source_ref?: string; kind?: string; locator?: string; state?: string; reachability?: string; reachability_checked_at?: string; reachability_message?: string; latest_scan?: SourceScan; latest_snapshot_ref?: string; latest_namespace_root_id?: string };
type SourceProjection = { state: "READY" | "DEGRADED"; message?: string };
type ActionFeedback = { state: "IDLE" | "RUNNING" | "SUCCEEDED" | "DEGRADED" | "FAILED"; message?: string };
type DetailResourceStatus = "idle" | "loading" | "ready" | "error";
type DetailState = { subjectRef: string; annotations: DetailResourceStatus; representations: DetailResourceStatus; descriptions: DetailResourceStatus };
type DetailCollection = { status: DetailResourceStatus; items: any[] };
type BrowseCrumb = { subject_ref: string; entry_id: string; name: string };
type BrowseRoot = { root_id: string; name: string; source_path?: string };
type SettingsSection = "storage" | "search" | "descriptions" | "recovery" | "service";
type ViewMode = "content" | "tag" | "path" | "search";
type SystemFacetState = { type: string; format: string; dedup: string; tags: string; index: string };
const apiBase = "/api/v1";
const lexicalDimension = "lexical-metadata-fts";
const semanticDimension = "semantic-embedding";
const semanticProvider = "query.semantic.onnx-bge-zvec.v1";

async function command(operation: string, input: unknown): Promise<Result> {
  const clientMessage = (message: string) => createTranslator(resolveInitialLocale())(message);
  let response: Response;
  try {
    response = await fetch(`${apiBase}/command`, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ operation, input }) });
  } catch {
    throw new Error(clientMessage("Could not connect to RestoreWeave service. Start the service, then refresh."));
  }
  const raw = await response.text();
  let payload: Result = {};
  if (raw.trim()) {
    try {
      payload = JSON.parse(raw) as Result;
    } catch {
      throw new Error(clientMessage(response.ok ? "RestoreWeave returned an invalid response." : "Could not connect to RestoreWeave service. Start the service, then refresh."));
    }
  }
  if (!response.ok || !payload.status || !["SUCCEEDED", "DEGRADED"].includes(payload.status)) {
    const reason = payload.reasons?.[0]?.message;
    if (reason) throw new Error(reason);
    if (!response.ok) throw new Error(clientMessage("Could not connect to RestoreWeave service. Start the service, then refresh."));
    throw new Error(clientMessage("RestoreWeave returned an invalid response."));
  }
  return payload;
}

function subjectBoundDetailCollection(result: PromiseSettledResult<Result>, key: "annotations" | "documents" | "representations", subjectRef: string): DetailCollection {
  if (result.status !== "fulfilled" || result.value.status !== "SUCCEEDED") return { status: "error", items: [] };
  const data = result.value.data;
  const items = data?.[key];
  if (!Array.isArray(items)) return { status: "error", items: [] };
  if (key === "representations") {
    return typeof data?.subject_ref === "string" && data.subject_ref.trim() === subjectRef
      ? { status: "ready", items }
      : { status: "error", items: [] };
  }
  return items.every((item) => typeof item?.subject_ref === "string" && item.subject_ref.trim() === subjectRef)
    ? { status: "ready", items }
    : { status: "error", items: [] };
}

function annotationBelongsToSubject(annotation: unknown, subjectRef: string): annotation is AnnotationRecord {
  return Boolean(subjectRef && typeof annotation === "object" && annotation && typeof (annotation as AnnotationRecord).subject_ref === "string" && (annotation as AnnotationRecord).subject_ref?.trim() === subjectRef);
}

function App() {
  const [locale, setLocale] = useState<Locale>(resolveInitialLocale);
  const [query, setQuery] = useState("");
  const [source, setSource] = useState("");
  const [workspaceID, setWorkspaceID] = useState(() => window.localStorage.getItem("restoreweave.workspace_id") ?? "");
  const [hits, setHits] = useState<SearchHit[]>([]);
  const [libraryItems, setLibraryItems] = useState<SearchHit[]>([]);
  const [selected, setSelected] = useState<SearchHit>();
  const [plan, setPlan] = useState<any>();
  const [annotations, setAnnotations] = useState<any[]>([]);
  const [representations, setRepresentations] = useState<any[]>([]);
  const [descriptions, setDescriptions] = useState<any[]>([]);
  const [descriptionsTruncated, setDescriptionsTruncated] = useState(false);
  const [detailState, setDetailState] = useState<DetailState>({ subjectRef: "", annotations: "idle", representations: "idle", descriptions: "idle" });
  const [statusData, setStatusData] = useState<any>();
  const [capabilities, setCapabilities] = useState<any[]>([]);
  const [sources, setSources] = useState<SourceSummary[]>([]);
  const [sourceProjection, setSourceProjection] = useState<SourceProjection>({ state: "READY" });
  const [recheckSourceRef, setRecheckSourceRef] = useState("");
  const [status, setStatus] = useState("Checking service");
  const [notice, setNotice] = useState<{ kind: "success" | "error" | "warning"; text: string }>();
  const [busy, setBusy] = useState(false);
  const [addOpen, setAddOpen] = useState(false);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [settingsSection, setSettingsSection] = useState<SettingsSection>("storage");
  const [configData, setConfigData] = useState<any>();
  const [configDraft, setConfigDraft] = useState<any>();
  const [configLoading, setConfigLoading] = useState(false);
  const [semanticInstall, setSemanticInstall] = useState<ActionFeedback>({ state: "IDLE" });
  const [searchRebuild, setSearchRebuild] = useState<ActionFeedback>({ state: "IDLE" });
  const [noteDraft, setNoteDraft] = useState("");
  const [tagDraft, setTagDraft] = useState("");
  const [workspaceTags, setWorkspaceTags] = useState<AnnotationRecord[]>([]);
  const [activeTags, setActiveTags] = useState<string[]>([]);
  const [activeSystemFacets, setActiveSystemFacets] = useState<SystemFacetState>({ type: "", format: "", dedup: "", tags: "", index: "" });
  const [editingNoteID, setEditingNoteID] = useState("");
  const [editingBody, setEditingBody] = useState("");
  const [snapshotRef, setSnapshotRef] = useState("");
  const [restoreOpen, setRestoreOpen] = useState(false);
  const [restoreDestination, setRestoreDestination] = useState("");
  const [restorePlan, setRestorePlan] = useState<any>();
  const [viewMode, setViewMode] = useState<ViewMode>("content");
  const [browseRootID, setBrowseRootID] = useState("");
  const [browseRoots, setBrowseRoots] = useState<BrowseRoot[]>([]);
  const [browseCrumbs, setBrowseCrumbs] = useState<BrowseCrumb[]>([]);
  const t = useMemo(() => createTranslator(locale), [locale]);
  const lexicalReady = useMemo(() => capabilities.some((item) => item.kind === "index-dimension" && item.id === lexicalDimension && item.state === "AVAILABLE"), [capabilities]);
  const semanticReady = useMemo(() => capabilities.some((item) => item.kind === "index-dimension" && item.id === semanticDimension && item.source === semanticProvider && item.state === "AVAILABLE"), [capabilities]);
  const semanticBundleCapability = useMemo(() => capabilities.find((item) => item.kind === "model-bundle" && item.id === "bge-small-zh-v1.5"), [capabilities]);
  const semanticBundleReady = useMemo(() => {
    return semanticBundleCapability?.state === "AVAILABLE";
  }, [semanticBundleCapability]);
  const semanticBundleRestartRequired = useMemo(() => semanticBundleReady && /restart required before semantic worker\/index use/i.test(String(semanticBundleCapability?.notes ?? "")), [semanticBundleCapability, semanticBundleReady]);
  const semanticBundleOperatorProvided = useMemo(() => /operator-provided/i.test(String(semanticBundleCapability?.source ?? "")), [semanticBundleCapability]);
  const semanticInstallAvailable = useMemo(() => capabilities.some((item) => item.kind === "operation" && item.id === "semantic.bundle.install" && item.state === "AVAILABLE"), [capabilities]);
  const searchRebuildAvailable = useMemo(() => capabilities.some((item) => item.kind === "operation" && item.id === "search.rebuild" && item.state === "AVAILABLE"), [capabilities]);
  const tagsBySubject = useMemo(() => {
    const result = new Map<string, string[]>();
    for (const tag of workspaceTags) {
      const subject = tag.subject_ref?.trim();
      const value = tag.body?.trim();
      if (!subject || !value || tag.kind !== "TAG" || tag.tombstoned) continue;
      const values = result.get(subject) ?? [];
      if (!values.includes(value)) values.push(value);
      result.set(subject, values);
    }
    for (const values of result.values()) values.sort((left, right) => left.localeCompare(right, locale));
    return result;
  }, [locale, workspaceTags]);
  const identityCounts = useMemo(() => {
    const counts = new Map<string, number>();
    for (const item of libraryItems) {
      const key = exactIdentityKey(item);
      if (key) counts.set(key, (counts.get(key) ?? 0) + 1);
    }
    return counts;
  }, [libraryItems]);
  const facetFilteredItems = useMemo(() => libraryItems.filter((item) => {
    const format = formatFacetValue(item);
    const dedup = dedupFacetValue(item, identityCounts);
    const tagState = item.index_status?.tags;
    const indexStates = [item.index_status?.lexical, item.index_status?.semantic].filter((value): value is string => typeof value === "string" && value.length > 0);
    const indexNotReady = indexStates.length > 0 && indexStates.some((value) => value === "NOT_BUILT" || value === "UNAVAILABLE");
    return (!activeSystemFacets.type || item.entry_type === activeSystemFacets.type) && (!activeSystemFacets.format || format === activeSystemFacets.format) && (!activeSystemFacets.dedup || dedup === activeSystemFacets.dedup) && (!activeSystemFacets.tags || tagState === activeSystemFacets.tags) && (!activeSystemFacets.index || (activeSystemFacets.index === "NOT_READY" && indexNotReady));
  }), [activeSystemFacets, identityCounts, libraryItems]);
  const tagFacets = useMemo(() => {
    const visibleSubjects = new Set(facetFilteredItems.map((item) => item.subject_ref).filter((value): value is string => Boolean(value)));
    const counts = new Map<string, number>();
    for (const [subject, values] of tagsBySubject) {
      if (!visibleSubjects.has(subject)) continue;
      for (const value of values) counts.set(value, (counts.get(value) ?? 0) + 1);
    }
    return [...counts].map(([value, count]) => ({ value, count })).sort((left, right) => left.value.localeCompare(right.value, locale));
  }, [facetFilteredItems, locale, tagsBySubject]);
  const systemFacets = useMemo(() => {
    const types = new Map<string, number>();
    const formats = new Map<string, number>();
    const dedup = new Map<string, number>();
    for (const item of libraryItems) {
      if (item.entry_type) types.set(item.entry_type, (types.get(item.entry_type) ?? 0) + 1);
      const format = formatFacetValue(item);
      if (format) formats.set(format, (formats.get(format) ?? 0) + 1);
      const status = dedupFacetValue(item, identityCounts);
      if (status) dedup.set(status, (dedup.get(status) ?? 0) + 1);
    }
    return {
      types: [...types].map(([value, count]) => ({ value, count })).sort((left, right) => left.value.localeCompare(right.value, locale)),
      formats: [...formats].map(([value, count]) => ({ value, count })).sort((left, right) => left.value.localeCompare(right.value, locale)),
      dedup: ["DUPLICATE", "UNIQUE"].filter((value) => dedup.has(value)).map((value) => ({ value, count: dedup.get(value) ?? 0 })),
      tags: [{ value: "UNMARKED", count: libraryItems.filter((item) => item.index_status?.tags === "UNMARKED").length }].filter((facet) => facet.count > 0),
      index: [{ value: "NOT_READY", count: libraryItems.filter((item) => {
        const states = [item.index_status?.lexical, item.index_status?.semantic].filter((value): value is string => typeof value === "string" && value.length > 0);
        return states.length > 0 && states.some((value) => value === "NOT_BUILT" || value === "UNAVAILABLE");
      }).length }].filter((facet) => facet.count > 0),
    };
  }, [identityCounts, libraryItems, locale]);
  const filteredLibraryItems = useMemo(() => facetFilteredItems.filter((item) => {
    const values = item.subject_ref ? tagsBySubject.get(item.subject_ref) ?? [] : [];
    return activeTags.every((tag) => values.includes(tag));
  }), [activeTags, facetFilteredItems, tagsBySubject]);
  const hasSystemFacets = Boolean(activeSystemFacets.type || activeSystemFacets.format || activeSystemFacets.dedup || activeSystemFacets.tags || activeSystemFacets.index);
  const hasActiveFilters = activeTags.length > 0 || hasSystemFacets;
  const tagVocabulary = useMemo(() => [...new Set(workspaceTags.map((tag) => tag.body?.trim()).filter((value): value is string => Boolean(value)))].sort((left, right) => left.localeCompare(right, locale)), [locale, workspaceTags]);
  const isConnected = status === "Connected";
  const isUnavailable = status === "Unavailable";
  const catalogHealthy = statusData?.catalog?.ok === true;
  const repositoryHealthy = statusData?.repository?.ok === true;
  const repositoryCapacity = statusData?.repository;
  const annotationStorageReady = isConnected && catalogHealthy;
  const coreStorageReady = isConnected && catalogHealthy && repositoryHealthy;
  const coreStorageComponent = [
    !catalogHealthy ? t("Catalog unavailable") : "",
    !repositoryHealthy ? t("Repository unavailable") : "",
  ].filter(Boolean).join("; ");
  const coreStorageMessage = !isConnected
    ? t("Could not connect to RestoreWeave service. Start the service, then refresh.")
    : !statusData
      ? t("Core storage status unavailable")
      : t("Service connected, but core storage is unavailable: {{component}}", { component: coreStorageComponent || t("Core storage status unavailable") });
  const catalogStorageMessage = !isConnected
    ? t("Could not connect to RestoreWeave service. Start the service, then refresh.")
    : !statusData
      ? t("Core storage status unavailable")
      : t("Service connected, but catalog is unavailable.");
  const drawerOpen = addOpen || settingsOpen || restoreOpen;
  const drawerKind = addOpen ? "add" : settingsOpen ? "settings" : restoreOpen ? "restore" : "closed";
  const configDirty = useMemo(() => Boolean(configDraft && configData?.config && JSON.stringify(configDraft) !== JSON.stringify(configData.config)), [configDraft, configData]);
  const settingsGuard = useRef({ open: false, dirty: false, prompt: "" });
  // Capture the invoking control before React renders the modal. This keeps
  // close/escape/backdrop dismissal predictable even when autoFocus runs.
  const drawerTriggerRef = useRef<HTMLElement | null>(null);
  const refreshGenerationRef = useRef(0);
  const detailRequestRef = useRef(0);
  const detailSubjectRef = useRef("");

  function clearCatalogBackedState() {
    setWorkspaceID("");
    window.localStorage.removeItem("restoreweave.workspace_id");
    setSnapshotRef(""); setPlan(undefined); setRestorePlan(undefined); setAnnotations([]); setRepresentations([]); setDescriptions([]); setSources([]); setHits([]); setLibraryItems([]); setSelected(undefined);
    setBrowseRootID(""); setBrowseRoots([]); setBrowseCrumbs([]);
    setActiveTags([]); setActiveSystemFacets({ type: "", format: "", dedup: "", tags: "", index: "" }); setViewMode("content");
    detailRequestRef.current += 1; detailSubjectRef.current = ""; setDetailState({ subjectRef: "", annotations: "idle", representations: "idle", descriptions: "idle" });
  }

  function clearRepositoryDependentState() {
    setSnapshotRef(""); setPlan(undefined); setRestorePlan(undefined);
  }

  async function refresh() {
    const refreshGeneration = ++refreshGenerationRef.current;
    const isCurrentRefresh = () => refreshGenerationRef.current === refreshGeneration;
    setStatus("Checking service");
    const [healthResult, statusResult] = await Promise.allSettled([fetch(`${apiBase}/healthz`), command("status.get", {})]);
    if (!isCurrentRefresh()) return;
    if (healthResult.status !== "fulfilled" || statusResult.status !== "fulfilled") {
      setSourceProjection({ state: "DEGRADED" });
      setCapabilities([]);
      setStatusData(undefined);
      clearCatalogBackedState();
      setStatus("Unavailable");
      return;
    }
    const health = healthResult.value;
    const statusResponse = statusResult.value;
    const serviceConnected = health.ok && statusResponse.status !== "FAILED";
    const nextStatusData = statusResponse.data;
    const nextCatalogHealthy = nextStatusData?.catalog?.ok === true;
    const nextRepositoryHealthy = nextStatusData?.repository?.ok === true;
    const nextCoreStorageReady = serviceConnected && nextCatalogHealthy && nextRepositoryHealthy;
    setStatus(serviceConnected ? "Connected" : "Unavailable");
    setStatusData(nextStatusData);

    if (!serviceConnected || !nextCatalogHealthy) {
      // A reachable HTTP adapter is not evidence that its durable catalog can
      // serve content. Do not wait on catalog-backed follow-up operations
      // before exposing that failure or clearing stale browser projections.
      setCapabilities([]);
      clearCatalogBackedState();
      return;
    }

    const [capabilityResult, snapshotResult] = await Promise.allSettled([command("capability.list", {}), command("snapshot.list", {})]);
    if (!isCurrentRefresh()) return;
    if (capabilityResult.status === "fulfilled") setCapabilities(capabilityResult.value.data?.capabilities ?? []);
    else setCapabilities([]);
    const snapshots = snapshotResult.status === "fulfilled" ? (snapshotResult.value.data?.snapshots ?? []) as SnapshotSummary[] : [];
    const latest = snapshots.at(-1);
    if (nextCoreStorageReady && snapshotResult.status === "fulfilled") setSnapshotRef(latest?.snapshot_ref ?? "");
    else clearRepositoryDependentState();
    // A browser-local workspace id is only a convenience after an explicit
    // ingest; it is not evidence for a newly opened daemon/catalog. Refresh
    // trusts only durable state reported by this daemon.
    const activeWorkspaceID = latest?.workspace_id || nextStatusData?.recent_plans?.[0]?.workspace_id || "";
    if (activeWorkspaceID) rememberWorkspace(activeWorkspaceID);
    if (activeWorkspaceID) {
      try {
        await loadSources(activeWorkspaceID, refreshGeneration);
        await loadContentLibrary(activeWorkspaceID, refreshGeneration);
      } catch (error) {
        if (!isCurrentRefresh()) return;
        setHits([]); setLibraryItems([]); setSelected(undefined); setBrowseRootID(""); setBrowseRoots([]); setBrowseCrumbs([]); setActiveTags([]); setActiveSystemFacets({ type: "", format: "", dedup: "", tags: "", index: "" }); setViewMode("content");
        setNotice({ kind: "warning", text: error instanceof Error ? t("Content library is unavailable: {{message}}", { message: error.message }) : t("Content library is unavailable.") });
      }
    } else {
      clearCatalogBackedState(); setSourceProjection({ state: "READY" });
    }
  }
  async function refreshCapabilities() {
    try {
      const response = await command("capability.list", {});
      setCapabilities(response.data?.capabilities ?? []);
    } catch {
      setCapabilities([]);
    }
  }
  useEffect(() => { void refresh(); }, []);
  useEffect(() => {
    if (!workspaceID) { setWorkspaceTags([]); setActiveTags([]); setActiveSystemFacets({ type: "", format: "", dedup: "", tags: "", index: "" }); return; }
    void loadTagVocabulary(workspaceID);
  }, [workspaceID]);
  useEffect(() => {
    if (viewMode !== "tag") return;
    const subjects = new Set(filteredLibraryItems.map((item) => item.subject_ref));
    setHits(filteredLibraryItems);
    setSelected((current) => current?.subject_ref && subjects.has(current.subject_ref) ? current : undefined);
  }, [filteredLibraryItems, viewMode]);
  useEffect(() => {
    window.localStorage.setItem("restoreweave.locale", locale);
    document.documentElement.lang = locale;
  }, [locale]);
  useEffect(() => {
    settingsGuard.current = { open: settingsOpen, dirty: configDirty, prompt: t("Discard unsaved settings?") };
  }, [configDirty, settingsOpen, t]);
  useEffect(() => {
    if (!drawerOpen) return;
    const previouslyFocused = drawerTriggerRef.current ?? (document.activeElement instanceof HTMLElement ? document.activeElement : undefined);
    document.body.classList.add("drawer-open");
    const focusFirstControl = () => {
      const drawer = document.querySelector<HTMLElement>('[aria-modal="true"]');
      // Prefer the task's first field over the dismiss button. Settings and
      // restore drawers both have an actionable input; falling back to a
      // button keeps empty/error states keyboard reachable.
      const firstField = drawer?.querySelector<HTMLElement>('input:not([disabled]), textarea:not([disabled]), select:not([disabled]), [autofocus]');
      const first = firstField ?? drawer?.querySelector<HTMLElement>('button:not([disabled]), [tabindex]:not([tabindex="-1"])');
      first?.focus();
    };
    requestAnimationFrame(focusFirstControl);
    function handleKeyDown(event: KeyboardEvent) {
      if (event.key === "Escape") {
        const guard = settingsGuard.current;
        if (guard.open && guard.dirty && !window.confirm(guard.prompt)) return;
        setAddOpen(false); setRecheckSourceRef(""); setSettingsOpen(false); setRestoreOpen(false);
        return;
      }
      if (event.key !== "Tab") return;
      const drawer = document.querySelector<HTMLElement>('[aria-modal="true"]');
      const focusable = drawer ? Array.from(drawer.querySelectorAll<HTMLElement>('button:not([disabled]), input:not([disabled]), textarea:not([disabled]), summary, [tabindex]:not([tabindex="-1"])')) : [];
      if (!focusable.length) return;
      const first = focusable[0];
      const last = focusable[focusable.length - 1];
      if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last.focus(); }
      else if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first.focus(); }
    }
    document.addEventListener("keydown", handleKeyDown);
    return () => {
      document.body.classList.remove("drawer-open");
      document.removeEventListener("keydown", handleKeyDown);
      previouslyFocused?.focus();
      drawerTriggerRef.current = null;
    };
  }, [drawerOpen, drawerKind]);

  async function runSearch(event?: FormEvent) {
    event?.preventDefault();
    if (!query.trim()) return;
    if (!isConnected || !catalogHealthy) { setNotice({ kind: "error", text: catalogStorageMessage }); return; }
    if (!workspaceID) { setNotice({ kind: "warning", text: t("Add a source first so there is content to search.") }); return; }
    setBusy(true); setNotice(undefined);
    try {
      // Leave dimension selection to the typed dispatcher so ordinary search
      // always uses its lexical + structured + semantic default broker.
      const response = await command("search.query", { workspace_id: workspaceID, query: query.trim() });
      const statusBySubject = new Map(libraryItems.map((item) => [item.subject_ref, item.index_status]));
      const searchHits = (response.data?.hits ?? []) as SearchHit[];
      setHits(searchHits.map((hit) => ({ ...hit, index_status: hit.index_status ?? statusBySubject.get(hit.subject_ref ?? "") })));
      setSelected(undefined); setActiveTags([]); setActiveSystemFacets({ type: "", format: "", dedup: "", tags: "", index: "" }); setViewMode("search");
      if (response.status === "DEGRADED") {
        const reason = response.reasons?.map((item) => item.message?.trim()).filter((message): message is string => Boolean(message)).map((message) => t(message)).join("; ") || t("Search degradation reason was not reported.");
        setNotice({ kind: "warning", text: t("Search results are available, but some search coverage is degraded: {{reason}}", { reason }) });
      }
      setStatus("Connected");
    } catch (error) { setNotice({ kind: "error", text: error instanceof Error ? error.message : t("Search failed") }); }
    finally { setBusy(false); }
  }
  async function loadNamespace(activeWorkspaceID: string, rootID: string, parentEntryID = "", crumbs: BrowseCrumb[] = []) {
    const response = await command("namespace.list", { workspace_id: activeWorkspaceID, root_id: rootID, parent_id: parentEntryID || undefined });
    const entries = (response.data?.entries ?? []) as Array<{ entry_id?: string; subject_ref?: string; display_name?: string; entry_type?: string; content_id?: string; logical_size?: number; index_status?: SearchHit["index_status"] }>;
    const prefix = crumbs.map((crumb) => crumb.name).join("/");
    setHits(entries.map((entry) => ({
      subject_ref: entry.subject_ref || entry.entry_id,
      entry_id: entry.entry_id || entry.subject_ref,
      name: entry.display_name,
      path: [prefix, entry.display_name].filter(Boolean).join("/"),
      entry_type: entry.entry_type,
      content_id: entry.content_id,
      logical_size: entry.logical_size,
      index_status: entry.index_status,
    })));
    setSelected(undefined); setBrowseRootID(rootID); setBrowseCrumbs(crumbs); setActiveTags([]); setActiveSystemFacets({ type: "", format: "", dedup: "", tags: "", index: "" }); setViewMode("path");
  }
  async function loadContentLibrary(activeWorkspaceID: string, refreshGeneration = refreshGenerationRef.current) {
    const response = await command("content.list", { workspace_id: activeWorkspaceID });
    if (refreshGenerationRef.current !== refreshGeneration) return;
    const items = (response.data?.items ?? []) as SearchHit[];
    setLibraryItems(items); setHits(items);
    const roots = (response.data?.roots ?? []) as BrowseRoot[];
    const rootID = response.data?.root_id || (roots.length === 1 ? roots[0].root_id : "");
    setSelected(undefined); setBrowseRoots(roots); setBrowseRootID(rootID); setBrowseCrumbs([]); setActiveTags([]); setActiveSystemFacets({ type: "", format: "", dedup: "", tags: "", index: "" }); setViewMode("content");
  }
  async function loadSources(activeWorkspaceID: string, refreshGeneration = refreshGenerationRef.current) {
    try {
      const response = await command("source.list", { workspace_id: activeWorkspaceID });
      if (refreshGenerationRef.current !== refreshGeneration) return;
      setSources((response.data?.sources ?? []) as SourceSummary[]);
      setSourceProjection({ state: "READY" });
    } catch {
      if (refreshGenerationRef.current !== refreshGeneration) return;
      // Source management is a secondary projection; an unavailable projection
      // must not hide the content library or imply that saved bytes are gone.
      // Drop the old cards so a failed refresh cannot leave actions bound to a
      // stale source projection.
      setSources([]);
      setSourceProjection({ state: "DEGRADED" });
    }
  }
  function toggleTagFilter(value: string) {
    const next = activeTags.includes(value) ? activeTags.filter((tag) => tag !== value) : [...activeTags, value];
    if (next.length === 0) {
      setHits(facetFilteredItems); setSelected(undefined); setActiveTags([]); setViewMode(hasSystemFacets ? "tag" : "content");
      return;
    }
    setQuery(""); setSelected(undefined); setActiveTags(next); setViewMode("tag");
  }
  function toggleSystemFacet(kind: keyof SystemFacetState, value: string) {
    const next = { ...activeSystemFacets, [kind]: activeSystemFacets[kind] === value ? "" : value };
    setQuery(""); setSelected(undefined); setActiveSystemFacets(next); setViewMode("tag");
  }
  function clearLibraryFilters() {
    setHits(libraryItems); setSelected(undefined); setActiveTags([]); setActiveSystemFacets({ type: "", format: "", dedup: "", tags: "", index: "" }); setViewMode("content");
  }
  async function openLibraryEntry(hit: SearchHit) {
    if (hit.entry_type === "DIRECTORY" && (hit.entry_id || hit.subject_ref) && browseRootID) {
      const entryID = hit.entry_id || hit.subject_ref || "";
      const nextCrumbs = [...browseCrumbs, { subject_ref: hit.subject_ref || entryID, entry_id: entryID, name: hit.name || "Folder" }];
      setBusy(true);
      try { await loadNamespace(workspaceID, browseRootID, entryID, nextCrumbs); }
      catch (error) { setNotice({ kind: "error", text: error instanceof Error ? error.message : t("Could not open folder") }); }
      finally { setBusy(false); }
      return;
    }
    await loadDetails(hit);
  }
  async function navigateLibrary(parentEntryID: string, crumbs: BrowseCrumb[], rootID = browseRootID) {
    if (!workspaceID || !rootID) return;
    setBusy(true);
    try { await loadNamespace(workspaceID, rootID, parentEntryID, crumbs); }
    catch (error) { setNotice({ kind: "error", text: error instanceof Error ? error.message : t("Could not open folder") }); }
    finally { setBusy(false); }
  }
  async function loadSettings() {
    setConfigLoading(true);
    try {
      const response = await command("config.get", {});
      setConfigData(response.data); setConfigDraft(structuredClone(response.data?.config ?? {}));
    } catch (error) {
      setNotice({ kind: "error", text: error instanceof Error ? error.message : t("Could not load settings") });
    } finally { setConfigLoading(false); }
  }
  async function saveSettings() {
    if (!configData?.config_digest || !configDraft) return;
    setConfigLoading(true); setNotice(undefined);
    try {
      const response = await command("config.update", { expected_config_digest: configData.config_digest, config: configDraft });
      setConfigData(response.data); setConfigDraft(structuredClone(response.data?.config ?? {}));
      setNotice({ kind: "success", text: response.data?.restart_required ? t("Settings saved. Restart RestoreWeave to apply them.") : t("Settings saved.") });
    } catch (error) {
      setNotice({ kind: "error", text: error instanceof Error ? error.message : t("Could not save settings") });
    } finally { setConfigLoading(false); }
  }
  async function installSemanticBundle() {
    if (!semanticInstallAvailable || configDirty || configData?.restart_required) return;
    setSemanticInstall({ state: "RUNNING" });
    try {
      await command("semantic.bundle.install", {});
      setSemanticInstall({ state: "SUCCEEDED", message: t("BGE installed. Restart the service to activate it.") });
      await refreshCapabilities();
    } catch (error) {
      setSemanticInstall({ state: "FAILED", message: error instanceof Error ? error.message : t("BGE installation failed.") });
    }
  }
  async function rebuildSearch() {
    // Lexical/structured indexing is useful on its own.  Rebuilding it must
    // never be gated on the optional semantic bundle; the semantic lane can
    // remain unavailable while the exact and keyword lanes recover.
    if (!searchRebuildAvailable || !workspaceID || configDirty || configData?.restart_required) return;
    setSearchRebuild({ state: "RUNNING" });
    try {
      const response = await command("search.rebuild", { workspace_id: workspaceID });
      const data = response.data ?? {};
      if (data.semantic_state === "AVAILABLE" && response.status === "DEGRADED") {
        const coverage = data.lexical_coverage;
        const coverageMessage = coverage?.available === false
          ? t("Search indexes rebuilt; semantic search is ready, but keyword field coverage is unavailable.")
          : t("Search indexes rebuilt; semantic search is ready, but some keyword fields are not covered.");
        setSearchRebuild({ state: "DEGRADED", message: coverageMessage });
      } else if (data.semantic_state !== "AVAILABLE") {
        setSearchRebuild({ state: "DEGRADED", message: t("Keyword search rebuilt; semantic search failed: {{reason}}", { reason: data.semantic_failure || t("unavailable") }) });
      } else {
        setSearchRebuild({ state: "SUCCEEDED", message: t("Search indexes rebuilt; semantic search is ready.") });
      }
      await refreshCapabilities();
    } catch (error) {
      setSearchRebuild({ state: "FAILED", message: error instanceof Error ? error.message : t("Search rebuild failed.") });
    }
  }
  function setConfigField(section: string, field: string, value: unknown) {
    setConfigDraft((current: any) => current ? { ...current, [section]: { ...current[section], [field]: value } } : current);
  }
  function setRepositoryProfile(profile: string) {
    setConfigDraft((current: any) => current ? {
      ...current,
      storage: {
        ...current.storage,
        repository_profile: profile,
        compression_profile: profile === "local-zstd-v1" ? "zstd-v1" : "identity-v1",
      },
    } : current);
  }
  async function makePlan(event: FormEvent) {
    event.preventDefault(); if (!source.trim()) return;
    if (!coreStorageReady) { setNotice({ kind: "error", text: coreStorageMessage }); return; }
    if (looksLikeRemoteLocator(source)) {
      setNotice({ kind: "warning", text: t("Web addresses are not supported here. Mount remote storage on the RestoreWeave host, then enter its server path.") });
      return;
    }
    setBusy(true); setNotice(undefined);
    try { const response = await command("plan.ingest", { root: source.trim() }); setPlan(response.data); rememberWorkspace(response.data?.workspace_id); setNotice({ kind: response.data?.executable ? "success" : "warning", text: response.data?.executable ? t("Storage plan is ready to review.") : t("This source has blocked entries and cannot be saved yet.") }); }
    catch (error) { setNotice({ kind: "error", text: error instanceof Error ? error.message : t("Could not create storage plan") }); }
    finally { setBusy(false); }
  }
  async function applyPlan() {
    if (!plan?.plan_id || !plan?.plan_digest) return;
    if (!coreStorageReady) { setNotice({ kind: "error", text: coreStorageMessage }); return; }
    setBusy(true);
    try { const response = await command("plan.apply", { workspace_id: plan.workspace_id, plan_id: plan.plan_id, plan_digest: plan.plan_digest }); setPlan({ ...plan, ...response.data, state: response.data?.state ?? "SUCCEEDED" }); rememberWorkspace(response.data?.workspace_id ?? plan.workspace_id); const degraded = response.status === "DEGRADED" || (response.data?.warnings?.length ?? 0) > 0; setNotice({ kind: degraded ? "warning" : "success", text: t(degraded ? "Exact bytes saved, but derived processing is unavailable." : "Content saved. Exact bytes are now in the repository.") }); await refresh(); }
    catch (error) { setNotice({ kind: "error", text: error instanceof Error ? error.message : t("Could not save content") }); }
    finally { setBusy(false); }
  }
  async function loadDetails(hit: SearchHit) {
    const subjectRef = hit.subject_ref?.trim() ?? "";
    const requestToken = ++detailRequestRef.current;
    detailSubjectRef.current = subjectRef;
    setSelected(hit); setAnnotations([]); setRepresentations([]); setDescriptions([]); setDescriptionsTruncated(false);
    setEditingNoteID(""); setEditingBody(""); setNoteDraft(""); setTagDraft("");
    setDetailState({ subjectRef, annotations: "loading", representations: "loading", descriptions: "loading" });
    if (!workspaceID || !subjectRef) {
      setDetailState({ subjectRef, annotations: "error", representations: "error", descriptions: "error" });
      return;
    }
    const requests = await Promise.allSettled([
      command("annotation.list", { workspace_id: workspaceID, subject_ref: subjectRef }),
      command("representation.list", { workspace_id: workspaceID, subject_ref: subjectRef }),
      // Keep enough history to resolve the current note in the normal case.
      // If the server reports a longer chain, we fail closed below instead of
      // presenting an old revision as current.
      command("description.list", { workspace_id: workspaceID, subject_ref: subjectRef, limit: 1000 }),
    ]);
    // A late response from a previous subject must never populate the current
    // inspector. The subject check is intentional in addition to the token:
    // it keeps detail writes bound to the durable Subject the user selected.
    if (detailRequestRef.current !== requestToken || detailSubjectRef.current !== subjectRef) return;
    const annotationResult = subjectBoundDetailCollection(requests[0], "annotations", subjectRef);
    const representationResult = subjectBoundDetailCollection(requests[1], "representations", subjectRef);
    const descriptionResult = subjectBoundDetailCollection(requests[2], "documents", subjectRef);
    const descriptionTruncated = requests[2].status === "fulfilled" && requests[2].value.data?.truncated === true;
    // Truncation is a partial-success condition: the returned Notes remain
    // useful and must stay visible, with an explicit indication that history
    // continues beyond this bounded response.
    const descriptionState: DetailResourceStatus = descriptionResult.status;
    setDescriptionsTruncated(descriptionTruncated);
    setDetailState({
      subjectRef,
      annotations: annotationResult.status,
      representations: representationResult.status,
      descriptions: descriptionState,
    });
    setAnnotations(annotationResult.items);
    setRepresentations(representationResult.items);
    // Keep summary rows out of the visible Notes list until their bodies have
    // been read. This prevents a title or a transient placeholder from
    // looking like the actual note text.
    setDescriptions([]);
    if (descriptionState !== "ready" || descriptionResult.items.length === 0) return;
    // description.list deliberately returns bounded summaries. Hydrate the
    // bodies for this one subject so the single Notes view shows the actual
    // text while keeping the list endpoint small for broader callers. User
    // notes are already visible while these extra reads are in flight.
    const hydrated = await Promise.allSettled(descriptionResult.items.map((item) => command("description.get", {
      workspace_id: workspaceID,
      description_document_id: item.description_document_id,
    })));
    if (detailRequestRef.current !== requestToken || detailSubjectRef.current !== subjectRef) return;
    const descriptionItems = descriptionResult.items.map((summary, index) => {
      const result = hydrated[index];
      if (result?.status === "fulfilled" && result.value.status === "SUCCEEDED") {
        const document = result.value.data?.document;
        if (document?.subject_ref?.trim() === subjectRef) return document;
      }
      return { ...summary, body_unavailable: true };
    });
    setDescriptions(descriptionItems);
    setDetailState((current) => current.subjectRef === subjectRef && detailSubjectRef.current === subjectRef
      ? { ...current, descriptions: "ready" }
      : current);
  }
  function detailIsCurrent(subjectRef: string, requestToken = detailRequestRef.current) {
    return detailRequestRef.current === requestToken && detailSubjectRef.current === subjectRef;
  }
  function detailAnnotationsReady(subjectRef: string) {
    return detailState.subjectRef === subjectRef && detailSubjectRef.current === subjectRef && detailState.annotations === "ready";
  }
  async function saveNote(annotationID = "", body = "", expectedRevision = 0, annotationSubjectRef = "") {
    const subjectRef = selected?.subject_ref?.trim() ?? "";
    if (!annotationStorageReady || !subjectRef || !body.trim() || !workspaceID || !detailAnnotationsReady(subjectRef) || (annotationID && annotationSubjectRef.trim() !== subjectRef)) { if (!annotationStorageReady) setNotice({ kind: "error", text: catalogStorageMessage }); return; }
    const requestToken = detailRequestRef.current;
    setBusy(true);
    try {
      const response = await command("annotation.upsert", { workspace_id: workspaceID, subject_ref: subjectRef, kind: "NOTE", body: body.trim(), annotation_id: annotationID || undefined, expected_revision: expectedRevision });
      if (!detailIsCurrent(subjectRef, requestToken)) return;
      setAnnotations((items) => annotationID ? items.map((item) => item.annotation_id === annotationID ? response.data.annotation : item) : [...items, response.data.annotation]);
      setNoteDraft(""); setEditingNoteID(""); setEditingBody("");
      await refreshCapabilities();
      const degraded = response.status === "DEGRADED";
      setNotice({ kind: degraded ? "warning" : "success", text: degraded
        ? t(annotationID ? "Note updated, but search indexing is unavailable." : "Note added, but search indexing is unavailable.")
        : t(annotationID ? "Note updated." : "Note added.") });
    }
    catch (error) { setNotice({ kind: "error", text: error instanceof Error ? error.message : t("Could not save annotation") }); }
    finally { setBusy(false); }
  }
  async function loadTagVocabulary(activeWorkspaceID: string): Promise<AnnotationRecord[]> {
    try {
      const response = await command("annotation.list", { workspace_id: activeWorkspaceID });
      const items = ((response.data?.annotations ?? []) as AnnotationRecord[])
        .filter((item) => item.kind === "TAG" && !item.tombstoned && typeof item.body === "string" && Boolean(item.body.trim()));
      setWorkspaceTags(items);
      return items;
    } catch {
      setWorkspaceTags([]);
      return [];
    }
  }
  function syncTagCoverage(subjectRef: string, tags: AnnotationRecord[]) {
    const normalized = subjectRef.trim();
    if (!normalized) return;
    const state = tags.some((item) => item.subject_ref?.trim() === normalized && item.kind === "TAG" && !item.tombstoned) ? "MARKED" : "UNMARKED";
    const update = (item: SearchHit) => item.subject_ref?.trim() === normalized ? { ...item, index_status: { ...item.index_status, tags: state } } : item;
    setLibraryItems((items) => items.map(update));
    setHits((items) => items.map(update));
    setSelected((item) => item && item.subject_ref?.trim() === normalized ? update(item) : item);
  }
  async function saveTag() {
    const body = tagDraft.trim();
    const subjectRef = selected?.subject_ref?.trim() ?? "";
    if (!annotationStorageReady || !subjectRef || !workspaceID || !body || !detailAnnotationsReady(subjectRef)) { if (!annotationStorageReady) setNotice({ kind: "error", text: catalogStorageMessage }); return; }
    const requestToken = detailRequestRef.current;
    setBusy(true); setNotice(undefined);
    try {
      const response = await command("annotation.upsert", { workspace_id: workspaceID, subject_ref: subjectRef, kind: "TAG", body, expected_revision: 0 });
      if (!detailIsCurrent(subjectRef, requestToken)) return;
      const annotation = response.data?.annotation;
      setAnnotations((items) => items.some((item) => item.annotation_id === annotation?.annotation_id) ? items : [...items, annotation]);
      setTagDraft("");
      const tags = await loadTagVocabulary(workspaceID);
      syncTagCoverage(subjectRef, tags);
      await refreshCapabilities();
      setNotice({ kind: response.status === "DEGRADED" ? "warning" : "success", text: t(response.status === "DEGRADED" ? "Tag added, but search indexing is unavailable." : "Tag added.") });
    } catch (error) {
      setNotice({ kind: "error", text: error instanceof Error ? error.message : t("Could not save tag") });
    } finally { setBusy(false); }
  }
  async function deleteTag(tag: any) {
    const subjectRef = selected?.subject_ref?.trim() ?? "";
    if (!annotationStorageReady || !workspaceID || !subjectRef || !tag?.annotation_id || !tag?.revision || !detailAnnotationsReady(subjectRef) || !annotationBelongsToSubject(tag, subjectRef)) { if (!annotationStorageReady) setNotice({ kind: "error", text: catalogStorageMessage }); return; }
    const requestToken = detailRequestRef.current;
    setBusy(true); setNotice(undefined);
    try {
      const response = await command("annotation.delete", { workspace_id: workspaceID, annotation_id: tag.annotation_id, expected_revision: tag.revision });
      if (!detailIsCurrent(subjectRef, requestToken)) return;
      setAnnotations((items) => items.filter((item) => item.annotation_id !== tag.annotation_id));
      const remainingTags = await loadTagVocabulary(workspaceID);
      syncTagCoverage(subjectRef, remainingTags);
      const removedValue = typeof tag.body === "string" ? tag.body.trim() : "";
      if (removedValue && activeTags.includes(removedValue) && !remainingTags.some((item) => item.body?.trim() === removedValue)) {
        const next = activeTags.filter((value) => value !== removedValue);
        setActiveTags(next);
        if (next.length === 0) {
          setHits(facetFilteredItems); setSelected(undefined); setViewMode(hasSystemFacets ? "tag" : "content");
        }
      }
      await refreshCapabilities();
      setNotice({ kind: response.status === "DEGRADED" ? "warning" : "success", text: t(response.status === "DEGRADED" ? "Tag removed, but search indexing is unavailable." : "Tag removed.") });
    } catch (error) {
      setNotice({ kind: "error", text: error instanceof Error ? error.message : t("Could not remove tag") });
    } finally { setBusy(false); }
  }
  async function deleteNote(note: any) {
    const subjectRef = selected?.subject_ref?.trim() ?? "";
    if (!annotationStorageReady || !workspaceID || !subjectRef || !note?.annotation_id || !note?.revision || !detailAnnotationsReady(subjectRef) || !annotationBelongsToSubject(note, subjectRef) || !window.confirm(t("Delete this note?"))) { if (!annotationStorageReady) setNotice({ kind: "error", text: catalogStorageMessage }); return; }
    const requestToken = detailRequestRef.current;
    setBusy(true); setNotice(undefined);
    try {
      const response = await command("annotation.delete", { workspace_id: workspaceID, annotation_id: note.annotation_id, expected_revision: note.revision });
      if (!detailIsCurrent(subjectRef, requestToken)) return;
      setAnnotations((items) => items.filter((item) => item.annotation_id !== note.annotation_id));
      setEditingNoteID(""); setEditingBody("");
      await refreshCapabilities();
      setNotice({ kind: response.status === "DEGRADED" ? "warning" : "success", text: t(response.status === "DEGRADED" ? "Note deleted, but search indexing is unavailable." : "Note deleted.") });
    } catch (error) {
      setNotice({ kind: "error", text: error instanceof Error ? error.message : t("Could not delete note") });
    } finally { setBusy(false); }
  }
  async function makeRestorePlan(event: FormEvent) {
    event.preventDefault(); if (!snapshotRef || !restoreDestination.trim()) return;
    if (!coreStorageReady) { setNotice({ kind: "error", text: coreStorageMessage }); return; }
    setBusy(true); setNotice(undefined);
    try { const response = await command("plan.restore", { snapshot_ref: snapshotRef, destination: restoreDestination.trim() }); setRestorePlan(response.data); setNotice({ kind: "success", text: t("Restore plan is ready to review.") }); }
    catch (error) { setNotice({ kind: "error", text: error instanceof Error ? error.message : t("Could not create restore plan") }); }
    finally { setBusy(false); }
  }
  async function applyRestore() {
    if (!restorePlan?.plan_id || !restorePlan?.plan_digest || !restorePlan?.workspace_id) return;
    if (!coreStorageReady) { setNotice({ kind: "error", text: coreStorageMessage }); return; }
    setBusy(true);
    try {
      const response = await command("plan.apply", { workspace_id: restorePlan.workspace_id, plan_id: restorePlan.plan_id, plan_digest: restorePlan.plan_digest });
      setRestorePlan({ ...restorePlan, ...response.data, state: response.data?.state ?? response.status ?? "SUCCEEDED" });
      setRestoreOpen(false);
      const reasonMessages = [
        ...(response.reasons ?? []).map((item) => item.message),
        ...(Array.isArray(response.data?.warnings) ? response.data.warnings : []),
        ...(Array.isArray(response.data?.reasons) ? response.data.reasons.map((item: unknown) => typeof item === "string" ? item : (item as { message?: string })?.message) : []),
      ].filter((message): message is string => typeof message === "string" && Boolean(message.trim()));
      const details = reasonMessages.length > 0 ? ` ${reasonMessages.join(" ")}` : "";
      const files = response.data?.files ?? restorePlan.files;
      const destination = response.data?.destination ?? restoreDestination;
      if (response.status === "DEGRADED") {
        setNotice({ kind: "warning", text: `${t("Restored {{files}} file(s) to {{destination}}, but some fidelity or metadata checks were unavailable.", { files, destination })}${details}` });
      } else {
        setNotice({ kind: "success", text: `${t("Restored and verified {{files}} file(s) to {{destination}}.", { files, destination })}${details}` });
      }
    }
    catch (error) { setNotice({ kind: "error", text: error instanceof Error ? error.message : t("Could not restore snapshot") }); }
    finally { setBusy(false); }
  }
  function rememberWorkspace(value: unknown) { if (typeof value === "string" && value.trim()) { setWorkspaceID(value); window.localStorage.setItem("restoreweave.workspace_id", value); } }
  function canLeaveSettings() { return !settingsOpen || !configDirty || window.confirm(t("Discard unsaved settings?")); }
  function rememberDrawerTrigger(trigger?: EventTarget | null) {
    drawerTriggerRef.current = trigger instanceof HTMLElement
      ? trigger
      : document.activeElement instanceof HTMLElement
        ? document.activeElement
        : null;
  }
  function openAdd(sourceItem?: SourceSummary, trigger?: EventTarget | null) {
    if (!isConnected || !coreStorageReady) { setNotice({ kind: "error", text: coreStorageMessage }); return; }
    if (!canLeaveSettings()) return;
    rememberDrawerTrigger(trigger);
    setSettingsOpen(false); setRestoreOpen(false);
    setSource(sourceItem?.locator?.trim() ?? ""); setPlan(undefined);
    setRecheckSourceRef(sourceItem?.source_ref?.trim() ?? "");
    setAddOpen(true); setNotice(undefined);
  }
  function closeAdd() { setAddOpen(false); setRecheckSourceRef(""); }
  async function viewSource(sourceItem: SourceSummary) {
    if (!workspaceID) return;
    setBusy(true);
    try {
      if (sourceItem.latest_namespace_root_id) {
        await loadNamespace(workspaceID, sourceItem.latest_namespace_root_id);
      } else {
        await loadContentLibrary(workspaceID);
      }
    } catch (error) {
      setNotice({ kind: "error", text: error instanceof Error ? error.message : t("Could not open source content") });
    } finally { setBusy(false); }
  }
  function openSettings(trigger?: EventTarget | null) {
    if (!canLeaveSettings()) return;
    // Keep the settings trigger so closing the drawer (including Escape or
    // backdrop dismissal) returns keyboard users to the control that opened
    // it, just like the add/restore drawers do.
    rememberDrawerTrigger(trigger);
    setAddOpen(false); setRecheckSourceRef(""); setRestoreOpen(false); setSettingsOpen(true); setSettingsSection("storage"); void loadSettings();
  }
  function openRestore(trigger?: EventTarget | null) { if (!isConnected || !coreStorageReady) { setNotice({ kind: "error", text: coreStorageMessage }); return; } if (!canLeaveSettings()) return; rememberDrawerTrigger(trigger); setAddOpen(false); setRecheckSourceRef(""); setSettingsOpen(false); setRestorePlan(undefined); setRestoreDestination(""); setRestoreOpen(true); setNotice(undefined); }
  function closeSettings() {
    if (!canLeaveSettings()) return;
    setSettingsOpen(false);
  }
  function reloadSettings() {
    if (configDirty && !window.confirm(t("Discard unsaved settings?"))) return;
    void loadSettings();
  }

  const showFirstSource = viewMode === "content" && libraryItems.length === 0 && sources.length === 0 && sourceProjection.state !== "DEGRADED";
  const showConfiguredSourceEmpty = viewMode === "content" && libraryItems.length === 0 && sources.length > 0;
  const showLibraryFacets = (viewMode === "content" || viewMode === "tag") && libraryItems.length > 0;
  const repositoryPath = statusData?.repository?.path || t("Configured content repository");
  const sourceLooksRemote = looksLikeRemoteLocator(source);

  return <div className="app-shell">
    <header className="topbar">
      <a className="brand" href="/" aria-label={t("RestoreWeave home")}><span className="brand-mark"><ShieldCheck size={19} /></span><span>RestoreWeave</span></a>
      <form className="global-search" onSubmit={runSearch}>
        <Search size={17} aria-hidden="true" />
        <input aria-label={t("Search content library")} value={query} onChange={(event) => setQuery(event.target.value)} placeholder={t("Search names, paths, metadata, tags, notes, and extracted text...")} disabled={!isConnected || !catalogHealthy} />
        <button type="submit" disabled={busy || !query.trim() || !isConnected || !catalogHealthy}>{busy ? <LoaderCircle className="spin" size={16} /> : t("Search")}</button>
      </form>
      <div className="top-actions">
        <span className={`connection ${isConnected ? "ok" : "bad"}`} aria-label={t(status)}>{status === "Checking service" ? <LoaderCircle className="spin" size={15} /> : isConnected ? <CheckCircle2 size={15} /> : <AlertCircle size={15} />}<span>{t(status)}</span></span>
        <button className="secondary-button restore-button" aria-label={t("Restore latest snapshot")} title={t("Restore latest snapshot")} onClick={(event) => openRestore(event.currentTarget)} disabled={!snapshotRef || !isConnected || !coreStorageReady}><ArchiveRestore size={16} /><span>{t("Restore")}</span></button>
        <button className="primary-button add-button" aria-label={t("Add source")} title={t("Add source")} onClick={(event) => openAdd(undefined, event.currentTarget)} disabled={!isConnected || !coreStorageReady}><Plus size={16} /><span>{t("Add source")}</span></button>
        <button className="icon-button" title={t("Refresh service status")} aria-label={t("Refresh service status")} onClick={() => void refresh()}><RefreshCw size={17} /></button>
        <button className="icon-button" title={t("Open settings")} aria-label={t("Open settings")} onClick={(event) => openSettings(event.currentTarget)}><Settings size={17} /></button>
      </div>
    </header>
    {isConnected && <details className="system-rail" aria-label={t("System readiness")} open={!coreStorageReady}>
      <summary className={`rail-summary ${coreStorageReady ? "" : "degraded"}`}><span className="rail-title">{t(coreStorageReady ? "SYSTEM READY" : "Core storage unavailable")}</span><ChevronRight size={14} /></summary>
      <div className="system-rail-details"><span className={repositoryHealthy ? "ready" : "degraded"}><Database size={15} />{repositoryHealthy ? t("Exact repository") : t("Repository unavailable")}</span><span className={catalogHealthy ? "ready" : "degraded"}><Database size={15} />{catalogHealthy ? t("Catalog ready") : t("Catalog unavailable")}</span><span className={lexicalReady ? "ready" : "degraded"}><Search size={15} />{lexicalReady ? t("Keyword search") : `${t("Keyword search")} · ${t("Offline")}`}</span><span className={semanticReady ? "ready" : "degraded"}><Sparkles size={15} />{semanticReady ? t("Semantic search ready") : t("Semantic search unavailable")}</span><span className={snapshotRef ? "ready" : "degraded"}><ArchiveRestore size={15} />{snapshotRef ? t("Recovery point ready") : t("No recovery point")}</span><CapacitySummary repository={repositoryCapacity} t={t} /></div>
    </details>}
    <main className={`workspace ${selected ? "detail-active" : ""} ${showFirstSource ? "empty-library" : ""}`}><section className="content-column">
      <div className="workspace-heading"><div><p className="eyebrow">{t("YOUR LIBRARY")}</p><h1>{viewMode === "search" ? t("Results for \"{{query}}\"", { query }) : viewMode === "tag" ? activeTags.length ? t("Content tagged {{tags}}", { tags: activeTags.join(" + ") }) : t("Filtered content") : viewMode === "path" ? browseCrumbs.at(-1)?.name || t("Source paths") : t("All content")}</h1></div><div className="heading-actions">{hasActiveFilters && <button className="library-return" onClick={clearLibraryFilters}><X size={14} />{t("Clear filters")}</button>}{(viewMode === "search" || viewMode === "path") && workspaceID && <button className="library-return" onClick={() => void loadContentLibrary(workspaceID)}><Home size={14} />{t("All content")}</button>}<span className="result-count">{hits.length ? t(viewMode === "search" ? "{{count}} results" : "{{count}} items", { count: hits.length }) : workspaceID ? t("0 items") : t("No workspace yet")}</span></div></div>
      {showLibraryFacets && <section className="tag-browser" aria-label={t("Filter by tags")}>
        <div className="tag-browser-heading"><Tag size={15} /><span>{t("Browse by tags")}</span><small>{t("Choose multiple tags to narrow the library.")}</small></div>
        <div className="tag-filter-list">
          <button type="button" className={`tag-filter all ${activeTags.length === 0 ? "active" : ""}`} aria-pressed={activeTags.length === 0} onClick={() => { setHits(facetFilteredItems); setSelected(undefined); setActiveTags([]); setViewMode(hasSystemFacets ? "tag" : "content"); }}><span>{t("All tags")}</span><b>{facetFilteredItems.length}</b></button>
          {tagFacets.map((facet) => <button type="button" key={facet.value} className={`tag-filter ${activeTags.includes(facet.value) ? "active" : ""}`} aria-pressed={activeTags.includes(facet.value)} aria-label={t("Filter by tag {{tag}}, {{count}} items", { tag: facet.value, count: facet.count })} onClick={() => toggleTagFilter(facet.value)}><span>{facet.value}</span><b>{facet.count}</b></button>)}
          {tagFacets.length === 0 && <span className="tag-filter-empty">{t("No tags yet")}</span>}
        </div>
        <details className="system-facets-disclosure">
        <summary className="system-facet-heading"><Hash size={14} /><span>{t("System fields")}</span><small>{t("Read-only type, format, dedup, and index facets")}</small><ChevronRight size={14} /></summary>
        <div className="system-facet-groups">
          <div className="system-facet-group"><span>{t("Type")}</span>{systemFacets.types.map((facet) => <button type="button" key={facet.value} className={`system-facet ${activeSystemFacets.type === facet.value ? "active" : ""}`} aria-pressed={activeSystemFacets.type === facet.value} onClick={() => toggleSystemFacet("type", facet.value)}><span>{formatEntryType(facet.value, t)}</span><b>{facet.count}</b></button>)}</div>
          <div className="system-facet-group"><span>{t("Format")}</span>{systemFacets.formats.length ? systemFacets.formats.map((facet) => <button type="button" key={facet.value} className={`system-facet ${activeSystemFacets.format === facet.value ? "active" : ""}`} aria-pressed={activeSystemFacets.format === facet.value} onClick={() => toggleSystemFacet("format", facet.value)}><span>{facet.value}</span><b>{facet.count}</b></button>) : <small className="facet-unavailable">{t("No file formats")}</small>}</div>
          <div className="system-facet-group"><span>{t("Dedup")}</span>{systemFacets.dedup.map((facet) => <button type="button" key={facet.value} className={`system-facet ${activeSystemFacets.dedup === facet.value ? "active" : ""}`} aria-pressed={activeSystemFacets.dedup === facet.value} onClick={() => toggleSystemFacet("dedup", facet.value)}><span>{formatDedupFacet(facet.value, t)}</span><b>{facet.count}</b></button>)}</div>
          <div className="system-facet-group"><span>{t("Coverage")}</span>{systemFacets.tags.map((facet) => <button type="button" key={facet.value} className={`system-facet ${activeSystemFacets.tags === facet.value ? "active" : ""}`} aria-pressed={activeSystemFacets.tags === facet.value} onClick={() => toggleSystemFacet("tags", facet.value)}><span>{t("Unmarked")}</span><b>{facet.count}</b></button>)}{systemFacets.index.map((facet) => <button type="button" key={facet.value} className={`system-facet ${activeSystemFacets.index === facet.value ? "active" : ""}`} aria-pressed={activeSystemFacets.index === facet.value} onClick={() => toggleSystemFacet("index", facet.value)}><span>{t("Index not ready")}</span><b>{facet.count}</b></button>)}{!systemFacets.tags.length && !systemFacets.index.length && <small className="facet-unavailable">{t("No incomplete coverage")}</small>}</div>
        </div></details>
        {hasActiveFilters && <div className="filter-summary" aria-label={t("Active filters")}><span className="filter-summary-label">{t("Active filters")}:</span>{activeTags.map((tag) => <button type="button" className="active-filter" key={tag} onClick={() => toggleTagFilter(tag)} aria-label={t("Remove tag filter {{tag}}", { tag })}><Tag size={11} />{tag}<X size={11} /></button>)}{hasSystemFacets && <span className="filter-summary-system">{[activeSystemFacets.type ? `type:${formatEntryType(activeSystemFacets.type, t)}` : "", activeSystemFacets.format ? `format:${activeSystemFacets.format}` : "", activeSystemFacets.dedup ? `dedup:${formatDedupFacet(activeSystemFacets.dedup, t)}` : "", activeSystemFacets.tags ? t("Unmarked") : "", activeSystemFacets.index ? t("Index not ready") : ""].filter(Boolean).join(" + ")}</span>}<button type="button" className="filter-summary-clear" onClick={clearLibraryFilters}>{t("Clear filters")}</button></div>}
      </section>}
      {(sources.length > 0 || sourceProjection.state === "DEGRADED") && <details className="source-manager">
        <summary><HardDrive size={14} /><span>{t("Sources")}</span><small>{sourceProjection.state === "DEGRADED" ? t("Source status unavailable") : t("{{count}} configured", { count: sources.length })}</small><ChevronRight size={14} /></summary>
        {sourceProjection.state === "DEGRADED" && <div className="source-projection-warning" role="status"><AlertCircle size={14} /><span>{t("Source status could not be loaded. Saved content remains available; refresh to try again.")}</span></div>}
        <div className="source-manager-list">{sources.map((sourceItem, index) => <SourceCard key={sourceItem.source_ref ?? sourceItem.locator ?? index} source={sourceItem} locale={locale} t={t} busy={busy} coreStorageReady={coreStorageReady} onView={() => void viewSource(sourceItem)} onRecheck={(trigger) => openAdd(sourceItem, trigger)} />)}</div>
      </details>}
      {(viewMode === "content" || viewMode === "tag") && browseRoots.length > 0 && <details className="source-disclosure"><summary><Folder size={14} /><span>{t("Browse source paths")}</span><small>{t("Source paths are provenance")}</small><ChevronRight size={14} /></summary><div className="source-switcher" aria-label={t("Source paths")}>{browseRoots.map((root) => { const label = sourceButtonLabel(root, browseRoots, t); const path = root.source_path?.trim(); return <button type="button" key={root.root_id} title={path || label} aria-label={path ? t("Browse source {{name}} at {{path}}", { name: root.name || t("Source path"), path }) : label} onClick={() => void navigateLibrary("", [], root.root_id)}><Folder size={14} />{label}</button>; })}</div></details>}
      {viewMode === "path" && browseRootID && <nav className="breadcrumbs" aria-label={t("Source paths")}><button title={t("Source paths")} aria-label={t("Source path root")} onClick={() => void navigateLibrary("", [])}><Home size={14} /></button>{browseCrumbs.map((crumb, index) => <span key={crumb.entry_id}><ChevronRight size={13} /><button onClick={() => void navigateLibrary(crumb.entry_id || crumb.subject_ref, browseCrumbs.slice(0, index + 1))}>{crumb.name}</button></span>)}</nav>}
      {isConnected && !coreStorageReady ? <div className="notice error" role="alert"><AlertCircle size={16} />{coreStorageMessage}</div> : isUnavailable ? <div className="service-offline" role="alert"><span><Server size={18} /></span><div><strong>{t("RestoreWeave service is unavailable")}</strong><small>{t("Start the service, then refresh this page. Saved content has not been changed.")}</small></div><button className="secondary-button compact" onClick={() => void refresh()}><RefreshCw size={15} />{t("Retry")}</button></div> : notice && <div className={`notice ${notice.kind}`} role={notice.kind === "error" ? "alert" : "status"}>{notice.kind === "error" ? <AlertCircle size={16} /> : notice.kind === "warning" ? <Sparkles size={16} /> : <Check size={16} />}{notice.text}<button aria-label={t("Dismiss notice")} onClick={() => setNotice(undefined)}><X size={14} /></button></div>}
    {hits.length ? <div className="result-list">{hits.map((hit, index) => <ResultRow key={hit.subject_ref ?? index} hit={hit} tags={hit.subject_ref ? tagsBySubject.get(hit.subject_ref) ?? [] : []} activeTags={activeTags} selected={selected?.subject_ref === hit.subject_ref} t={t} onSelect={() => void (viewMode === "path" ? openLibraryEntry(hit) : loadDetails(hit))} onTagFilter={toggleTagFilter} />)}</div> : <div className="empty-state"><span className="vault-sigil"><ShieldCheck size={35} /></span><h2>{t(showFirstSource ? "Start by adding a source" : showConfiguredSourceEmpty ? "Source configured, no saved content yet" : viewMode === "search" ? "No matches found" : viewMode === "tag" ? activeTags.length ? "No content matches these tags" : hasSystemFacets ? "No content matches these filters" : "No content matches these tags" : viewMode === "path" ? "This source path is empty" : "No content yet")}</h2><p>{t(showFirstSource ? "Add a server folder, review what will be stored, then confirm." : showConfiguredSourceEmpty ? "Recheck creates a reviewable plan. Existing saved content is not changed until you confirm." : viewMode === "search" ? "Try another name, path, metadata value, tag, note, or extracted phrase." : viewMode === "tag" ? activeTags.length ? "Choose fewer tags to broaden the result." : hasSystemFacets ? "Adjust or clear the system filters." : "Choose fewer tags to broaden the result." : viewMode === "path" ? "No captured items are recorded at this source path." : "Saved content will appear here independent of its original folders.")}</p>{showFirstSource && <button className="primary-button" onClick={(event) => openAdd(undefined, event.currentTarget)} disabled={!coreStorageReady}><Plus size={16} />{t("Add source")}</button>}</div>}
    </section>{(selected || hits.length > 0) && <aside className="detail-column">{selected ? <Details hit={selected} annotations={annotations} representations={representations} descriptions={descriptions} descriptionsTruncated={descriptionsTruncated} annotationStatus={detailState.annotations} representationsStatus={detailState.representations} descriptionsStatus={detailState.descriptions} annotationMutationEnabled={annotationStorageReady && detailState.subjectRef === selected.subject_ref?.trim() && detailSubjectRef.current === selected.subject_ref?.trim() && detailState.annotations === "ready"} noteDraft={noteDraft} tagDraft={tagDraft} tagVocabulary={tagVocabulary} editingNoteID={editingNoteID} editingBody={editingBody} busy={busy} t={t} locale={locale} onBack={() => { detailRequestRef.current += 1; detailSubjectRef.current = ""; setDetailState({ subjectRef: "", annotations: "idle", representations: "idle", descriptions: "idle" }); setSelected(undefined); }} onDraft={setNoteDraft} onTagDraft={setTagDraft} onAddTag={() => void saveTag()} onDeleteTag={(tag) => void deleteTag(tag)} onAdd={() => void saveNote("", noteDraft)} onEdit={(note) => { setEditingNoteID(note.annotation_id); setEditingBody(note.body); }} onDeleteNote={(note) => void deleteNote(note)} onEditBody={setEditingBody} onSaveEdit={(note) => void saveNote(note.annotation_id, editingBody, note.revision, note.subject_ref)} onCancelEdit={() => { setEditingNoteID(""); setEditingBody(""); }} /> : <div className="detail-placeholder"><span className="inspection-mark"><Hash size={25} /></span><p className="eyebrow">{t("INSPECTOR")}</p><h2>{t("Select an item")}</h2><p>{t("Details, exact-storage status, tags, and notes appear here.")}</p></div>}</aside>}</main>
    {addOpen && <div className="drawer-backdrop" onMouseDown={(event) => { if (event.target === event.currentTarget) closeAdd(); }}><aside className="drawer source-drawer" role="dialog" aria-modal="true" aria-label={t(recheckSourceRef ? "Recheck source" : "Add source")}><div className="drawer-header"><div><p className="eyebrow">{t(recheckSourceRef ? "RECHECK SOURCE" : "NEW SOURCE")}</p><h2>{t(recheckSourceRef ? "Recheck source" : "Add source")}</h2></div><button className="icon-button" aria-label={t("Close add source")} onClick={closeAdd}><X size={17} /></button></div><p className="drawer-copy">{t(recheckSourceRef ? "Inspect this source again. Review the changes, then confirm to publish a new snapshot." : "Choose a server folder and preview its exact storage plan. Saving starts only after you confirm.")}</p>{isUnavailable ? <div className="drawer-inline-warning" role="alert"><AlertCircle size={15} />{t("Start the RestoreWeave service before previewing this source.")}</div> : !coreStorageReady ? <div className="drawer-inline-warning" role="alert"><AlertCircle size={15} />{coreStorageMessage}</div> : notice && <div className={`notice drawer-notice ${notice.kind}`} role={notice.kind === "error" ? "alert" : "status"}>{notice.kind === "error" ? <AlertCircle size={16} /> : notice.kind === "warning" ? <Sparkles size={16} /> : <Check size={16} />}{notice.text}</div>}<div className="source-kind"><span className="source-kind-icon"><HardDrive size={19} /></span><div><strong>{t("Server folder")}</strong><small>{t("Local disk or mounted storage visible to RestoreWeave")}</small></div><em>{t("Supported now")}</em></div><div className="source-route" aria-label={t("Source storage route")}><div><span>{t("Read from")}</span><strong title={source.trim()}>{source.trim() || t("Enter a server path")}</strong><small>{t("Source files stay in place")}</small></div><ChevronRight size={17} /><div><span>{t("Store unique bytes in")}</span><strong title={repositoryPath}>{repositoryPath}</strong><small>{t("Exact whole-file deduplication")}</small></div></div><form className="stack-form" onSubmit={makePlan}><label>{t("Path on RestoreWeave host")}<input autoFocus value={source} onChange={(event) => { setSource(event.target.value); setPlan(undefined); }} placeholder={t("/data/to-add")} aria-invalid={sourceLooksRemote || undefined} /><small className="path-helper"><Server size={13} />{t("Use a local or mounted server path. This browser does not upload files or fetch URLs.")}</small>{sourceLooksRemote && <small className="source-inline-error"><AlertCircle size={13} />{t("Enter a server path, not a URL.")}</small>}</label><button className="primary-button" disabled={busy || isUnavailable || !coreStorageReady || !source.trim() || sourceLooksRemote}>{busy ? t("Inspecting...") : t("Preview storage plan")}</button></form><p className="planning-note">{t("Planning records the source and plan in the catalog, but does not write file bytes or publish a snapshot until you confirm.")}</p>{plan && <PlanCard plan={plan} busy={busy} coreStorageReady={coreStorageReady} t={t} onApply={() => void applyPlan()} />}</aside></div>}
    {settingsOpen && <SettingsPanel section={settingsSection} configData={configData} draft={configDraft} loading={configLoading} dirty={configDirty} lexicalReady={lexicalReady} semanticReady={semanticReady} semanticBundleReady={semanticBundleReady} semanticBundleRestartRequired={semanticBundleRestartRequired} semanticBundleOperatorProvided={semanticBundleOperatorProvided} semanticInstallAvailable={semanticInstallAvailable} searchRebuildAvailable={searchRebuildAvailable} semanticInstall={semanticInstall} searchRebuild={searchRebuild} statusData={statusData} workspaceID={workspaceID} locale={locale} t={t} onLocale={setLocale} onSection={setSettingsSection} onClose={closeSettings} onField={setConfigField} onRepositoryProfile={setRepositoryProfile} onReload={reloadSettings} onSave={() => void saveSettings()} onInstallSemanticBundle={() => void installSemanticBundle()} onRebuildSearch={() => void rebuildSearch()} />}
    {restoreOpen && <div className="drawer-backdrop" onMouseDown={(event) => { if (event.target === event.currentTarget) setRestoreOpen(false); }}><aside className="drawer" role="dialog" aria-modal="true" aria-label={t("Restore snapshot")}><div className="drawer-header"><div><p className="eyebrow">{t("RECOVER")}</p><h2>{t("Restore snapshot")}</h2></div><button className="icon-button" aria-label={t("Close restore")} onClick={() => setRestoreOpen(false)}><X size={17} /></button></div><p className="drawer-copy">{t("Choose an empty destination, inspect the plan, then restore and verify exact bytes.")}</p><form className="stack-form" onSubmit={makeRestorePlan}><label>{t("Destination folder")}<input autoFocus value={restoreDestination} onChange={(event) => { setRestoreDestination(event.target.value); setRestorePlan(undefined); }} placeholder={t("/data/restored-copy")} /></label><button className="primary-button" disabled={busy || !coreStorageReady || !restoreDestination.trim()}>{busy ? t("Inspecting...") : t("Preview restore")}</button></form>{restorePlan && <RestoreCard plan={restorePlan} busy={busy} coreStorageReady={coreStorageReady} t={t} onApply={() => void applyRestore()} />}</aside></div>}
  </div>;
}

function ResultRow({ hit, tags, activeTags, selected, t, onSelect, onTagFilter }: { hit: SearchHit; tags: string[]; activeTags: string[]; selected: boolean; t: Translator; onSelect: () => void; onTagFilter: (tag: string) => void }) {
  const directory = hit.entry_type === "DIRECTORY";
  const matchedSegments = (hit.segments ?? []).filter((segment) => Boolean(segment.matched_text?.trim()));
  // The result list is a scan surface, not the full evidence viewer. Keep one
  // short match here; the selected item's inspector remains the place for
  // complete provenance, identity, and notes.
  const visibleSegments = matchedSegments.slice(0, 1);
  const hiddenSegmentCount = matchedSegments.length - visibleSegments.length;
  const title = hit.name || t("Untitled subject");
  const path = hit.path || (hit.entry_type ? formatEntryType(hit.entry_type, t) : t("Protected content item"));
  const tagValue = hit.index_status?.tags;
  const lexicalValue = hit.index_status?.lexical;
  const semanticValue = hit.index_status?.semantic;
  const tagStatus = formatCoverageStatus(tagValue, t);
  const lexicalStatus = formatIndexStatus(lexicalValue, t);
  const semanticStatus = formatIndexStatus(semanticValue, t);
  return <div className={`result-row ${selected ? "selected" : ""}`} role="group" aria-label={title} onClick={onSelect}><span className="file-icon">{directory ? <Folder size={20} /> : <FileSearch size={19} />}</span><span className="result-copy"><button type="button" className="result-open-copy" onClick={(event) => { event.stopPropagation(); onSelect(); }}><strong>{title}</strong><small>{path}{hit.logical_size != null && !directory ? ` · ${formatBytes(hit.logical_size)}` : ""}</small></button>{visibleSegments.map((segment, index) => { const segmentKey = `${segment.semantic_segment_id || segment.source_id || segment.source_type || "segment"}-${segment.ordinal ?? index}-${index}`; return <span className="result-match" title={segment.matched_text} aria-label={t("Matched segment provenance")} key={segmentKey}><b>{searchSegmentSourceLabel(segment, t)}</b>{previewUnicodeText(segment.matched_text, 180)}</span>; })}{tags.length > 0 && <span className="row-tags" aria-label={t("Tags")}>{tags.map((tag) => { const active = activeTags.includes(tag); return <button type="button" className={`row-tag user-filter ${active ? "active" : ""}`} aria-pressed={active} title={t(active ? "Remove tag filter {{tag}}" : "Filter by tag {{tag}}", { tag })} aria-label={t(active ? "Remove tag filter {{tag}}" : "Filter by tag {{tag}}", { tag })} key={tag} onClick={(event) => { event.stopPropagation(); onTagFilter(tag); }}><Tag size={10} />{tag}</button>; })}</span>}<span className="result-status" aria-label={t("Index status")}><span className="result-status-item"><span className={`status-dot ${lexicalValue === "READY" ? "ready" : ""}`} />{t("Lexical")}: {lexicalStatus}</span><span className="result-status-item"><span className={`status-dot ${semanticValue === "READY" ? "ready" : ""}`} />{t("Semantic")}: {semanticStatus}</span><span className="result-status-item result-tag-status">{tagStatus}</span></span>{hiddenSegmentCount > 0 && <small className="result-more-hint">{t("{{count}} more matched segments", { count: hiddenSegmentCount })}</small>}</span><span className="result-kind">{formatEntryType(hit.entry_type, t)}</span><ChevronRight className="row-arrow" size={17} /></div>;
}

function SourceCard({ source, locale, t, busy, coreStorageReady, onView, onRecheck }: { source: SourceSummary; locale: Locale; t: Translator; busy: boolean; coreStorageReady: boolean; onView: () => void; onRecheck: (trigger?: EventTarget | null) => void }) {
  const scan = source.latest_scan;
  const scanTime = scan?.finished_at || scan?.started_at;
  const issueCount = (scan?.failed_entries ?? 0) + (scan?.unstable_entries ?? 0) + (scan?.detection_failures ?? 0);
  const path = source.locator?.trim() || t("Unnamed source");
  return <article className={`source-card ${source.reachability === "AVAILABLE" ? "available" : "offline"}`}>
    <header className="source-card-header"><span className="source-card-icon"><HardDrive size={17} /></span><div className="source-card-title"><strong title={path}>{path}</strong><small>{formatSourceKind(source.kind, t)}</small></div><span className={`source-health ${source.reachability === "AVAILABLE" ? "available" : "offline"}`}><span />{formatReachability(source.reachability, t)}</span></header>
    <div className="source-card-state"><span>{t("Recorded state")}</span><strong>{formatSourceState(source.state, t)}</strong>{source.reachability_message && <small>{formatReachabilityMessage(source.reachability_message, t)}</small>}</div>
    <div className="source-card-stats">
      <div><span>{t("Last scan")}</span><strong>{scanTime ? formatDateTime(scanTime, locale, t) : t("Not scanned")}</strong><small>{scan ? formatScanState(scan.state, t) : t("No scan recorded")}</small></div>
      <div><span>{t("Files")}</span><strong>{scan ? String(scan.regular_files ?? 0) : "—"}</strong><small>{scan ? t("regular files") : t("Not measured")}</small></div>
      <div><span>{t("Bytes hashed")}</span><strong>{scan ? formatBytes(scan.bytes_hashed ?? 0) : "—"}</strong><small>{scan ? t("hashed during scan") : t("Not measured")}</small></div>
      <div><span>{t("Issues")}</span><strong className={issueCount ? "has-issues" : "ok"}>{scan ? String(issueCount) : "—"}</strong><small>{scan ? (issueCount ? t("scan findings") : t("No scan findings")) : t("Not measured")}</small></div>
    </div>
    <div className={`source-recovery ${source.latest_snapshot_ref ? "ready" : "pending"}`}><ArchiveRestore size={15} /><div><span>{t("Latest recovery point")}</span><strong>{source.latest_snapshot_ref ? t("Available") : t("Not published yet")}</strong></div></div>
    <p className="muted">{t("Recheck creates a reviewable plan. Existing saved content is not changed until you confirm.")}</p>
      <footer className="source-card-actions"><button type="button" className="secondary-button compact" onClick={(event) => onRecheck(event.currentTarget)} disabled={busy || !coreStorageReady || source.reachability !== "AVAILABLE"}><RefreshCw size={14} />{t("Recheck source")}</button><button type="button" className="secondary-button compact" onClick={onView} disabled={busy || !source.latest_namespace_root_id && !source.latest_snapshot_ref}><Folder size={14} />{t("View content")}</button></footer>
  </article>;
}

function sourceButtonLabel(root: BrowseRoot, roots: BrowseRoot[], t: Translator) {
  const name = root.name?.trim() || t("Source path");
  const duplicateRoots = roots.filter((other) => (other.name?.trim() || t("Source path")) === name);
  const duplicateName = duplicateRoots.length > 1;
  if (!duplicateName) return name;
  const path = root.source_path?.trim();
  if (!path) return `${name} · ${t("Source path")}`;
  const parts = path.split(/[\\/]+/).filter(Boolean);
  for (let depth = 2; depth <= parts.length; depth += 1) {
    const suffix = parts.slice(-depth).join("/");
    const matches = duplicateRoots.filter((other) => {
      const otherParts = other.source_path?.trim().split(/[\\/]+/).filter(Boolean) ?? [];
      return otherParts.slice(-depth).join("/") === suffix;
    });
    if (matches.length === 1) return suffix;
  }
  return path;
}

function Details({ hit, annotations, representations, descriptions, descriptionsTruncated, annotationStatus, representationsStatus, descriptionsStatus, annotationMutationEnabled, noteDraft, tagDraft, tagVocabulary, editingNoteID, editingBody, busy, t, locale, onBack, onDraft, onTagDraft, onAddTag, onDeleteTag, onAdd, onEdit, onDeleteNote, onEditBody, onSaveEdit, onCancelEdit }: { hit: SearchHit; annotations: any[]; representations: any[]; descriptions: any[]; descriptionsTruncated: boolean; annotationStatus: DetailResourceStatus; representationsStatus: DetailResourceStatus; descriptionsStatus: DetailResourceStatus; annotationMutationEnabled: boolean; noteDraft: string; tagDraft: string; tagVocabulary: string[]; editingNoteID: string; editingBody: string; busy: boolean; t: Translator; locale: Locale; onBack: () => void; onDraft: (value: string) => void; onTagDraft: (value: string) => void; onAddTag: () => void; onDeleteTag: (tag: any) => void; onAdd: () => void; onEdit: (note: any) => void; onDeleteNote: (note: any) => void; onEditBody: (value: string) => void; onSaveEdit: (note: any) => void; onCancelEdit: () => void }) {
  const notes = annotations.filter((item) => item.kind === "NOTE" && !item.tombstoned);
  // A successor replaces a previous generated/imported note in the daily
  // view. The complete revision chain remains durable in the catalog.
  const supersededDescriptionIDs = new Set(descriptions.map((item) => item.predecessor_id).filter(Boolean));
  const currentDescriptions = descriptions.filter((item) => !item.tombstoned && !supersededDescriptionIDs.has(item.description_document_id));
  const descriptionNotes = currentDescriptions.map((item) => {
    const body = typeof item.body === "string" && item.body.trim()
      ? item.body
      : typeof item.title === "string" && item.title.trim()
        ? item.title
        : t("Note content unavailable");
    return {
      ...item,
      body,
      sourceLabel: descriptionKindLabel(item.kind, t),
      descriptionOnly: true,
    };
  });
  const combinedNotes = [...notes, ...descriptionNotes];
  const tags = annotations.filter((item) => item.kind === "TAG" && !item.tombstoned);
  const systemTags = deriveSystemTags(hit);
  const suggestions = tagVocabulary.filter((value) => !tags.some((tag) => tag.body === value));
  const exact = representations.find((item) => item.class === "EXACT" && item.authoritative === true);
  const verifiedExact = Boolean(exact && exact.placement === "present" && exact.verified === true);
  const exactState = representationsStatus === "loading" ? "Checking service" : representationsStatus === "error" ? "Unavailable" : verifiedExact ? "Verified exact bytes" : exact?.placement === "missing" || exact?.verified === false ? "Exact copy is missing or failed verification" : exact ? "Exact copy is present but not verified" : "No authoritative exact copy reported";
  const assuranceLabel = representationsStatus === "loading" ? "Checking service" : representationsStatus === "error" ? "Unavailable" : verifiedExact ? "VERIFIED" : exact ? "UNVERIFIED" : "CATALOGED";
  const detailStatus = (state: DetailResourceStatus) => state === "loading" ? t("Checking service") : state === "error" ? t("Unavailable") : "";
  const canMutateAnnotations = annotationMutationEnabled && annotationStatus === "ready";
  const noteMeta = (note: any) => {
    if (note.descriptionOnly) {
      const parts = [note.sourceLabel || t("Imported note")];
      if (note.accepted === false) parts.push(t("Not accepted"));
      if (note.revision) parts.push(t("revision {{revision}}", { revision: note.revision }));
      if (note.updated_at) parts.push(formatNoteDate(note.updated_at, locale, t));
      if (note.body_unavailable) parts.push(t("Body unavailable"));
      return parts.join(" · ");
    }
    return `${formatNoteDate(note.updated_at, locale, t)} · ${t("revision {{revision}}", { revision: note.revision })}`;
  };
  const renderNote = (note: any) => {
    const noteID = note.annotation_id || note.description_document_id || `${note.kind || "note"}-${note.revision || note.body}`;
    const editable = !note.descriptionOnly;
    if (editable && editingNoteID === note.annotation_id) {
      return <div className="note-row editing" key={noteID}><textarea autoFocus aria-label={t("Edit note")} value={editingBody} onChange={(event) => onEditBody(event.target.value)} disabled={!canMutateAnnotations} /><div className="note-actions"><button className="text-button" onClick={onCancelEdit} disabled={busy}>{t("Cancel")}</button><button className="primary-button compact" onClick={() => onSaveEdit(note)} disabled={busy || !canMutateAnnotations || !editingBody.trim()}><Check size={14} />{t("Save")}</button></div></div>;
    }
    return <div className={`note-row ${note.descriptionOnly ? "source-note" : ""}`} key={noteID}><p>{note.body}</p><div className="note-meta"><span>{noteMeta(note)}</span>{editable && <span className="note-icon-actions"><button className="note-edit" title={t("Edit note")} aria-label={t("Edit note")} onClick={() => onEdit(note)} disabled={busy || !canMutateAnnotations}><Pencil size={13} /></button><button className="note-edit danger" title={t("Delete note")} aria-label={t("Delete note")} onClick={() => onDeleteNote(note)} disabled={busy || !canMutateAnnotations}><Trash2 size={13} /></button></span>}</div></div>;
  };
  return <div className="details">
    <button className="detail-back text-button" onClick={onBack}><ArrowLeft size={15} />{t("Back to results")}</button>
    <div className="detail-title"><span className="file-icon large"><FileSearch size={23} /></span><div className="detail-title-copy"><p className="eyebrow">{t("CONTENT ITEM")}</p><h2>{hit.name || t("Untitled subject")}</h2><small>{formatEntryType(hit.entry_type, t)}</small></div><span className={`assurance-seal ${verifiedExact ? "" : "warning"}`}><ShieldCheck size={16} />{t(assuranceLabel)}</span></div>
    <div className="identity-band"><div className="identity-label"><span>{t("CONTENT WEAVE")}</span><small>{abbreviateIdentity(hit.content_id)}</small></div><IdentityWeave value={hit.content_id || hit.subject_ref} t={t} /></div>
    <dl className="facts"><div><dt>{t("Original path")}</dt><dd>{hit.path || t("Not recorded")}</dd></div><div><dt>{t("Content identity (SHA-256 + length)")}</dt><dd className="mono">{hit.content_id || t("Not available in this result")}</dd></div><div><dt>{t("Protection")}</dt><dd><span className={`inline-state ${verifiedExact ? "" : "warning"}`}>{verifiedExact ? <CheckCircle2 size={15} /> : <AlertCircle size={15} />}{t(exactState)}</span></dd></div>{exact && <div><dt>{t("Logical size")}</dt><dd>{formatBytes(exact.decoded_length)}</dd></div>}</dl>
    <div className="detail-section tags-section"><div className="section-heading"><h3>{t("Tags")}</h3><span>{tags.length}</span></div>
      {annotationStatus !== "ready" ? <p className="muted" role="status">{detailStatus(annotationStatus)}</p> : <div className="tag-cloud">{systemTags.map((value) => <span className="tag-chip system" title={t("System field")} key={value}><Hash size={12} />{value}</span>)}{tags.map((tag) => <span className="tag-chip user" key={tag.annotation_id}><Tag size={12} />{tag.body}<button title={t("Remove tag")} aria-label={t("Remove tag {{tag}}", { tag: tag.body })} onClick={() => onDeleteTag(tag)} disabled={busy || !canMutateAnnotations}><X size={12} /></button></span>)}{!tags.length && <span className="muted">{t("No user tags yet.")}</span>}</div>}
      <div className="tag-composer"><input list="restoreweave-tag-vocabulary" aria-label={t("New tag")} value={tagDraft} onChange={(event) => onTagDraft(event.target.value)} onKeyDown={(event) => { if (event.key === "Enter" && tagDraft.trim() && canMutateAnnotations) { event.preventDefault(); onAddTag(); } }} placeholder={t("Add or reuse a tag")} disabled={!canMutateAnnotations} /><datalist id="restoreweave-tag-vocabulary">{suggestions.map((value) => <option value={value} key={value} />)}</datalist><button className="primary-button compact" onClick={onAddTag} disabled={busy || !canMutateAnnotations || !tagDraft.trim()}><Plus size={14} />{t("Add tag")}</button></div>
    </div>
    <div className="detail-section notes-section"><div className="section-heading"><h3>{t("Notes")}</h3><span>{combinedNotes.length}</span></div>
      <div className="notes-list">
        {annotationStatus !== "ready" && <p className="muted" role="status">{detailStatus(annotationStatus)}</p>}
        {descriptionsStatus === "loading" && <p className="muted" role="status">{t("Loading additional notes...")}</p>}
        {descriptionsTruncated && descriptionsStatus === "ready" && <p className="muted" role="status">{t("Showing the latest notes; older note history is not loaded yet.")}</p>}
        {descriptionsStatus === "error" && <p className="muted" role="status">{t("Some imported or generated notes are unavailable.")}</p>}
        {(annotationStatus === "ready" || descriptionsStatus === "ready") && combinedNotes.length > 0 && combinedNotes.map(renderNote)}
        {annotationStatus === "ready" && descriptionsStatus === "ready" && combinedNotes.length === 0 && <p className="muted">{t("No notes yet.")}</p>}
      </div>
      <div className="note-composer"><textarea aria-label={t("New note")} value={noteDraft} onChange={(event) => onDraft(event.target.value)} placeholder={t("Add useful context about this file")} disabled={!canMutateAnnotations} /><button className="primary-button compact" onClick={onAdd} disabled={busy || !canMutateAnnotations || !noteDraft.trim()}><Plus size={14} />{t("Add note")}</button></div>
    </div>
    <div className="detail-footer"><span><ShieldCheck size={14} />{t("Recovery remains independent of search indexes")}</span></div>
  </div>;
}
function PlanCard({ plan, busy, coreStorageReady, t, onApply }: { plan: any; busy: boolean; coreStorageReady: boolean; t: Translator; onApply: () => void }) {
  const blocked = plan.blocked_entries?.length ?? 0;
  const decisions = Array.isArray(plan.protection_decisions) ? plan.protection_decisions : [];
  const warnings: string[] = Array.isArray(plan.warnings) ? (plan.warnings as unknown[]).filter((warning): warning is string => typeof warning === "string" && Boolean(warning.trim())) : [];
  const applied = plan.state === "SUCCEEDED";
  const measured = applied && plan.savings_measured === true;
  const dedupReused = Math.max(0, Number(plan.local_bytes ?? 0) - Number(plan.new_bytes ?? 0));
  return <div className={`plan-card ${applied ? "applied" : ""}`}>
    <div className="plan-title"><div><p className="eyebrow">{t(applied ? "THIS SAVE" : "REVIEW STORAGE PLAN")}</p><h3>{t(plan.state || (plan.executable ? "READY" : "BLOCKED"))}</h3></div>{applied ? <CheckCircle2 size={21} /> : <ShieldCheck size={21} />}</div>
    <div className="plan-stats">
      <span><b>{plan.files ?? 0}</b>{t("files")}</span>
      <span><b>{formatBytes(plan.bytes ?? 0)}</b>{t("logical content")}</span>
      {!applied && <><span><b>{formatBytes(plan.new_bytes ?? 0)}</b>{t("unique bytes to add")}</span><span className={dedupReused > 0 ? "saving" : ""}><b>{formatBytes(dedupReused)}</b>{t("duplicate bytes expected to reuse")}</span></>}
      {applied && measured ? <>
        <span><b>{formatBytes(plan.new_bytes ?? 0)}</b>{t("unique logical bytes stored")}</span>
        <span className={dedupReused > 0 ? "saving measured" : "measured"}><b>{formatBytes(dedupReused)}</b>{t("duplicate bytes reused")}</span>
        <span className="measured"><b>{formatBytes(plan.new_physical_bytes ?? 0)}</b>{t("new payload bytes stored")}</span>
        <span className={Number(plan.compression_saved_bytes ?? 0) > 0 ? "saving measured" : "measured"}><b>{formatBytes(plan.compression_saved_bytes ?? 0)}</b>{t("compression bytes avoided")}</span>
      </> : <span><b>{t("Not measured")}</b>{t("compression / physical")}</span>}
    </div>
    <p className="plan-disclosure">{t(applied ? measured ? "Whole-file deduplication and repository bytes are measured from verified placement receipts for this save. Repository records, indexes, and model files are not included." : "This save completed, but its physical placement outcome was not measurable. No deduplication or physical saving is inferred." : "Deduplication is an exact pre-save estimate. Compression and physical savings require a verified repository measurement and are not guessed here.")}</p>
    <div className="plan-outcomes"><span>{plan.protection_mode || "STORE_EXACT"}</span>{measured && <span>{t("Measured receipt")}</span>}{blocked > 0 && <span className="warn">{t("{{count}} blocked", { count: blocked })}</span>}</div>
    {warnings.length > 0 && <div className="plan-warning" role="status"><div className="plan-warning-title"><AlertCircle size={15} /><strong>{t("Exact bytes are saved; derived processing is unavailable.")}</strong></div><ul>{warnings.map((warning: string, index: number) => <li key={`${warning}-${index}`}>{warning}</li>)}</ul></div>}
    {decisions.length > 0 && <details className="plan-files"><summary><FileText size={15} /><span>{t("Per-file protection decisions")}</span><b>{decisions.length}</b><ChevronRight size={14} /></summary><div className="plan-file-list">{decisions.map((decision: any, index: number) => <div className="plan-file" key={`${decision.relative_path || "entry"}-${index}`}><div className="plan-file-heading"><strong title={decision.relative_path}>{decision.relative_path || t("Unnamed entry")}</strong><span className="plan-file-mode">{decision.mode || "STORE_EXACT"}</span></div><div className="plan-file-meta"><span>{t("Outcome")}: {decision.planned_outcome || t("Not reported")}</span><span>{t("Reason")}: {decision.reason_code || t("Not reported")}</span></div>{decision.expected_content_id && <small className="mono">{t("Content identity")}: {decision.expected_content_id} · {formatBytes(Number(decision.expected_logical_bytes ?? 0))}</small>}</div>)}</div></details>}
    {blocked > 0 && <details className="plan-files blocked-files"><summary><AlertCircle size={15} /><span>{t("Blocked entries")}</span><b>{blocked}</b><ChevronRight size={14} /></summary><div className="plan-file-list">{plan.blocked_entries.map((entry: any, index: number) => <div className="plan-file" key={`${entry.relative_path || "entry"}-${index}`}><div className="plan-file-heading"><strong title={entry.relative_path}>{entry.relative_path || t("Unnamed entry")}</strong><span className="plan-file-mode">{entry.mode || t("Not reported")}</span></div><div className="plan-file-meta"><span>{t("Outcome")}: {entry.planned_outcome || t("Not reported")}</span><span>{t("State")}: {entry.state || t("Not reported")}</span></div><small>{entry.reason_code || t("Not reported")}{entry.message ? ` · ${entry.message}` : ""}</small></div>)}</div></details>}
    {plan.executable && !applied ? <button className="primary-button" onClick={onApply} disabled={busy || !coreStorageReady}>{t(busy ? "Saving content..." : "Save exact copy")}</button> : <p className="muted">{t(blocked > 0 ? "Resolve blocked entries before confirming." : "This plan has already been applied.")}</p>}
  </div>;
}
function RestoreCard({ plan, busy, coreStorageReady, t, onApply }: { plan: any; busy: boolean; coreStorageReady: boolean; t: Translator; onApply: () => void }) { return <div className="plan-card"><div className="plan-title"><div><p className="eyebrow">{t("REVIEW RESTORE")}</p><h3>{t(plan.state || "READY")}</h3></div><ArchiveRestore size={21} /></div><div className="plan-stats"><span><b>{plan.files ?? 0}</b>{t("files")}</span><span><b>{formatBytes(plan.bytes ?? 0)}</b>{t("verified output")}</span></div><div className="plan-outcomes"><span>{t("EXACT BYTES")}</span></div>{plan.executable && plan.state !== "SUCCEEDED" ? <button className="primary-button" onClick={onApply} disabled={busy || !coreStorageReady}>{t(busy ? "Restoring..." : "Confirm restore")}</button> : <p className="muted">{t("Restore is not executable.")}</p>}</div>; }
function SettingsPanel({ section, configData, draft, loading, dirty, lexicalReady, semanticReady, semanticBundleReady, semanticBundleRestartRequired, semanticBundleOperatorProvided, semanticInstallAvailable, searchRebuildAvailable, semanticInstall, searchRebuild, statusData, workspaceID, locale, t, onLocale, onSection, onClose, onField, onRepositoryProfile, onReload, onSave, onInstallSemanticBundle, onRebuildSearch }: { section: SettingsSection; configData: any; draft: any; loading: boolean; dirty: boolean; lexicalReady: boolean; semanticReady: boolean; semanticBundleReady: boolean; semanticBundleRestartRequired: boolean; semanticBundleOperatorProvided: boolean; semanticInstallAvailable: boolean; searchRebuildAvailable: boolean; semanticInstall: ActionFeedback; searchRebuild: ActionFeedback; statusData: any; workspaceID: string; locale: Locale; t: Translator; onLocale: (locale: Locale) => void; onSection: (section: SettingsSection) => void; onClose: () => void; onField: (section: string, field: string, value: unknown) => void; onRepositoryProfile: (profile: string) => void; onReload: () => void; onSave: () => void; onInstallSemanticBundle: () => void; onRebuildSearch: () => void }) {
  const sections: Array<{ id: SettingsSection; label: string; icon: ReactNode }> = [
    { id: "storage", label: t("Storage & protection"), icon: <HardDrive size={17} /> },
    { id: "search", label: t("Search"), icon: <Search size={17} /> },
    { id: "descriptions", label: t("Notes"), icon: <FileText size={17} /> },
    { id: "recovery", label: t("Recovery & trust"), icon: <ShieldCheck size={17} /> },
    { id: "service", label: t("Service & access"), icon: <Server size={17} /> },
  ];
  const config = draft ?? {};
  const semanticMode = config.semantic?.embedding_mode ?? "local";
  const localProfile = String(config.semantic?.local_profile || "bge-small-zh-v1.5").trim() || "bge-small-zh-v1.5";
  const pinnedLocalProfile = localProfile === "bge-small-zh-v1.5";
  const localBundleUsable = pinnedLocalProfile || semanticBundleOperatorProvided;
  const fixedBundleDownloadable = pinnedLocalProfile && !semanticBundleOperatorProvided;
  const actionBlocked = dirty || Boolean(configData?.restart_required);
  const restartRequired = Boolean(configData?.restart_required) || semanticBundleRestartRequired;
  // A successful install action is not capability admission; only an
  // admitted bundle may appear installed or usable.
  const bundleInstalled = localBundleUsable && semanticBundleReady;
  const bundleStateLabel = semanticInstall.state === "RUNNING"
    ? "Installing..."
    : !semanticBundleReady
      ? (semanticBundleOperatorProvided ? "Operator-provided bundle unavailable or verification failed" : "Model not installed or verification failed")
      : semanticBundleRestartRequired
        ? "Installed; restart required"
        : semanticReady
          ? "Installed and running"
          : "Model installed; index not ready";
  const bundleNotice = !semanticBundleReady
    ? (semanticBundleOperatorProvided
      ? "The operator-provided local embedding bundle is unavailable or failed verification. Keyword search and exact recovery remain available."
      : "The local BGE bundle is missing or failed verification. Keyword search and exact recovery remain available.")
    : semanticBundleRestartRequired
      ? "BGE installed. Restart the service to activate it."
      : semanticBundleOperatorProvided
        ? "This local embedding profile is supplied by the operator. RestoreWeave does not download or verify it here."
        : bundleInstalled
        ? "The verified local BGE bundle is installed. Semantic search becomes ready after a compatible index is opened or rebuilt."
        : "The local BGE bundle is missing or failed verification. Keyword search and exact recovery remain available.";
  const installDisabled = actionBlocked || !fixedBundleDownloadable || !semanticInstallAvailable || semanticInstall.state === "RUNNING";
  // The rebuild operation always repairs the baseline lexical projection and
  // opportunistically rebuilds semantic vectors when an admitted provider is
  // present.  Keep the action visible even when BGE is not installed so a
  // user can restore keyword search without first downloading a model.
  const rebuildVisible = searchRebuildAvailable && Boolean(workspaceID) && !restartRequired && semanticInstall.state !== "SUCCEEDED";
  const rebuildDisabled = actionBlocked || !searchRebuildAvailable || !workspaceID || searchRebuild.state === "RUNNING";
  const closeButton = useRef<HTMLButtonElement>(null);
  const content = useRef<HTMLDivElement>(null);
  useEffect(() => { closeButton.current?.focus(); }, []);
  useEffect(() => { content.current?.scrollTo({ top: 0 }); }, [section]);
  return <div className="drawer-backdrop settings-backdrop" onMouseDown={(event) => { if (event.target === event.currentTarget) onClose(); }}>
    <aside className="settings-page" role="dialog" aria-modal="true" aria-label={t("Settings")}>
      <header className="settings-header"><div><p className="eyebrow">{t("RESTOREWEAVE ADMIN")}</p><h2>{t("Settings")}</h2></div><div className="settings-header-actions"><div className="language-switch" role="group" aria-label={t("Switch language")}><button aria-pressed={locale === "zh-CN"} title={t("Chinese")} onClick={() => onLocale("zh-CN")}>中</button><button aria-pressed={locale === "en"} title={t("English")} onClick={() => onLocale("en")}>EN</button></div><span className={`config-state ${dirty ? "dirty" : configData?.restart_required ? "restart" : "saved"}`}>{t(dirty ? "Unsaved changes" : configData?.restart_required ? "Restart required" : "Saved")}</span><button className="icon-button" title={t("Reload configuration")} aria-label={t("Reload configuration")} onClick={onReload} disabled={loading}><RefreshCw className={loading ? "spin" : ""} size={17} /></button><button ref={closeButton} className="icon-button" aria-label={t("Close settings")} onClick={onClose}><X size={17} /></button></div></header>
      <div className="settings-layout">
        <nav className="settings-nav" aria-label={t("Settings sections")}>{sections.map((item) => <button className={section === item.id ? "active" : ""} aria-current={section === item.id ? "page" : undefined} key={item.id} onClick={() => onSection(item.id)}>{item.icon}<span>{item.label}</span><ChevronRight size={14} /></button>)}</nav>
        <div ref={content} className="settings-content">
          {loading && !draft ? <div className="settings-loading"><LoaderCircle className="spin" size={24} /><span>{t("Loading configuration")}</span></div> : <>
            {section === "storage" && <section className="config-section"><ConfigHeading eyebrow={t("CONTENT LOCATIONS")} title={t("Storage")} copy={t("Choose where RestoreWeave keeps its catalog, exact bytes, indexes, and models; signed portable recovery records live with the content repository.")} />
              <ConfigGroup title={t("Storage plan")} copy={t("Choose how exact file bytes are stored. Both options keep SHA-256 whole-file deduplication.")}><ConfigOptions value={config.storage?.repository_profile} onChange={onRepositoryProfile} options={[{ value: "directory-cas-dev-v1", title: t("Directory CAS"), state: t("Development"), copy: t("Stores exact bytes without compression. Best for inspection and compatibility tests."), detail: t("Identity compression") }, { value: "local-zstd-v1", title: t("Local zstd"), state: t("Candidate"), copy: t("Keeps exact recovery while compressing each unique whole file."), detail: t("Lossless zstd") }]} /></ConfigGroup>
              <ConfigGroup title={t("Content location")} copy={t("The repository contains exact file bytes; changing this path does not move existing data.")}><div className="form-grid two"><ConfigInput label={t("Content repository")} value={config.paths?.repository} onChange={(value) => onField("paths", "repository", value)} /><ConfigReadonly label={t("Compression")} value={config.storage?.compression_profile} t={t} /><ConfigReadonly label={t("Default storage mode")} value="STORE_EXACT" t={t} /><ConfigToggle label={t("Allow explicit link-only plans")} copy={t("The user must still confirm that bytes are not stored locally.")} checked={Boolean(config.storage?.allow_link_only)} onChange={(value) => onField("storage", "allow_link_only", value)} /></div></ConfigGroup>
              <details className="config-advanced"><summary>{t("Advanced paths")}<ChevronRight size={15} /></summary><p>{t("Catalog, vector, model, and signing-material paths for operators and migrations. Signed portable recovery records live with the content repository.")}</p><div className="form-grid"><ConfigInput label={t("Catalog database")} value={config.paths?.catalog} onChange={(value) => onField("paths", "catalog", value)} /><ConfigInput label={t("Vector indexes")} value={config.paths?.vectors} onChange={(value) => onField("paths", "vectors", value)} /><ConfigInput label={t("Models")} value={config.paths?.models} onChange={(value) => onField("paths", "models", value)} /><ConfigInput label={t("Signing material & recovery workflow")} value={config.paths?.recovery_records} onChange={(value) => onField("paths", "recovery_records", value)} /></div></details>
              <ConfigNotice kind="warning">{t("Changing a repository or catalog path does not move existing data. Restart only after the target has been prepared or migrated.")}</ConfigNotice>
            </section>}
            {section === "search" && <section className="config-section"><ConfigHeading eyebrow={t("DISCOVERY")} title={t("Search")} copy={t("Keyword search is always local. Semantic search uses the selected provider profile and disposable zvec generations.")} />
              <ConfigGroup title={t("Provider health")} copy={t("See what is available now before changing the provider profile.")}><div className="running-label"><span />{t("Current running state")}</div><div className="provider-status" aria-live="polite"><span><Search size={17} />{t("Keyword search")}</span><strong className={lexicalReady ? "ready" : "offline"}>{t(lexicalReady ? "Ready" : "Offline")}</strong><span><Sparkles size={17} />{t("Semantic search")}</span><strong className={semanticReady ? "ready" : "offline"}>{t(semanticReady ? "Ready" : "Offline")}</strong></div></ConfigGroup>
              <ConfigGroup title={t("Semantic provider")} copy={t("Select local BGE, an admitted online replacement, or a hybrid profile.")}><div className="running-label draft"><span />{t("Configuration to save")}</div><div className="segmented" role="group" aria-label={t("Embedding mode")}>{["local", "online", "hybrid"].map((mode) => <button className={semanticMode === mode ? "active" : ""} aria-pressed={semanticMode === mode} key={mode} onClick={() => onField("semantic", "embedding_mode", mode)}>{semanticMode === mode && <Check size={14} />}{t(mode === "local" ? "Local" : mode === "online" ? "Online" : "Hybrid")}</button>)}</div>
              {semanticMode !== "online" && <div className={`model-card ${semanticReady ? "ready" : bundleInstalled ? "installed" : "offline"}`}><span className="model-icon"><Sparkles size={19} /></span><div><strong>{pinnedLocalProfile ? "BAAI/bge-small-zh-v1.5" : localProfile}</strong><small>{t(semanticBundleOperatorProvided || !pinnedLocalProfile ? "Operator-provided local embedding profile" : "Pinned local ONNX embedding profile")}</small></div><span className="model-state" role="status" aria-live="polite">{t(bundleStateLabel)}</span><div className="model-meta"><span>zvec</span><span>{config.paths?.models || t("Models path not reported")}</span></div>{fixedBundleDownloadable && !bundleInstalled && <div className="model-action"><button type="button" className="primary-button compact" onClick={onInstallSemanticBundle} disabled={installDisabled}><DownloadIcon />{t(semanticInstall.state === "RUNNING" ? "Installing..." : "Download and verify fixed BGE (about 100MB model; full runtime is larger)")}</button>{actionBlocked && <small>{t(configData?.restart_required ? "Restart required before model actions." : "Save settings before model actions.")}</small>}{!semanticInstallAvailable && !actionBlocked && <small>{t("This build cannot install the local BGE bundle.")}</small>}</div>}{semanticInstall.message && <div className={`model-action-message ${semanticInstall.state === "FAILED" ? "error" : "success"}`}>{semanticInstall.message}</div>}{rebuildVisible && <div className="model-action"><button type="button" className="secondary-button compact" onClick={onRebuildSearch} disabled={rebuildDisabled}><RefreshCw size={14} className={searchRebuild.state === "RUNNING" ? "spin" : ""} />{t(searchRebuild.state === "RUNNING" ? "Rebuilding search indexes..." : "Rebuild search indexes")}</button>{actionBlocked && <small>{t(configData?.restart_required ? "Restart required before model actions." : "Save settings before model actions.")}</small>}{!workspaceID && <small>{t("Add a source before rebuilding search indexes.")}</small>}{!searchRebuildAvailable && !actionBlocked && workspaceID && <small>{t("Search rebuild is unavailable in this build.")}</small>}</div>}{searchRebuild.message && <div className={`model-action-message ${searchRebuild.state === "FAILED" || searchRebuild.state === "DEGRADED" ? "error" : "success"}`}>{searchRebuild.message}</div>}</div>}
              <div className="form-grid two"><ConfigReadonly label={t("Local embedding profile")} value={config.semantic?.local_profile || "bge-small-zh-v1.5"} t={t} /><ConfigReadonly label={t("Vector backend")} value={config.semantic?.vector_backend || "zvec"} t={t} />{semanticMode !== "local" && <><ConfigInput label={t("Online provider profile")} value={config.semantic?.online_profile} placeholder="installed-provider-profile" onChange={(value) => onField("semantic", "online_profile", value)} /><ConfigInput label={t("Credential reference")} value={config.semantic?.online_credential_ref} placeholder="keychain://restoreweave/provider" onChange={(value) => onField("semantic", "online_credential_ref", value)} /><ConfigToggle label={t("Send content without per-request confirmation")} copy={t("Keep off unless this provider and its data policy are explicitly trusted.")} checked={Boolean(config.semantic?.send_content_without_confirmation)} onChange={(value) => onField("semantic", "send_content_without_confirmation", value)} /></>}</div></ConfigGroup>
              {semanticMode === "local" ? <ConfigNotice kind={bundleInstalled ? "info" : "warning"}>{t(bundleNotice)}</ConfigNotice> : <ConfigNotice kind="warning">{t("Online and hybrid are replacement-profile selections. This build will report semantic search unavailable unless that provider is separately installed and admitted.")}</ConfigNotice>}
            </section>}
            {section === "descriptions" && <section className="config-section"><ConfigHeading eyebrow={t("NOTES")} title={t("Notes")} copy={t("Notes remain durable facts; optional AI notes are added only when requested.")} />
              <ConfigGroup title={t("Notes storage")} copy={t("Your notes are always available; imported, extracted, and AI-created text appears in this same Notes list.")}><div className="form-grid two"><ConfigToggle label={t("Keep additional notes")} copy={t("Keep imported, extracted, and generated text alongside your notes.")} checked={Boolean(config.descriptions?.enabled)} onChange={(value) => { onField("descriptions", "enabled", value); onField("descriptions", "generate", value ? "on_demand" : "disabled"); if (value) onField("descriptions", "retain_full_text", true); }} /><ConfigSelect label={t("Generation")} value={config.descriptions?.generate} onChange={(value) => onField("descriptions", "generate", value)} options={[{ value: "disabled", label: t("Disabled") }, { value: "on_demand", label: t("On demand") }]} disabled={!config.descriptions?.enabled} /><ConfigInput label={t("Provider profile")} value={config.descriptions?.provider_profile} placeholder="optional-describe-profile" onChange={(value) => onField("descriptions", "provider_profile", value)} disabled={!config.descriptions?.enabled || config.descriptions?.generate === "disabled"} /><ConfigInput label={t("Credential reference")} value={config.descriptions?.credential_ref} placeholder="keychain://restoreweave/notes" onChange={(value) => onField("descriptions", "credential_ref", value)} disabled={!config.descriptions?.enabled || config.descriptions?.generate === "disabled"} /><ConfigToggle label={t("Retain full text")} copy={t("Required when model-generated notes are enabled.")} checked={Boolean(config.descriptions?.retain_full_text)} onChange={(value) => onField("descriptions", "retain_full_text", value)} disabled={config.descriptions?.enabled && config.descriptions?.generate !== "disabled"} /></div></ConfigGroup>
            </section>}
            {section === "recovery" && <section className="config-section"><ConfigHeading eyebrow={t("SAFETY CONTRACT")} title={t("Recovery")} copy={t("Recovery settings protect the exact-byte promise and signed portable records.")} />
              <ConfigGroup title={t("Recovery authority")} copy={t("These values define the signed recovery lineage. Change them only as part of a reviewed migration.")}><div className="form-grid two"><ConfigReadonly label={t("Exact fallback")} value={t("Required")} t={t} /><ConfigReadonly label={t("Publication signing")} value={config.recovery?.publication_signing} t={t} /><ConfigInput label={t("Publication domain")} value={config.recovery?.publication_domain} onChange={(value) => onField("recovery", "publication_domain", value)} /><ConfigReadonly label={t("Automatic external reacquisition")} value={t("Disabled in the core profile")} t={t} /></div></ConfigGroup>
              <ConfigNotice kind="warning">{t("Changing the publication domain changes signing lineage. Do not change it for an existing repository without a reviewed migration.")}</ConfigNotice>
            </section>}
            {section === "service" && <section className="config-section"><ConfigHeading eyebrow={t("DAEMON")} title={t("Service")} copy={t("Control the bounded HTTP adapter used by this browser. Authentication tokens remain environment-only and are never written here.")} />
              <ConfigGroup title={t("Web API")} copy={t("Configure the loopback adapter used by this browser client.")}><div className="form-grid two"><ConfigToggle label={t("Enable Web API")} copy={t("Required for this browser client after the next restart.")} checked={Boolean(config.api?.enabled)} onChange={(value) => onField("api", "enabled", value)} /><ConfigInput label={t("Listen address")} value={config.api?.listen} onChange={(value) => onField("api", "listen", value)} disabled={!config.api?.enabled} /></div></ConfigGroup>
              <ConfigGroup title={t("Runtime identity")} copy={t("Inspect the active and persisted configuration without exposing credentials.")}><div className="form-grid two"><ConfigReadonly label={t("Configuration file")} value={configData?.config_path} t={t} wide /><ConfigReadonly label={t("Running digest")} value={configData?.running_config_digest} t={t} mono /><ConfigReadonly label={t("Persisted digest")} value={configData?.config_digest} t={t} mono /><ConfigReadonly label={t("Workspace")} value={workspaceID || t("Not created")} t={t} /><ConfigReadonly label={t("Service socket")} value={statusData?.listen || t("Loopback")} t={t} /></div></ConfigGroup>
              <ConfigNotice><KeyRound size={15} />{t("API tokens are supplied with RESTOREWEAVE_API_TOKEN or --api-token; the WebUI never stores plaintext credentials.")}</ConfigNotice>
            </section>}
          </>}
        </div>
      </div>
      <footer className="settings-footer"><span>{t(dirty ? "Review and save your changes." : configData?.restart_required ? "Saved configuration differs from the running daemon." : "Configuration is synchronized.")}</span><button className="primary-button" onClick={onSave} disabled={loading || !dirty}><Save size={16} />{t(loading ? "Saving..." : "Save settings")}</button></footer>
    </aside>
  </div>;
}
function ConfigGroup({ title, copy, children }: { title: string; copy: string; children: ReactNode }) { return <section className="config-group"><div className="config-group-heading"><h4>{title}</h4><p>{copy}</p></div>{children}</section>; }
function ConfigHeading({ eyebrow, title, copy }: { eyebrow: string; title: string; copy: string }) { return <div className="config-heading"><p className="eyebrow">{eyebrow}</p><h3>{title}</h3><p>{copy}</p></div>; }
function ConfigOptions({ value, options, onChange }: { value?: string; options: Array<{ value: string; title: string; state: string; copy: string; detail: string }>; onChange: (value: string) => void }) { return <div className="config-options" role="radiogroup">{options.map((option) => { const selected = value === option.value; return <button type="button" className={`config-option ${selected ? "selected" : ""}`} role="radio" aria-checked={selected} key={option.value} onClick={() => onChange(option.value)}><span className="option-check">{selected ? <Check size={15} /> : null}</span><span className="option-title"><strong>{option.title}</strong><em>{option.state}</em></span><small>{option.copy}</small><span className="option-detail">{option.detail}</span></button>; })}</div>; }
function ConfigInput({ label, value, placeholder, disabled = false, onChange }: { label: string; value?: string; placeholder?: string; disabled?: boolean; onChange: (value: string) => void }) { return <label className="config-field"><span>{label}</span><input value={value ?? ""} placeholder={placeholder} disabled={disabled} onChange={(event) => onChange(event.target.value)} /></label>; }
function ConfigSelect({ label, value, options, disabled = false, onChange }: { label: string; value?: string; options: Array<{ value: string; label: string }>; disabled?: boolean; onChange: (value: string) => void }) { return <label className="config-field"><span>{label}</span><select value={value ?? ""} disabled={disabled} onChange={(event) => onChange(event.target.value)}>{options.map((option) => <option value={option.value} key={option.value}>{option.label}</option>)}</select></label>; }
function ConfigReadonly({ label, value, t, mono = false, wide = false }: { label: string; value?: string; t: Translator; mono?: boolean; wide?: boolean }) { return <div className={`config-field readonly ${wide ? "wide" : ""}`}><span>{label}</span><strong className={mono ? "mono" : ""}>{value || t("Not reported")}</strong></div>; }
function ConfigToggle({ label, copy, checked, disabled = false, onChange }: { label: string; copy: string; checked: boolean; disabled?: boolean; onChange: (value: boolean) => void }) { return <label className={`toggle-field ${disabled ? "disabled" : ""}`}><span className="toggle-copy"><strong>{label}</strong><small>{copy}</small></span><input type="checkbox" checked={checked} disabled={disabled} onChange={(event) => onChange(event.target.checked)} /><i /></label>; }
function ConfigNotice({ kind = "info", children }: { kind?: "info" | "warning"; children: ReactNode }) { return <div className={`config-notice ${kind}`}>{children}</div>; }
function CapacitySummary({ repository, t }: { repository?: any; t: Translator }) {
  const measured = repository?.capacity_state === "AVAILABLE"
    && ["capacity_total", "capacity_free", "capacity_used"].every((key) => typeof repository?.[key] === "number" && Number.isFinite(repository[key]));
  const value = (key: string) => measured ? formatBytes(repository[key]) : t("Unknown");
  const reason = typeof repository?.capacity_reason === "string" ? repository.capacity_reason.trim() : "";
  return <span className={`capacity-summary ${measured ? "ready" : "degraded"}`} aria-label={t("Physical repository capacity")} title={reason || t("Physical repository capacity")}><HardDrive size={15} /><span className="capacity-summary-values"><b>{t("Capacity")}</b><small>{t("Total")}: {value("capacity_total")} · {t("Available")}: {value("capacity_free")} · {t("Used")}: {value("capacity_used")}</small>{!measured && reason && <small className="capacity-reason">{t("Reason")}: {reason}</small>}</span></span>;
}
function IdentityWeave({ value, t, compact = false }: { value?: string; t: Translator; compact?: boolean }) {
  const bars = identityBars(value, compact ? 12 : 32);
  return <span className={`identity-weave ${compact ? "compact" : ""}`} role="img" aria-label={t("Visual identity derived from the content SHA-256")}>{bars.map((bar, index) => <i className={`tone-${bar.tone}`} data-level={bar.level} key={index} />)}</span>;
}
function formatBytes(value: number) {
  if (!value) return "0 B";
  if (value < 1024) return `${value} B`;
  const units = ["KB", "MB", "GB", "TB"];
  let scaled = value;
  let unit = -1;
  while (scaled >= 1024 && unit < units.length - 1) { scaled /= 1024; unit += 1; }
  return `${scaled.toFixed(1)} ${units[unit]}`;
}
function looksLikeRemoteLocator(value: string) { return /^[a-z][a-z0-9+.-]*:\/\//i.test(value.trim()); }
function formatEntryType(value: string | undefined, t: Translator) { return t(value ? value.replaceAll("_", " ") : "FILE"); }
// Search provenance can contain extracted or generated text close to the
// backend's payload limit. Keep the browser projection small while retaining
// the complete segment in the API response and its immutable identifiers.
const MATCHED_TEXT_PREVIEW_CODE_POINTS = 280;
function previewUnicodeText(value: string | undefined, limit = MATCHED_TEXT_PREVIEW_CODE_POINTS) {
  const text = value?.trim() ?? "";
  const codePointLimit = Math.max(1, Math.floor(limit));
  const codePoints = Array.from(text);
  return codePoints.length > codePointLimit ? `${codePoints.slice(0, codePointLimit).join("")}…` : text;
}
function formatIndexStatus(value: string | undefined, t: Translator) {
  const normalized = value?.trim().toUpperCase();
  if (normalized === "READY") return t("READY");
  if (normalized === "NOT_BUILT") return t("Not built");
  if (normalized === "UNAVAILABLE") return t("Unavailable");
  return t("Unknown");
}
function formatCoverageStatus(value: string | undefined, t: Translator) {
  const normalized = value?.trim().toUpperCase();
  if (normalized === "MARKED") return t("Marked");
  if (normalized === "UNMARKED") return t("Unmarked");
  return t("Unknown");
}
function descriptionKindLabel(kind: string | undefined, t: Translator) {
  const normalized = kind?.trim().toUpperCase();
  const labels: Record<string, string> = {
    USER: "User note",
    IMPORTED: "Imported note",
    EXTRACTED: "Extracted note",
    AI_SUMMARY: "AI note",
    AI_ANALYSIS: "AI note",
  };
  if (normalized && labels[normalized]) return t(labels[normalized]);
  return normalized ? `${t("Unknown note type")}: ${normalized}` : t("Note source unavailable");
}
function searchSegmentSourceLabel(segment: SearchSegment, t: Translator) {
  const sourceType = segment.source_type?.trim().toUpperCase();
  const kind = segment.kind?.trim().toUpperCase();
  if (sourceType === "ANNOTATION" && kind === "NOTE") return t("User note");
  if (sourceType === "ARTIFACT" && kind === "EXTRACT") return t("Extracted content");
  if (sourceType === "DESCRIPTION") return descriptionKindLabel(kind, t);
  if (sourceType === "FILENAME" || kind === "FILENAME") return t("Filename");
  const rawLabel = [segment.source_type?.trim(), segment.kind?.trim()].filter(Boolean).join(" / ");
  return rawLabel || t("Matched segment");
}
function searchSegmentMetadata(segment: SearchSegment, t: Translator) {
  const metadata: string[] = [];
  if (segment.producer?.trim()) metadata.push(`${t("Producer")}: ${segment.producer.trim()}`);
  if (segment.language?.trim()) metadata.push(`${t("Language")}: ${segment.language.trim()}`);
  if (typeof segment.accepted === "boolean") metadata.push(segment.accepted ? t("Accepted") : t("Not accepted"));
  return metadata;
}
function formatFacetValue(hit: SearchHit) {
  if (hit.entry_type === "DIRECTORY" || hit.entry_type === "SYMLINK") return "";
  const name = hit.name || hit.path?.split(/[\\/]/).at(-1) || "";
  const dot = name.lastIndexOf(".");
  return dot > 0 && dot < name.length - 1 ? name.slice(dot + 1).toLowerCase() : "";
}
function exactIdentityKey(hit: SearchHit) {
  const contentID = hit.content_id?.trim();
  return contentID && hit.logical_size != null ? JSON.stringify([contentID, hit.logical_size]) : "";
}
function dedupFacetValue(hit: SearchHit, identityCounts: Map<string, number>) {
  const key = exactIdentityKey(hit);
  if (!key) return "";
  return (identityCounts.get(key) ?? 0) > 1 ? "DUPLICATE" : "UNIQUE";
}
function formatDedupFacet(value: string, t: Translator) { return t(value === "DUPLICATE" ? "Duplicate content" : "Unique content"); }
function formatNoteDate(value: string | undefined, locale: Locale, t: Translator) { if (!value) return t("Saved"); const date = new Date(value); return Number.isNaN(date.getTime()) ? t("Saved") : new Intl.DateTimeFormat(locale, { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" }).format(date); }
function formatDateTime(value: string, locale: Locale, t: Translator) { const date = new Date(value); return Number.isNaN(date.getTime()) ? t("Not recorded") : new Intl.DateTimeFormat(locale, { year: "numeric", month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" }).format(date); }
function formatSourceKind(value: string | undefined, t: Translator) { return t(value === "LOCAL_TREE" ? "Local folder" : value || "Source"); }
function formatSourceState(value: string | undefined, t: Translator) {
  const labels: Record<string, string> = { ACTIVE: "Active", DECOMMISSIONED: "Decommissioned", LOST: "Lost", QUARANTINED: "Quarantined", PLANNED: "Planned" };
  return t(labels[value ?? ""] ?? "Not recorded");
}
function formatReachability(value: string | undefined, t: Translator) { return t(value === "AVAILABLE" ? "Available now" : value === "UNAVAILABLE" ? "Not reachable now" : "Access unknown"); }
function formatReachabilityMessage(value: string, t: Translator) {
  const known: Record<string, string> = {
    "source path is not accessible": "Source path is not accessible.",
    "source path is not a directory": "Source path is not a directory.",
  };
  return t(known[value] ?? "Source access could not be checked.");
}
function formatScanState(value: string | undefined, t: Translator) {
  const labels: Record<string, string> = { PLANNED: "Scan planned", RUNNING: "Scan in progress", COMPLETE: "Scan complete", INCOMPLETE: "Scan incomplete", FAILED: "Scan failed", CANCELLED: "Scan cancelled" };
  return t(labels[value ?? ""] ?? "No scan recorded");
}
function deriveSystemTags(hit: SearchHit) {
  const result: string[] = [];
  if (hit.entry_type) result.push(`type:${hit.entry_type.toLowerCase()}`);
  const name = hit.name || hit.path?.split("/").at(-1) || "";
  const dot = name.lastIndexOf(".");
  if (dot > 0 && dot < name.length - 1) result.push(`format:${name.slice(dot + 1).toLowerCase()}`);
  return result;
}

const rootElement = document.getElementById("root")! as HTMLElement & { restoreWeaveRoot?: ReturnType<typeof createRoot> };
const root = rootElement.restoreWeaveRoot ?? createRoot(rootElement);
rootElement.restoreWeaveRoot = root;
root.render(<App />);
export default App;
