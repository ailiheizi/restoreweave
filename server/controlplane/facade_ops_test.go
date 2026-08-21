package controlplane

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ailiheizi/restoreweave/client/command"
	"github.com/ailiheizi/restoreweave/server/internal/exact"
	"github.com/ailiheizi/restoreweave/server/internal/gateway/protocol"
	"github.com/ailiheizi/restoreweave/server/internal/repository"
	"github.com/ailiheizi/restoreweave/server/testutil"
)

func TestProtocolFacadeUsesCommandABI(t *testing.T) {
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
	mp3 := testID3v23(map[string]string{
		"TIT2": "Nightfall",
		"TPE1": "Example Artist",
		"TALB": "Demo Album",
		"TRCK": "2/10",
	})
	if err := os.WriteFile(filepath.Join(root, "song.mp3"), mp3, 0o644); err != nil {
		t.Fatalf("write mp3: %v", err)
	}
	if err := os.WriteFile(filepath.Join(root, "novel.epub"), testEPUB("Lighthouse Keeper", "Example Author", "2011", "en"), 0o644); err != nil {
		t.Fatalf("write epub: %v", err)
	}

	ingestData := mustAppliedIngest(t, ctx, dispatcher, map[string]any{"root": root})

	facade, err := protocol.New(dispatcher.Handle, protocol.Options{
		WorkspaceID: ingestData.WorkspaceID,
		SnapshotRef: ingestData.SnapshotRef,
		Token:       "facade-token",
		Listen:      "127.0.0.1:0",
	})
	if err != nil {
		t.Fatalf("new facade: %v", err)
	}
	server := httptest.NewServer(facade.Handler())
	defer server.Close()

	unauthorized := getJSON(t, server.URL+"/rest/ping.view?f=json&p=wrong")
	if statusOf(unauthorized) != "failed" {
		t.Fatalf("unauthorized ping = %#v", unauthorized)
	}

	ping := getJSON(t, server.URL+"/rest/ping.view?f=json&p=facade-token")
	if statusOf(ping) != "ok" {
		t.Fatalf("ping = %#v", ping)
	}

	artists := getJSON(t, server.URL+"/rest/getArtists.view?f=json&p=facade-token")
	index := mustSlice(t, nested(artists, "artists", "index"))
	if len(index) == 0 {
		t.Fatalf("artists = %#v", artists)
	}

	albumList := getJSON(t, server.URL+"/rest/getAlbumList2.view?f=json&p=facade-token")
	albums := mustSlice(t, nested(albumList, "albumList2", "album"))
	if len(albums) != 1 {
		t.Fatalf("albums = %#v", albumList)
	}
	albumID, _ := albums[0].(map[string]any)["id"].(string)
	album := getJSON(t, server.URL+"/rest/getAlbum.view?f=json&p=facade-token&id="+albumID)
	songs := mustSlice(t, nested(album, "album", "song"))
	if len(songs) != 1 {
		t.Fatalf("songs = %#v", album)
	}
	songID, _ := songs[0].(map[string]any)["id"].(string)
	if songID == "" {
		t.Fatal("missing song id")
	}

	stream, err := http.Get(server.URL + "/rest/stream.view?f=json&p=facade-token&id=" + songID)
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	body, err := io.ReadAll(stream.Body)
	stream.Body.Close()
	if err != nil {
		t.Fatalf("read stream: %v", err)
	}
	if stream.StatusCode != http.StatusOK || len(body) != len(mp3) {
		t.Fatalf("stream status=%d len=%d want %d", stream.StatusCode, len(body), len(mp3))
	}

	scrobble := getJSON(t, server.URL+"/rest/scrobble.view?f=json&p=facade-token&id="+songID+"&submission=true")
	if statusOf(scrobble) != "ok" {
		t.Fatalf("scrobble = %#v", scrobble)
	}
	listed := dispatcher.Handle(ctx, mustEnvelope(t, command.OpAnnotationList, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"subject_ref":  songID,
	}))
	if listed.Status != command.StatusSucceeded {
		t.Fatalf("annotation.list = %q: %+v", listed.Status, listed.Reasons)
	}
	var annotations command.AnnotationListData
	if err := json.Unmarshal(listed.Data, &annotations); err != nil {
		t.Fatalf("decode annotations: %v", err)
	}
	foundProgress := false
	for _, annotation := range annotations.Annotations {
		if annotation.Kind == "PROGRESS" && strings.Contains(annotation.Body, `"source":"opensubsonic"`) {
			foundProgress = true
		}
	}
	if !foundProgress {
		t.Fatalf("progress annotation missing: %+v", annotations.Annotations)
	}

	works, err := http.Get(server.URL + "/opds/works?token=facade-token")
	if err != nil {
		t.Fatalf("opds works: %v", err)
	}
	feed, err := io.ReadAll(works.Body)
	works.Body.Close()
	if err != nil {
		t.Fatalf("read opds: %v", err)
	}
	if works.StatusCode != http.StatusOK || !strings.Contains(string(feed), "Lighthouse Keeper") {
		t.Fatalf("opds status=%d body=%s", works.StatusCode, feed)
	}
	if !strings.Contains(string(feed), "/opds/acquire/") {
		t.Fatalf("opds missing acquisition link: %s", feed)
	}

	search := getJSON(t, server.URL+"/rest/search3.view?f=json&p=facade-token&query=Nightfall")
	songs = mustSlice(t, nested(search, "searchResult3", "song"))
	if len(songs) != 1 {
		t.Fatalf("search songs = %#v", search)
	}

	star := getJSON(t, server.URL+"/rest/star.view?f=json&p=facade-token&id="+songID)
	if statusOf(star) != "ok" {
		t.Fatalf("star = %#v", star)
	}
	starred := getJSON(t, server.URL+"/rest/getStarred2.view?f=json&p=facade-token")
	starredSongs := mustSlice(t, nested(starred, "starred2", "song"))
	if len(starredSongs) != 1 {
		t.Fatalf("starred = %#v", starred)
	}

	cover, err := http.Get(server.URL + "/rest/getCoverArt.view?p=facade-token&id=" + songID)
	if err != nil {
		t.Fatalf("cover: %v", err)
	}
	coverBody, _ := io.ReadAll(cover.Body)
	cover.Body.Close()
	if cover.StatusCode != http.StatusOK || cover.Header.Get("Content-Type") != "image/png" || len(coverBody) < 32 {
		t.Fatalf("cover status=%d type=%s len=%d", cover.StatusCode, cover.Header.Get("Content-Type"), len(coverBody))
	}

	xmlArtists, err := http.Get(server.URL + "/rest/getArtists.view?f=xml&p=facade-token")
	if err != nil {
		t.Fatalf("xml artists: %v", err)
	}
	xmlBody, _ := io.ReadAll(xmlArtists.Body)
	xmlArtists.Body.Close()
	if xmlArtists.StatusCode != http.StatusOK || !strings.Contains(string(xmlBody), "Example Artist") {
		t.Fatalf("xml artists = %s", xmlBody)
	}

	bookmark := getJSON(t, server.URL+"/rest/createBookmark.view?f=json&p=facade-token&id="+songID+"&position=12000")
	if statusOf(bookmark) != "ok" {
		t.Fatalf("bookmark = %#v", bookmark)
	}
	bookmarks := getJSON(t, server.URL+"/rest/getBookmarks.view?f=json&p=facade-token")
	if len(mustSlice(t, nested(bookmarks, "bookmarks", "bookmark"))) != 1 {
		t.Fatalf("bookmarks = %#v", bookmarks)
	}

	opdsSearch, err := http.Get(server.URL + "/opds/search?token=facade-token&q=Lighthouse")
	if err != nil {
		t.Fatalf("opds search: %v", err)
	}
	opdsBody, _ := io.ReadAll(opdsSearch.Body)
	opdsSearch.Body.Close()
	if opdsSearch.StatusCode != http.StatusOK || !strings.Contains(string(opdsBody), "Lighthouse Keeper") {
		t.Fatalf("opds search = %s", opdsBody)
	}
}

