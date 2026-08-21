package controlplane

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ailiheizi/restoreweave/client/command"
	"github.com/ailiheizi/restoreweave/server/internal/exact"
	"github.com/ailiheizi/restoreweave/server/internal/repository"
	"github.com/ailiheizi/restoreweave/server/testutil"
)

func TestBooksListAfterIngest(t *testing.T) {
	ctx := context.Background()
	store := testutil.OpenStore(t, filepath.Join(t.TempDir(), "catalog.sqlite"))
	repo, err := repository.OpenDir(filepath.Join(t.TempDir(), "repository"))
	if err != nil {
		t.Fatalf("open repository: %v", err)
	}
	dispatcher := NewDispatcher(store, "catalog.sqlite", "/tmp/rw.sock", WithExact(&exact.Service{
		Store: store,
		Repo:  repo,
	}))

	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "novel.epub"), testEPUB("Lighthouse Keeper", "Example Author", "2011", "en"), 0o644); err != nil {
		t.Fatalf("write epub: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "notes.md"), []byte("# Driftwood\n\nA shoreline story."), 0o644); err != nil {
		t.Fatalf("write md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "plain.txt"), []byte("Quiet Harbor\n\nChapter one."), 0o644); err != nil {
		t.Fatalf("write txt: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "song.mp3"), testID3v23(map[string]string{
		"TIT2": "Nightfall",
		"TPE1": "Example Artist",
	}), 0o644); err != nil {
		t.Fatalf("write mp3: %v", err)
	}

	ingestData := mustAppliedIngest(t, ctx, dispatcher, map[string]any{"root": root})

	listed := dispatcher.Handle(ctx, mustEnvelope(t, command.OpBooksList, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"snapshot_ref": ingestData.SnapshotRef,
	}))
	if listed.Status != command.StatusSucceeded {
		t.Fatalf("books.list = %q: %+v", listed.Status, listed.Reasons)
	}
	var data command.BookListData
	if err := json.Unmarshal(listed.Data, &data); err != nil {
		t.Fatalf("decode book list: %v", err)
	}
	if len(data.Works) != 3 {
		t.Fatalf("works = %+v, want 3", data.Works)
	}
	byTitle := map[string]command.BookWork{}
	for _, work := range data.Works {
		byTitle[work.Title] = work
	}
	if byTitle["Lighthouse Keeper"].Kind != "epub" || byTitle["Lighthouse Keeper"].Author != "Example Author" {
		t.Fatalf("epub work = %+v", byTitle["Lighthouse Keeper"])
	}
	if byTitle["Driftwood"].Kind != "text" || byTitle["Driftwood"].Name != "notes.md" {
		t.Fatalf("markdown work = %+v", byTitle["Driftwood"])
	}
	if byTitle["Quiet Harbor"].Kind != "text" || byTitle["Quiet Harbor"].Name != "plain.txt" {
		t.Fatalf("text work = %+v", byTitle["Quiet Harbor"])
	}

	found := dispatcher.Handle(ctx, mustEnvelope(t, command.OpSearchQuery, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"query":        "Lighthouse",
	}))
	if found.Status != command.StatusSucceeded {
		t.Fatalf("search Lighthouse = %q: %+v", found.Status, found.Reasons)
	}
	var searchData command.SearchQueryData
	if err := json.Unmarshal(found.Data, &searchData); err != nil {
		t.Fatalf("decode search: %v", err)
	}
	if len(searchData.Hits) != 1 || searchData.Hits[0].SubjectRef != byTitle["Lighthouse Keeper"].SubjectRef {
		t.Fatalf("search hits = %+v, want %s", searchData.Hits, byTitle["Lighthouse Keeper"].SubjectRef)
	}

	audio := dispatcher.Handle(ctx, mustEnvelope(t, command.OpAudioList, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
	}))
	if audio.Status != command.StatusSucceeded {
		t.Fatalf("audio.list = %q: %+v", audio.Status, audio.Reasons)
	}
	var tracks command.AudioListData
	if err := json.Unmarshal(audio.Data, &tracks); err != nil {
		t.Fatalf("decode audio: %v", err)
	}
	if len(tracks.Tracks) != 1 || tracks.Tracks[0].Title != "Nightfall" {
		t.Fatalf("audio tracks = %+v", tracks.Tracks)
	}
}

func TestBooksListRequiresWorkspace(t *testing.T) {
	store := testutil.OpenStore(t, filepath.Join(t.TempDir(), "catalog.sqlite"))
	repo, err := repository.OpenDir(filepath.Join(t.TempDir(), "repository"))
	if err != nil {
		t.Fatalf("open repository: %v", err)
	}
	dispatcher := NewDispatcher(store, "catalog.sqlite", "/tmp/rw.sock", WithExact(&exact.Service{
		Store: store,
		Repo:  repo,
	}))
	result := dispatcher.Handle(context.Background(), mustEnvelope(t, command.OpBooksList, map[string]any{}))
	if result.Status != command.StatusFailed || !hasReasonCode(result, ReasonCodeInvalidInput) {
		t.Fatalf("books.list missing workspace = %q reasons=%+v", result.Status, result.Reasons)
	}
}

func testEPUB(title, author, date, language string) []byte {
	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	mimetype, err := writer.CreateHeader(&zip.FileHeader{Name: "mimetype", Method: zip.Store})
	if err != nil {
		panic(err)
	}
	if _, err := mimetype.Write([]byte("application/epub+zip")); err != nil {
		panic(err)
	}
	container, err := writer.Create("META-INF/container.xml")
	if err != nil {
		panic(err)
	}
	if _, err := container.Write([]byte(`<?xml version="1.0"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
  <rootfiles>
    <rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/>
  </rootfiles>
</container>
`)); err != nil {
		panic(err)
	}
	opf, err := writer.Create("OEBPS/content.opf")
	if err != nil {
		panic(err)
	}
	if _, err := opf.Write([]byte(`<?xml version="1.0"?>
<package xmlns="http://www.idpf.org/2007/opf" unique-identifier="BookId" version="2.0">
  <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
    <dc:title>` + title + `</dc:title>
    <dc:creator>` + author + `</dc:creator>
    <dc:date>` + date + `</dc:date>
    <dc:language>` + language + `</dc:language>
  </metadata>
  <manifest>
    <item id="ncx" href="toc.ncx" media-type="application/x-dtbncx+xml"/>
  </manifest>
  <spine toc="ncx"/>
</package>
`)); err != nil {
		panic(err)
	}
	if err := writer.Close(); err != nil {
		panic(err)
	}
	return buf.Bytes()
}
