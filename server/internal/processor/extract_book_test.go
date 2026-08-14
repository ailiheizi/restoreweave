package processor

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ailiheizi/restoreweave/server/internal/exact"
	"github.com/ailiheizi/restoreweave/server/internal/repository"
	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
)

func TestBookMetaExtractsEPUB(t *testing.T) {
	payload := buildEPUB("Harbor Night", "Example Author", "2011-03-01", "en")
	work, ok := parseEPUB(payload)
	if !ok || work.Title != "Harbor Night" || work.Author != "Example Author" || work.Year != "2011" || work.Language != "en" {
		t.Fatalf("epub meta = %+v ok=%v", work, ok)
	}
}

func TestBookExtractAdmittedAndDoesNotBlockExact(t *testing.T) {
	ctx := context.Background()
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "novel.epub"), buildEPUB("Harbor Night", "Example Author", "2011", "en"), 0o644); err != nil {
		t.Fatalf("write epub: %v", err)
	}
	if err := os.WriteFile(filepath.Join(source, "notes.md"), []byte("# Driftwood\n\nA shoreline story."), 0o644); err != nil {
		t.Fatalf("write md: %v", err)
	}
	if err := os.WriteFile(filepath.Join(source, "unknown.bin"), []byte{0x00, 0xff}, 0o644); err != nil {
		t.Fatalf("write bin: %v", err)
	}

	store, err := sqlite.Open(ctx, filepath.Join(t.TempDir(), "catalog.sqlite"), sqlite.Options{})
	if err != nil {
		t.Fatalf("open catalog: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	repo, err := repository.OpenDir(filepath.Join(t.TempDir(), "repository"))
	if err != nil {
		t.Fatalf("open repo: %v", err)
	}
	host := NewHost(store, repo, Options{StagingDir: t.TempDir()})
	service := &exact.Service{Store: store, Repo: repo, Processor: host}
	ingested, err := service.Ingest(ctx, source)
	if err != nil {
		t.Fatalf("ingest: %v", err)
	}
	if _, err := service.Verify(ctx, ingested.SnapshotRef); err != nil {
		t.Fatalf("verify: %v", err)
	}
	artifacts, err := store.ListAdmittedArtifacts(ctx, ingested.WorkspaceID, ingested.SnapshotRef)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	var book, text int
	for _, artifact := range artifacts {
		switch artifact.CapabilityID {
		case CapabilityBookMeta:
			book++
			var work bookWork
			if err := json.Unmarshal([]byte(artifact.Body), &work); err != nil {
				t.Fatalf("decode book: %v", err)
			}
			if work.Title != "Harbor Night" || work.Author != "Example Author" {
				t.Fatalf("admitted book = %+v", work)
			}
		case CapabilityTextExtract:
			text++
			if artifact.Body != "# Driftwood\n\nA shoreline story." {
				t.Fatalf("admitted text = %q", artifact.Body)
			}
		default:
			t.Fatalf("unexpected capability %s", artifact.CapabilityID)
		}
	}
	if book != 1 || text != 1 {
		t.Fatalf("artifacts book=%d text=%d total=%d", book, text, len(artifacts))
	}
}

func buildEPUB(title, author, date, language string) []byte {
	var buf bytes.Buffer
	writer := zip.NewWriter(&buf)
	mimetype, err := writer.CreateHeader(&zip.FileHeader{
		Name:   "mimetype",
		Method: zip.Store,
	})
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