func TestExperienceSurfacesOverCommandABI(t *testing.T) {
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
	mp3 := testID3v23(map[string]string{
		"TIT2": "Nightfall",
		"TPE1": "Example Artist",
		"TALB": "Demo Album",
	})
	epub := testEPUB("Lighthouse Keeper", "Example Author", "2011", "en")
	notes := []byte("# Driftwood\n\nA shoreline story.")
	plain := []byte("Quiet Harbor\n\nChapter one.")
	for name, payload := range map[string][]byte{
		"song.mp3":   mp3,
		"novel.epub": epub,
		"notes.md":   notes,
		"plain.txt":  plain,
	} {
		if err := os.WriteFile(filepath.Join(root, name), payload, 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}

	ingestData := mustAppliedIngest(t, ctx, dispatcher, map[string]any{"root": root})

	facade, err := protocol.New(dispatcher.Handle, protocol.Options{
		WorkspaceID: ingestData.WorkspaceID,
		SnapshotRef: ingestData.SnapshotRef,
		Token:       "facade-token",
		Listen:      "127.0.0.1:0",
	})
	if err != nil {
		t.Fatalf("new facade: %v", err)
	}
	server := httptest.NewServer(facade.Handler())
	defer server.Close()
	base := server.URL
	token := "facade-token"

	user := getJSON(t, base+"/rest/getUser.view?f=json&p="+token)
	if statusOf(user) != "ok" {
		t.Fatalf("getUser = %#v", user)
	}
	scan := getJSON(t, base+"/rest/getScanStatus.view?f=json&p="+token)
	if statusOf(scan) != "ok" {
		t.Fatalf("getScanStatus = %#v", scan)
	}
	playlists := getJSON(t, base+"/rest/getPlaylists.view?f=json&p="+token)
	if statusOf(playlists) != "ok" {
		t.Fatalf("getPlaylists = %#v", playlists)
	}
	created := getJSON(t, base+"/rest/createPlaylist.view?f=json&p="+token+"&name=private")
	if statusOf(created) != "failed" {
		t.Fatalf("createPlaylist should be refused: %#v", created)
	}
	random := getJSON(t, base+"/rest/getRandomSongs.view?f=json&p="+token+"&size=5")
	if len(mustSlice(t, nested(random, "randomSongs", "song"))) != 1 {
		t.Fatalf("randomSongs = %#v", random)
	}
	legacyAlbums := getJSON(t, base+"/rest/getAlbumList.view?f=json&p="+token)
	legacy := mustSlice(t, nested(legacyAlbums, "albumList", "album"))
	if len(legacy) != 1 {
		t.Fatalf("getAlbumList = %#v", legacyAlbums)
	}
	albumID, _ := legacy[0].(map[string]any)["id"].(string)
	similar := getJSON(t, base+"/rest/getSimilarSongs.view?f=json&p="+token+"&id="+albumID)
	if statusOf(similar) != "ok" {
		t.Fatalf("getSimilarSongs = %#v", similar)
	}

	page1, err := http.Get(base + "/opds/works?token=" + token + "&start=0&count=1")
	if err != nil {
		t.Fatalf("opds page1: %v", err)
	}
	feed1, _ := io.ReadAll(page1.Body)
	page1.Body.Close()
	if page1.StatusCode != http.StatusOK || !strings.Contains(string(feed1), `rel="next"`) {
		t.Fatalf("opds page1 missing next: %s", feed1)
	}
	page2, err := http.Get(base + "/opds/works?token=" + token + "&start=1&count=1")
	if err != nil {
		t.Fatalf("opds page2: %v", err)
	}
	feed2, _ := io.ReadAll(page2.Body)
	page2.Body.Close()
	if page2.StatusCode != http.StatusOK || !strings.Contains(string(feed2), "<entry>") {
		t.Fatalf("opds page2 = %s", feed2)
	}
	if string(feed1) == string(feed2) {
		t.Fatal("opds pagination returned the same page")
	}

	inboxPage, err := http.Get(base + "/inbox?token=" + token)
	if err != nil {
		t.Fatalf("inbox page: %v", err)
	}
	html, _ := io.ReadAll(inboxPage.Body)
	inboxPage.Body.Close()
	if inboxPage.StatusCode != http.StatusOK || !strings.Contains(string(html), "RestoreWeave Inbox") {
		t.Fatalf("inbox page status=%d body=%s", inboxPage.StatusCode, html)
	}

	inboxStatus := getPlainJSON(t, base+"/inbox/api/status?token="+token)
	if inboxStatus["workspace_id"] != ingestData.WorkspaceID || inboxStatus["catalog_ok"] != true {
		t.Fatalf("inbox status = %#v", inboxStatus)
	}
	if snaps, _ := inboxStatus["snapshots"].(float64); snaps < 1 {
		t.Fatalf("inbox status missing snapshots: %#v", inboxStatus)
	}
	if jobs, _ := inboxStatus["jobs"].(float64); jobs < 1 {
		t.Fatalf("inbox status missing jobs: %#v", inboxStatus)
	}
	inboxPlan := getPlainJSON(t, base+"/inbox/api/plan?token="+token+"&id="+ingestData.PlanID)
	if inboxPlan["ok"] != true || inboxPlan["kind"] != "INGEST" || inboxPlan["applied"] != true || inboxPlan["executable"] != false {
		t.Fatalf("inbox plan = %#v", inboxPlan)
	}
	inboxJob := getPlainJSON(t, base+"/inbox/api/job?token="+token+"&id="+ingestData.JobID)
	if inboxJob["ok"] != true || inboxJob["job_state"] != "SUCCEEDED" || inboxJob["terminal"] != true {
		t.Fatalf("inbox job = %#v", inboxJob)
	}
	inboxDoctor := getPlainJSON(t, base+"/inbox/api/doctor?token="+token)
	if inboxDoctor["ok"] != true {
		t.Fatalf("inbox doctor = %#v", inboxDoctor)
	}
	inboxSnapshots := getPlainJSON(t, base+"/inbox/api/snapshots?token="+token)
	snaps, _ := inboxSnapshots["snapshots"].([]any)
	if inboxSnapshots["ok"] != true || len(snaps) < 1 {
		t.Fatalf("inbox snapshots = %#v", inboxSnapshots)
	}

	search := getPlainJSON(t, base+"/inbox/api/search?token="+token+"&q=Nightfall")
	hits, _ := search["hits"].([]any)
	songID := ""
	for _, hit := range hits {
		row, _ := hit.(map[string]any)
		if row["kind"] == "audio" {
			songID, _ = row["subject_ref"].(string)
			break
		}
	}
	if songID == "" {
		t.Fatalf("inbox search missing audio subject: %#v", search)
	}

	item := getPlainJSON(t, base+"/inbox/api/item?token="+token+"&id="+songID)
	if item["kind"] != "audio" {
		t.Fatalf("inbox item = %#v", item)
	}
	if item["path"] == "" || item["snapshot_ref"] != ingestData.SnapshotRef {
		t.Fatalf("inbox item missing provenance: %#v", item)
	}
	reps, _ := item["representations"].([]any)
	if len(reps) == 0 {
		t.Fatalf("inbox item missing representations: %#v", item)
	}

	preview, err := http.Get(base + "/inbox/api/preview?token=" + token + "&id=" + songID)
	if err != nil {
		t.Fatalf("inbox preview: %v", err)
	}
	previewBody, _ := io.ReadAll(preview.Body)
	preview.Body.Close()
	if preview.StatusCode != http.StatusOK || !bytes.Equal(previewBody, mp3) {
		t.Fatalf("inbox preview status=%d len=%d want %d", preview.StatusCode, len(previewBody), len(mp3))
	}

	progressStatus, progressBody := postJSON(t, base+"/inbox/api/progress?token="+token, map[string]any{
		"subject_ref": songID,
		"position_ms": 1500,
		"completed":   false,
	})
	if progressStatus != http.StatusOK || !strings.Contains(string(progressBody), `"ok":true`) {
		t.Fatalf("inbox progress status=%d body=%s", progressStatus, progressBody)
	}
	opdsProgressStatus, _ := postJSON(t, base+"/opds/progress?token="+token, map[string]any{
		"subject_ref": songID,
		"position_ms": 2400,
		"completed":   false,
	})
	if opdsProgressStatus != http.StatusOK {
		t.Fatalf("opds progress status=%d", opdsProgressStatus)
	}
	listed := dispatcher.Handle(ctx, mustEnvelope(t, command.OpAnnotationList, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"subject_ref":  songID,
	}))
	if listed.Status != command.StatusSucceeded {
		t.Fatalf("annotation.list = %q: %+v", listed.Status, listed.Reasons)
	}
	var annotations command.AnnotationListData
	if err := json.Unmarshal(listed.Data, &annotations); err != nil {
		t.Fatalf("decode annotations: %v", err)
	}
	foundProgress := false
	for _, annotation := range annotations.Annotations {
		if annotation.Kind == "PROGRESS" && strings.Contains(annotation.Body, `"source":"opds"`) {
			foundProgress = true
		}
	}
	if !foundProgress {
		t.Fatalf("opds/inbox progress missing: %+v", annotations.Annotations)
	}
	inboxExport := getPlainJSON(t, base+"/inbox/api/annotations?token="+token+"&id="+songID)
	if inboxExport["ok"] != true || inboxExport["schema"] != command.AnnotationBundleSchema {
		t.Fatalf("inbox annotations = %#v", inboxExport)
	}
	exported, _ := inboxExport["annotations"].([]any)
	if len(exported) < 1 {
		t.Fatalf("inbox annotations empty: %#v", inboxExport)
	}
	importStatus, importBody := postJSON(t, base+"/inbox/api/annotations?token="+token, inboxExport)
	if importStatus != http.StatusOK || !strings.Contains(string(importBody), `"ok":true`) {
		t.Fatalf("inbox annotation import status=%d body=%s", importStatus, importBody)
	}
	resolved := getPlainJSON(t, base+"/inbox/api/resolve?token="+token+"&path=song.mp3")
	if resolved["ok"] != true || resolved["subject_ref"] != songID {
		t.Fatalf("inbox resolve = %#v want %s", resolved, songID)
	}
	pathSearch := getPlainJSON(t, base+"/inbox/api/search?token="+token+"&q=song.mp3")
	foundPath := false
	pathHits, _ := pathSearch["hits"].([]any)
	for _, hit := range pathHits {
		row, _ := hit.(map[string]any)
		if row["subject_ref"] == songID {
			foundPath = true
		}
	}
	if !foundPath {
		t.Fatalf("inbox search path miss: %#v", pathSearch)
	}

	verifyStatus, verifyBody := postJSON(t, base+"/inbox/api/verify?token="+token, map[string]any{})
	if verifyStatus != http.StatusOK || !strings.Contains(string(verifyBody), `"ok":true`) {
		t.Fatalf("inbox verify status=%d body=%s", verifyStatus, verifyBody)
	}

	artifact := filepath.Join(t.TempDir(), "recovery.json")
	recoveryStatus, recoveryBody := postJSON(t, base+"/inbox/api/recovery?token="+token, map[string]any{
		"destination": artifact,
	})
	if recoveryStatus != http.StatusOK || !strings.Contains(string(recoveryBody), `"independently_stored":true`) {
		t.Fatalf("inbox recovery status=%d body=%s", recoveryStatus, recoveryBody)
	}
	if _, err := os.Stat(artifact); err != nil {
		t.Fatalf("inbox recovery artifact: %v", err)
	}
	againStatus, againBody := postJSON(t, base+"/inbox/api/recovery?token="+token, map[string]any{
		"destination": artifact,
	})
	if againStatus != http.StatusConflict {
		t.Fatalf("inbox recovery overwrite status=%d body=%s", againStatus, againBody)
	}

	preflightStatus, preflightBody := postJSON(t, base+"/inbox/api/restore?token="+token, map[string]any{})
	if preflightStatus != http.StatusOK || !strings.Contains(string(preflightBody), `"wrote":false`) {
		t.Fatalf("inbox restore preflight status=%d body=%s", preflightStatus, preflightBody)
	}

	dest := filepath.Join(t.TempDir(), "restored")
	restoreStatus, restoreBody := postJSON(t, base+"/inbox/api/restore?token="+token, map[string]any{
		"destination": dest,
	})
	if restoreStatus != http.StatusOK || !strings.Contains(string(restoreBody), `"ok":true`) {
		t.Fatalf("inbox restore status=%d body=%s", restoreStatus, restoreBody)
	}
	for name, want := range map[string][]byte{"song.mp3": mp3, "novel.epub": epub, "notes.md": notes, "plain.txt": plain} {
		got, err := os.ReadFile(filepath.Join(dest, name))
		if err != nil {
			t.Fatalf("read restored %s: %v", name, err)
		}
		if sha256Hex(got) != sha256Hex(want) {
			t.Fatalf("restored %s sha mismatch", name)
		}
	}

	secondRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(secondRoot, "extra.txt"), []byte("second"), 0o600); err != nil {
		t.Fatalf("write extra: %v", err)
	}
	secondData := mustAppliedIngest(t, ctx, dispatcher, map[string]any{"root": secondRoot})
	inboxDiff := getPlainJSON(t, base+"/inbox/api/diff?token="+token+"&from="+ingestData.SnapshotRef+"&to="+secondData.SnapshotRef)
	if inboxDiff["ok"] != true {
		t.Fatalf("inbox diff = %#v", inboxDiff)
	}
	foundAdded := false
	changes, _ := inboxDiff["changes"].([]any)
	for _, raw := range changes {
		change, _ := raw.(map[string]any)
		if change["kind"] == command.DiffAdded && change["path"] == "extra.txt" {
			foundAdded = true
		}
	}
	if !foundAdded {
		t.Fatalf("inbox diff missing extra.txt: %#v", inboxDiff)
	}
}

