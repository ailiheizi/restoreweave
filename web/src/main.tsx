import { useEffect, useMemo, useState } from "react";
import type { FormEvent, ReactNode } from "react";
import { createRoot } from "react-dom/client";
import { AlertCircle, ArchiveRestore, Check, ChevronRight, FileSearch, Hash, LoaderCircle, Pencil, Plus, RefreshCw, Search, Settings, ShieldCheck, SlidersHorizontal, X } from "lucide-react";
import "./styles.css";

type Result = { status?: string; data?: any; reasons?: Array<{ message?: string }> };
type SearchHit = { subject_ref?: string; name?: string; path?: string; entry_type?: string; content_id?: string; segments?: Array<{ matched_text?: string; producer?: string }> };
const apiBase = "/api/v1";
const lexicalDimension = "lexical-metadata-fts";
const semanticDimension = "semantic-embedding";

async function command(operation: string, input: unknown): Promise<Result> {
  const response = await fetch(`${apiBase}/command`, { method: "POST", headers: { "Content-Type": "application/json" }, body: JSON.stringify({ operation, input }) });
  const payload = (await response.json()) as Result;
  if (!response.ok || payload.status === "FAILED") throw new Error(payload.reasons?.[0]?.message ?? "Request failed");
  return payload;
}

function App() {
  const [query, setQuery] = useState("");
  const [source, setSource] = useState("");
  const [workspaceID, setWorkspaceID] = useState(() => window.localStorage.getItem("restoreweave.workspace_id") ?? "");
  const [hits, setHits] = useState<SearchHit[]>([]);
  const [selected, setSelected] = useState<SearchHit>();
  const [plan, setPlan] = useState<any>();
  const [annotations, setAnnotations] = useState<any[]>([]);
  const [representations, setRepresentations] = useState<any[]>([]);
  const [descriptions, setDescriptions] = useState<any[]>([]);
  const [statusData, setStatusData] = useState<any>();
  const [capabilities, setCapabilities] = useState<any[]>([]);
  const [status, setStatus] = useState("Checking service");
  const [notice, setNotice] = useState<{ kind: "success" | "error" | "warning"; text: string }>();
  const [busy, setBusy] = useState(false);
  const [addOpen, setAddOpen] = useState(false);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [noteDraft, setNoteDraft] = useState("");
  const [editingNoteID, setEditingNoteID] = useState("");
  const [editingBody, setEditingBody] = useState("");
  const [snapshotRef, setSnapshotRef] = useState("");
  const [restoreOpen, setRestoreOpen] = useState(false);
  const [restoreDestination, setRestoreDestination] = useState("");
  const [restorePlan, setRestorePlan] = useState<any>();
  const semanticReady = useMemo(() => capabilities.some((item) => item.kind === "index-dimension" && item.id === semanticDimension && item.state === "AVAILABLE"), [capabilities]);
  const isConnected = status === "Connected" || status === "Semantic search unavailable";

  async function refresh() {
    setStatus("Checking service");
    try {
      const [health, statusResult, capabilityResult, snapshotResult] = await Promise.all([fetch(`${apiBase}/healthz`), command("status.get", {}), command("capability.list", {}), command("snapshot.list", {})]);
      setStatus(health.ok && statusResult.status !== "FAILED" ? "Connected" : "Unavailable");
      setStatusData(statusResult.data);
      setCapabilities(capabilityResult.data?.capabilities ?? []);
      const snapshots = snapshotResult.data?.snapshots ?? [];
      setSnapshotRef(snapshots.at(-1)?.snapshot_ref ?? "");
      if (!workspaceID && statusResult.data?.recent_plans?.[0]?.workspace_id) rememberWorkspace(statusResult.data.recent_plans[0].workspace_id);
    } catch { setStatus("Unavailable"); }
  }
  useEffect(() => { void refresh(); }, []);

  async function runSearch(event?: FormEvent) {
    event?.preventDefault();
    if (!query.trim()) return;
    if (!workspaceID) { setNotice({ kind: "warning", text: "Add content first so there is a workspace to search." }); return; }
    setBusy(true); setNotice(undefined);
    try {
      const input: Record<string, unknown> = { workspace_id: workspaceID, query: query.trim() };
      if (semanticReady) input.fuse = [lexicalDimension, semanticDimension];
      else input.dimension = lexicalDimension;
      const response = await command("search.query", input);
      setHits((response.data?.hits ?? []) as SearchHit[]); setSelected(undefined);
      setStatus(response.status === "DEGRADED" || !semanticReady ? "Semantic search unavailable" : "Connected");
    } catch (error) { setNotice({ kind: "error", text: error instanceof Error ? error.message : "Search failed" }); }
    finally { setBusy(false); }
  }
  async function makePlan(event: FormEvent) {
    event.preventDefault(); if (!source.trim()) return;
    setBusy(true); setNotice(undefined);
    try { const response = await command("plan.ingest", { root: source.trim() }); setPlan(response.data); rememberWorkspace(response.data?.workspace_id); setNotice({ kind: response.data?.executable ? "success" : "warning", text: response.data?.executable ? "Protection plan is ready to review." : "This source has blocked entries and cannot be applied yet." }); }
    catch (error) { setNotice({ kind: "error", text: error instanceof Error ? error.message : "Could not create plan" }); }
    finally { setBusy(false); }
  }
  async function applyPlan() {
    if (!plan?.plan_id || !plan?.plan_digest) return;
    setBusy(true);
    try { const response = await command("plan.apply", { workspace_id: plan.workspace_id, plan_id: plan.plan_id, plan_digest: plan.plan_digest }); setPlan({ ...plan, ...response.data, state: response.data?.state ?? "SUCCEEDED" }); rememberWorkspace(response.data?.workspace_id ?? plan.workspace_id); setAddOpen(false); setNotice({ kind: "success", text: "Protection completed. Exact content is now in the repository." }); await refresh(); }
    catch (error) { setNotice({ kind: "error", text: error instanceof Error ? error.message : "Could not apply plan" }); }
    finally { setBusy(false); }
  }
  async function loadDetails(hit: SearchHit) {
    setSelected(hit); setAnnotations([]); setRepresentations([]); setDescriptions([]);
    setEditingNoteID(""); setEditingBody(""); setNoteDraft("");
    if (!workspaceID || !hit.subject_ref) return;
    const requests = await Promise.allSettled([
      command("annotation.list", { workspace_id: workspaceID, subject_ref: hit.subject_ref }),
      command("representation.list", { workspace_id: workspaceID, subject_ref: hit.subject_ref }),
      command("description.list", { workspace_id: workspaceID, subject_ref: hit.subject_ref }),
    ]);
    if (requests[0].status === "fulfilled") setAnnotations(requests[0].value.data?.annotations ?? []);
    if (requests[1].status === "fulfilled") setRepresentations(requests[1].value.data?.representations ?? []);
    if (requests[2].status === "fulfilled") setDescriptions(requests[2].value.data?.documents ?? []);
  }
  async function saveNote(annotationID = "", body = "", expectedRevision = 0) {
    if (!selected?.subject_ref || !body.trim() || !workspaceID) return;
    setBusy(true);
    try { const response = await command("annotation.upsert", { workspace_id: workspaceID, subject_ref: selected.subject_ref, kind: "NOTE", body: body.trim(), annotation_id: annotationID || undefined, expected_revision: expectedRevision }); setAnnotations((items) => annotationID ? items.map((item) => item.annotation_id === annotationID ? response.data.annotation : item) : [...items, response.data.annotation]); setNoteDraft(""); setEditingNoteID(""); setEditingBody(""); setNotice({ kind: "success", text: annotationID ? "Note updated." : "Note added." }); }
    catch (error) { setNotice({ kind: "error", text: error instanceof Error ? error.message : "Could not save annotation" }); }
    finally { setBusy(false); }
  }
  async function makeRestorePlan(event: FormEvent) {
    event.preventDefault(); if (!snapshotRef || !restoreDestination.trim()) return;
    setBusy(true); setNotice(undefined);
    try { const response = await command("plan.restore", { snapshot_ref: snapshotRef, destination: restoreDestination.trim() }); setRestorePlan(response.data); setNotice({ kind: "success", text: "Restore plan is ready to review." }); }
    catch (error) { setNotice({ kind: "error", text: error instanceof Error ? error.message : "Could not create restore plan" }); }
    finally { setBusy(false); }
  }
  async function applyRestore() {
    if (!restorePlan?.plan_id || !restorePlan?.plan_digest || !restorePlan?.workspace_id) return;
    setBusy(true);
    try { const response = await command("plan.apply", { workspace_id: restorePlan.workspace_id, plan_id: restorePlan.plan_id, plan_digest: restorePlan.plan_digest }); setRestorePlan({ ...restorePlan, ...response.data, state: response.data?.state ?? "SUCCEEDED" }); setRestoreOpen(false); setNotice({ kind: "success", text: `Restored and verified ${response.data?.files ?? restorePlan.files} file(s) to ${response.data?.destination ?? restoreDestination}.` }); }
    catch (error) { setNotice({ kind: "error", text: error instanceof Error ? error.message : "Could not restore snapshot" }); }
    finally { setBusy(false); }
  }
  function rememberWorkspace(value: unknown) { if (typeof value === "string" && value.trim()) { setWorkspaceID(value); window.localStorage.setItem("restoreweave.workspace_id", value); } }

  return <div className="app-shell">
    <header className="topbar"><a className="brand" href="/" aria-label="RestoreWeave home"><span className="brand-mark"><ShieldCheck size={19} /></span><span>RestoreWeave</span></a><form className="global-search" onSubmit={runSearch}><Search size={17} aria-hidden="true" /><input aria-label="Search protected content" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="Search names, notes, descriptions..." /><button type="submit" disabled={busy || !query.trim()}>{busy ? <LoaderCircle className="spin" size={16} /> : "Search"}</button></form><div className="top-actions"><button className="secondary-button add-button" aria-label="Add content" onClick={() => { setAddOpen(true); setNotice(undefined); }}><Plus size={16} /><span>Add content</span></button><button className="icon-button" title="Refresh service status" aria-label="Refresh service status" onClick={() => void refresh()}><RefreshCw size={17} /></button><button className="icon-button" title="Open settings" aria-label="Open settings" onClick={() => setSettingsOpen(true)}><Settings size={17} /></button><span className={`connection ${isConnected ? "ok" : "bad"}`} aria-label={status}><i /><span>{status}</span></span></div></header>
    <main className="workspace"><section className="content-column"><div className="workspace-heading"><div><p className="eyebrow">YOUR LIBRARY</p><h1>{query ? `Results for "${query}"` : "Protected content"}</h1></div><span className="result-count">{hits.length ? `${hits.length} results` : workspaceID ? "Ready to search" : "No workspace yet"}</span></div>{notice && <div className={`notice ${notice.kind}`} role={notice.kind === "error" ? "alert" : "status"}>{notice.kind === "error" ? <AlertCircle size={16} /> : notice.kind === "warning" ? <SlidersHorizontal size={16} /> : <Check size={16} />}{notice.text}<button aria-label="Dismiss notice" onClick={() => setNotice(undefined)}><X size={14} /></button></div>}{hits.length ? <div className="result-list">{hits.map((hit, index) => <ResultRow key={hit.subject_ref ?? index} hit={hit} selected={selected?.subject_ref === hit.subject_ref} onSelect={() => void loadDetails(hit)} />)}</div> : <div className="empty-state"><FileSearch size={29} /><h2>{workspaceID ? "Search your protected library" : "Start by adding content"}</h2><p>{workspaceID ? "Search by filename, path, notes, descriptions, or meaning." : "Review a folder before protection. Nothing is stored until you confirm the plan."}</p><button className="primary-button" onClick={() => setAddOpen(true)}><Plus size={16} />Add content</button></div>}</section><aside className="detail-column">{selected ? <Details hit={selected} annotations={annotations} representations={representations} descriptions={descriptions} noteDraft={noteDraft} editingNoteID={editingNoteID} editingBody={editingBody} busy={busy} canRestore={Boolean(snapshotRef)} onRestore={() => { setRestorePlan(undefined); setRestoreDestination(""); setRestoreOpen(true); setNotice(undefined); }} onDraft={setNoteDraft} onAdd={() => void saveNote("", noteDraft)} onEdit={(note) => { setEditingNoteID(note.annotation_id); setEditingBody(note.body); }} onEditBody={setEditingBody} onSaveEdit={(note) => void saveNote(note.annotation_id, editingBody, note.revision)} onCancelEdit={() => { setEditingNoteID(""); setEditingBody(""); }} /> : <div className="detail-placeholder"><Hash size={23} /><h2>Select an item</h2><p>Details, protection evidence, and notes appear here.</p></div>}</aside></main>
    {addOpen && <div className="drawer-backdrop" onMouseDown={(event) => { if (event.target === event.currentTarget) setAddOpen(false); }}><aside className="drawer" aria-label="Add content"><div className="drawer-header"><div><p className="eyebrow">PROTECT</p><h2>Add content</h2></div><button className="icon-button" aria-label="Close add content" onClick={() => setAddOpen(false)}><X size={17} /></button></div><p className="drawer-copy">Choose a folder, inspect the plan, then confirm exact protection for every file inside.</p><form className="stack-form" onSubmit={makePlan}><label>Folder path<input autoFocus value={source} onChange={(event) => setSource(event.target.value)} placeholder="/data/to-protect" /></label><button className="primary-button" disabled={busy || !source.trim()}>{busy ? "Inspecting..." : "Preview protection"}</button></form>{plan && <PlanCard plan={plan} busy={busy} onApply={() => void applyPlan()} />}</aside></div>}
    {settingsOpen && <div className="drawer-backdrop" onMouseDown={(event) => { if (event.target === event.currentTarget) setSettingsOpen(false); }}><aside className="drawer settings-drawer" aria-label="Settings"><div className="drawer-header"><div><p className="eyebrow">STATUS</p><h2>Settings</h2></div><button className="icon-button" aria-label="Close settings" onClick={() => setSettingsOpen(false)}><X size={17} /></button></div><SettingGroup title="Storage"><SettingRow label="Catalog" value={statusData?.catalog?.path} state={statusData?.catalog?.ok} /><SettingRow label="Repository" value={statusData?.repository?.path} state={statusData?.repository?.ok} /><SettingRow label="Compression" value={statusData?.repository?.compression_profile} /></SettingGroup><SettingGroup title="Search"><SettingRow label="Lexical index" value="Metadata FTS" state={capabilities.some((item) => item.id === lexicalDimension && item.state === "AVAILABLE")} /><SettingRow label="Semantic index" value={semanticReady ? "BGE + zvec" : "Unavailable; lexical search remains"} state={semanticReady} /><SettingRow label="Workspace" value={workspaceID || "Not created"} /></SettingGroup><SettingGroup title="Protection"><SettingRow label="Identity" value="SHA-256 exact bytes" /><SettingRow label="Default" value="STORE_EXACT" /><SettingRow label="Recovery" value="Signed records and exact restore" /></SettingGroup><SettingGroup title="Service"><SettingRow label="Daemon socket" value={statusData?.listen || "Loopback"} state={isConnected} /><SettingRow label="Plans" value={statusData?.plans == null ? "-" : String(statusData.plans)} /><SettingRow label="Jobs" value={statusData?.jobs == null ? "-" : String(statusData.jobs)} /><SettingRow label="Config" value={statusData?.config_digest} /></SettingGroup><p className="settings-note">Settings are read-only here. Storage and model locations come from the daemon config.</p></aside></div>}
    {restoreOpen && <div className="drawer-backdrop" onMouseDown={(event) => { if (event.target === event.currentTarget) setRestoreOpen(false); }}><aside className="drawer" aria-label="Restore snapshot"><div className="drawer-header"><div><p className="eyebrow">RECOVER</p><h2>Restore snapshot</h2></div><button className="icon-button" aria-label="Close restore" onClick={() => setRestoreOpen(false)}><X size={17} /></button></div><p className="drawer-copy">Restore the latest protected snapshot into a new, empty directory and verify every file.</p><form className="stack-form" onSubmit={makeRestorePlan}><label>Destination folder<input autoFocus value={restoreDestination} onChange={(event) => { setRestoreDestination(event.target.value); setRestorePlan(undefined); }} placeholder="/data/restored-copy" /></label><button className="primary-button" disabled={busy || !restoreDestination.trim()}>{busy ? "Inspecting..." : "Preview restore"}</button></form>{restorePlan && <RestoreCard plan={restorePlan} busy={busy} onApply={() => void applyRestore()} />}</aside></div>}
  </div>;
}