// TestD5PinnedSupersonicCallSequence records the OpenSubsonic methods this
// host's installed Supersonic 0.22.1 issues (XML, c=supersonic). It is not a
// GUI driver and does not claim a live login was clicked.
func TestD5PinnedSupersonicCallSequence(t *testing.T) {
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
	mp3 := testID3v23(map[string]string{
		"TIT2": "Nightfall",
		"TPE1": "Example Artist",
		"TALB": "Demo Album",
	})
	if err := os.WriteFile(filepath.Join(root, "song.mp3"), mp3, 0o644); err != nil {
		t.Fatalf("write song: %v", err)
	}
	ingestData := mustAppliedIngest(t, ctx, dispatcher, map[string]any{"root": root})
	facade, err := protocol.New(dispatcher.Handle, protocol.Options{
		WorkspaceID: ingestData.WorkspaceID,
		SnapshotRef: ingestData.SnapshotRef,
		Token:       "facade-token",
		Listen:      "127.0.0.1:0",
	})
	if err != nil {
		t.Fatalf("new facade: %v", err)
	}
	server := httptest.NewServer(facade.Handler())
	defer server.Close()
	q := func(method, extra string) string {
		return server.URL + "/rest/" + method + ".view?f=xml&c=supersonic&v=1.15.0&u=local&p=facade-token" + extra
	}
	for _, method := range []string{"ping", "getLicense", "getUser", "getMusicFolders", "getPlayQueue", "getPlaylists"} {
		body := getRaw(t, q(method, ""))
		if !strings.Contains(body, `status="ok"`) {
			t.Fatalf("%s xml = %s", method, body)
		}
	}
	artists := getRaw(t, q("getArtists", ""))
	if !strings.Contains(artists, `status="ok"`) || !strings.Contains(artists, `name="Example Artist"`) {
		t.Fatalf("getArtists xml = %s", artists)
	}
	albums := getRaw(t, q("getAlbumList2", "&type=alphabeticalByName&offset=0&size=20"))
	if !strings.Contains(albums, `status="ok"`) || !strings.Contains(albums, `name="Demo Album"`) {
		t.Fatalf("getAlbumList2 xml = %s", albums)
	}
	search := getJSON(t, server.URL+"/rest/search3.view?f=json&c=supersonic&v=1.15.0&p=facade-token&query=Nightfall")
	if statusOf(search) != "ok" {
		t.Fatalf("search3 = %#v", search)
	}
	songs := mustSlice(t, nested(search, "searchResult3", "song"))
	if len(songs) == 0 {
		t.Fatalf("search3 songs = %#v", search)
	}
	songID, _ := songs[0].(map[string]any)["id"].(string)
	if songID == "" {
		t.Fatalf("search3 song id missing: %#v", songs[0])
	}
	song := getRaw(t, q("getSong", "&id="+songID))
	if !strings.Contains(song, `status="ok"`) || !strings.Contains(song, `title="Nightfall"`) {
		t.Fatalf("getSong xml = %s", song)
	}
	cover, err := http.Get(q("getCoverArt", "&id="+songID))
	if err != nil {
		t.Fatalf("cover: %v", err)
	}
	coverBody, _ := io.ReadAll(cover.Body)
	cover.Body.Close()
	if cover.StatusCode != http.StatusOK || cover.Header.Get("Content-Type") != "image/png" || len(coverBody) < 32 {
		t.Fatalf("cover status=%d type=%s len=%d", cover.StatusCode, cover.Header.Get("Content-Type"), len(coverBody))
	}
	stream, err := http.Get(q("stream", "&id="+songID))
	if err != nil {
		t.Fatalf("stream: %v", err)
	}
	got, _ := io.ReadAll(stream.Body)
	stream.Body.Close()
	if stream.StatusCode != http.StatusOK || !bytes.Equal(got, mp3) {
		t.Fatalf("stream status=%d len=%d", stream.StatusCode, len(got))
	}
	star := getJSON(t, server.URL+"/rest/star.view?f=json&c=supersonic&p=facade-token&id="+songID)
	if statusOf(star) != "ok" {
		t.Fatalf("star = %#v", star)
	}
	starred := getRaw(t, q("getStarred2", ""))
	if !strings.Contains(starred, `status="ok"`) || !strings.Contains(starred, `title="Nightfall"`) {
		t.Fatalf("getStarred2 xml = %s", starred)
	}
	artistStar := getJSON(t, server.URL+"/rest/star.view?f=json&c=supersonic&p=facade-token&artistId="+artistIDFromXML(t, artists))
	if statusOf(artistStar) != "ok" {
		t.Fatalf("star artist = %#v", artistStar)
	}
	artistsAfter := getRaw(t, q("getArtists", ""))
	if !strings.Contains(artistsAfter, `starred="`) || !strings.Contains(artistsAfter, `name="Example Artist"`) {
		t.Fatalf("getArtists after artist star = %s", artistsAfter)
	}
	artistPage := getRaw(t, q("getArtist", "&id="+artistIDFromXML(t, artistsAfter)))
	if !strings.Contains(artistPage, `starred="`) || !strings.Contains(artistPage, `name="Demo Album"`) {
		t.Fatalf("getArtist after star = %s", artistPage)
	}
	starredAfter := getRaw(t, q("getStarred2", ""))
	if !strings.Contains(starredAfter, `name="Example Artist"`) {
		t.Fatalf("getStarred2 missing artist = %s", starredAfter)
	}
	listed := dispatcher.Handle(ctx, mustEnvelope(t, command.OpAnnotationList, map[string]any{
		"workspace_id": ingestData.WorkspaceID,
		"subject_ref":  songID,
	}))
	if listed.Status != command.StatusSucceeded {
		t.Fatalf("annotation.list = %q: %+v", listed.Status, listed.Reasons)
	}
	var annotations command.AnnotationListData
	if err := json.Unmarshal(listed.Data, &annotations); err != nil {
		t.Fatalf("decode annotations: %v", err)
	}
	foundStar := false
	for _, annotation := range annotations.Annotations {
		if annotation.Kind == "TAG" && annotation.Body == "starred" {
			foundStar = true
		}
	}
	if !foundStar {
		t.Fatalf("star missing from annotation.list: %+v", annotations.Annotations)
	}
}

func artistIDFromXML(t *testing.T, body string) string {
	t.Helper()
	const prefix = `id="ar_`
	i := strings.Index(body, prefix)
	if i < 0 {
		t.Fatalf("artist id missing: %s", body)
	}
	rest := body[i+len(`id="`):]
	end := strings.Index(rest, `"`)
	if end < 0 {
		t.Fatalf("artist id unterminated: %s", body)
	}
	return rest[:end]
}

func getRaw(t *testing.T, url string) string {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s: %v", url, err)
	}
	return string(body)
}

func getJSON(t *testing.T, url string) map[string]any {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode %s: %v", url, err)
	}
	return payload
}

func statusOf(payload map[string]any) string {
	inner, _ := payload["subsonic-response"].(map[string]any)
	status, _ := inner["status"].(string)
	return status
}

func nested(payload map[string]any, keys ...string) any {
	var current any = payload["subsonic-response"]
	for _, key := range keys {
		object, _ := current.(map[string]any)
		current = object[key]
	}
	return current
}

func getPlainJSON(t *testing.T, url string) map[string]any {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatalf("GET %s: %v", url, err)
	}
	defer resp.Body.Close()
	var payload map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		t.Fatalf("decode %s: %v", url, err)
	}
	return payload
}

func postJSON(t *testing.T, url string, body any) (int, []byte) {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	resp, err := http.Post(url, "application/json", bytes.NewReader(raw))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	defer resp.Body.Close()
	payload, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("read %s: %v", url, err)
	}
	return resp.StatusCode, payload
}

func sha256Hex(payload []byte) string {
	sum := sha256.Sum256(payload)
	return hex.EncodeToString(sum[:])
}

func mustSlice(t *testing.T, value any) []any {
	t.Helper()
	switch typed := value.(type) {
	case []any:
		return typed
	case nil:
		t.Fatalf("missing list")
	default:
		t.Fatalf("want list, got %T", value)
	}
	return nil
}