function ResultRow({ hit, selected, onSelect }: { hit: SearchHit; selected: boolean; onSelect: () => void }) { return <button className={`result-row ${selected ? "selected" : ""}`} onClick={onSelect}><span className="file-icon"><FileSearch size={17} /></span><span className="result-copy"><strong>{hit.name || "Untitled subject"}</strong><small>{hit.path || hit.entry_type || "Protected content"}</small>{hit.segments?.[0]?.matched_text && <em>{hit.segments[0].matched_text}</em>}</span><ChevronRight className="row-arrow" size={16} /></button>; }
function Details({ hit, annotations, representations, descriptions, noteDraft, editingNoteID, editingBody, busy, canRestore, onRestore, onDraft, onAdd, onEdit, onEditBody, onSaveEdit, onCancelEdit }: { hit: SearchHit; annotations: any[]; representations: any[]; descriptions: any[]; noteDraft: string; editingNoteID: string; editingBody: string; busy: boolean; canRestore: boolean; onRestore: () => void; onDraft: (value: string) => void; onAdd: () => void; onEdit: (note: any) => void; onEditBody: (value: string) => void; onSaveEdit: (note: any) => void; onCancelEdit: () => void }) {
  const notes = annotations.filter((item) => item.kind === "NOTE" && !item.tombstoned);
  const exact = representations.find((item) => item.class === "EXACT");
  return <div className="details">
    <div className="detail-title"><span className="file-icon large"><FileSearch size={21} /></span><div><h2>{hit.name || "Untitled subject"}</h2><p>{hit.entry_type || "File"}</p></div></div>
    <dl className="facts"><div><dt>Original path</dt><dd>{hit.path || "Not recorded"}</dd></div><div><dt>SHA-256 identity</dt><dd className="mono">{hit.content_id || "Not available in this result"}</dd></div><div><dt>Protection</dt><dd><span className="inline-state"><i />{exact?.verified === true ? "Verified exact bytes" : exact ? "Exact representation" : "Protected subject"}</span></dd></div>{exact && <div><dt>Stored size</dt><dd>{formatBytes(exact.decoded_length)}</dd></div>}</dl>
    <div className="detail-section notes-section"><div className="section-heading"><h3>Notes</h3><span>{notes.length}</span></div>
      <div className="notes-list">{notes.length ? notes.map((note) => editingNoteID === note.annotation_id ? <div className="note-row editing" key={note.annotation_id}><textarea autoFocus aria-label="Edit note" value={editingBody} onChange={(event) => onEditBody(event.target.value)} /><div className="note-actions"><button className="text-button" onClick={onCancelEdit} disabled={busy}>Cancel</button><button className="primary-button compact" onClick={() => onSaveEdit(note)} disabled={busy || !editingBody.trim()}><Check size={14} />Save</button></div></div> : <div className="note-row" key={note.annotation_id}><p>{note.body}</p><div className="note-meta"><span>{formatNoteDate(note.updated_at)} · revision {note.revision}</span><button className="note-edit" title="Edit note" aria-label="Edit note" onClick={() => onEdit(note)}><Pencil size={13} /></button></div></div>) : <p className="muted">No notes yet.</p>}</div>
      <div className="note-composer"><textarea aria-label="New note" value={noteDraft} onChange={(event) => onDraft(event.target.value)} placeholder="Add useful context about this file" /><button className="primary-button compact" onClick={onAdd} disabled={busy || !noteDraft.trim()}><Plus size={14} />Add note</button></div>
    </div>
    {descriptions.length > 0 && <div className="detail-section"><h3>Description</h3><p className="description">{descriptions[0].title || descriptions[0].kind || "Description available"}</p></div>}
    <div className="detail-footer"><span><ShieldCheck size={14} />Recovery remains independent of search index</span><button className="secondary-button compact" onClick={onRestore} disabled={busy || !canRestore}><ArchiveRestore size={14} />Restore snapshot</button></div>
  </div>;
}
function PlanCard({ plan, busy, onApply }: { plan: any; busy: boolean; onApply: () => void }) { const blocked = plan.blocked_entries?.length ?? 0; return <div className="plan-card"><div className="plan-title"><div><p className="eyebrow">REVIEW PLAN</p><h3>{plan.state || (plan.executable ? "READY" : "BLOCKED")}</h3></div><ShieldCheck size={21} /></div><div className="plan-stats"><span><b>{plan.files ?? 0}</b> files</span><span><b>{formatBytes(plan.bytes ?? 0)}</b> logical</span><span><b>{formatBytes(plan.new_bytes ?? 0)}</b> new storage</span></div><div className="plan-outcomes"><span>{plan.protection_mode || "STORE_EXACT"}</span>{blocked > 0 && <span className="warn">{blocked} blocked</span>}</div>{plan.executable && plan.state !== "SUCCEEDED" ? <button className="primary-button" onClick={onApply} disabled={busy}>{busy ? "Protecting..." : "Confirm protection"}</button> : <p className="muted">{blocked > 0 ? "Resolve blocked entries before confirming." : "This plan has already been applied."}</p>}</div>; }
function RestoreCard({ plan, busy, onApply }: { plan: any; busy: boolean; onApply: () => void }) { return <div className="plan-card"><div className="plan-title"><div><p className="eyebrow">REVIEW RESTORE</p><h3>{plan.state || "READY"}</h3></div><ArchiveRestore size={21} /></div><div className="plan-stats"><span><b>{plan.files ?? 0}</b> files</span><span><b>{formatBytes(plan.bytes ?? 0)}</b> verified output</span></div><div className="plan-outcomes"><span>EXACT BYTES</span></div>{plan.executable && plan.state !== "SUCCEEDED" ? <button className="primary-button" onClick={onApply} disabled={busy}>{busy ? "Restoring..." : "Confirm restore"}</button> : <p className="muted">Restore is not executable.</p>}</div>; }
function SettingGroup({ title, children }: { title: string; children: ReactNode }) { return <section className="setting-group"><h3>{title}</h3>{children}</section>; }
function SettingRow({ label, value, state }: { label: string; value?: string; state?: boolean }) { return <div className="setting-row"><span>{label}</span><strong>{value || "Not reported"}</strong>{state !== undefined && <i className={state ? "good" : "bad"} />}</div>; }
function formatBytes(value: number) { if (!value) return "0 B"; if (value < 1024) return `${value} B`; if (value < 1024 * 1024) return `${(value / 1024).toFixed(1)} KB`; return `${(value / 1024 / 1024).toFixed(1)} MB`; }
function formatNoteDate(value?: string) { if (!value) return "Saved"; const date = new Date(value); return Number.isNaN(date.getTime()) ? "Saved" : new Intl.DateTimeFormat(undefined, { month: "short", day: "numeric", hour: "2-digit", minute: "2-digit" }).format(date); }

createRoot(document.getElementById("root")!).render(<App />);
export default App;
