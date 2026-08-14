package processor

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ailiheizi/restoreweave/server/internal/exact"
	"github.com/ailiheizi/restoreweave/server/internal/repository"
	"github.com/ailiheizi/restoreweave/server/internal/store/sqlite"
)

func TestAudioTagsExtractID3AndFLAC(t *testing.T) {
	id3 := buildID3v23(map[string]string{
		"TIT2": "Nightfall",
		"TPE1": "Example Artist",
		"TALB": "Demo Album",
		"TRCK": "2/10",
		"TYER": "1999",
	})
	flac := buildFLAC("Harbor", "Other Artist", "Live", 3, 44100, 44100)

	track, ok := parseAudioTags(id3)
	if !ok || track.Title != "Nightfall" || track.Artist != "Example Artist" || track.Track != 2 || track.Year != "1999" {
		t.Fatalf("id3 tags = %+v ok=%v", track, ok)
	}
	track, ok = parseAudioTags(flac)
	if !ok || track.Title != "Harbor" || track.Album != "Live" || track.Track != 3 || track.DurationMS != 1000 {
		t.Fatalf("flac tags = %+v ok=%v", track, ok)
	}
	ogg := buildOGG("Nightfall", "Example Artist", "Demo Album", 2)
	track, ok = parseAudioTags(ogg)
	if !ok || track.Title != "Nightfall" || track.Artist != "Example Artist" || track.Album != "Demo Album" || track.Track != 2 || track.Container != "ogg" {
		t.Fatalf("ogg tags = %+v ok=%v", track, ok)
	}
}

func TestAudioExtractAdmittedAndDoesNotBlockExact(t *testing.T) {
	ctx := context.Background()
	source := t.TempDir()
	id3 := buildID3v23(map[string]string{"TIT2": "Nightfall", "TPE1": "Example Artist"})
	if err := os.WriteFile(filepath.Join(source, "song.mp3"), id3, 0o644); err != nil {
		t.Fatalf("write mp3: %v", err)
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
	if len(artifacts) != 1 || artifacts[0].CapabilityID != CapabilityAudioTags {
		t.Fatalf("artifacts = %+v", artifacts)
	}
	var track audioTrack
	if err := json.Unmarshal([]byte(artifacts[0].Body), &track); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if track.Title != "Nightfall" || track.Artist != "Example Artist" {
		t.Fatalf("admitted track = %+v", track)
	}
}

func buildID3v23(frames map[string]string) []byte {
	var body []byte
	keys := []string{"TIT2", "TPE1", "TALB", "TRCK", "TYER"}
	for _, id := range keys {
		value, ok := frames[id]
		if !ok {
			continue
		}
		data := append([]byte{3}, []byte(value)...)
		frame := []byte(id)
		size := uint32(len(data))
		frame = binary.BigEndian.AppendUint32(frame, size)
		frame = append(frame, 0, 0)
		frame = append(frame, data...)
		body = append(body, frame...)
	}
	header := []byte("ID3\x03\x00\x00")
	header = append(header, putSynchsafe(len(body))...)
	return append(header, body...)
}

func buildFLAC(title, artist, album string, track, rate, samples int) []byte {
	streaminfo := make([]byte, 34)
	streaminfo[10] = byte(rate >> 12)
	streaminfo[11] = byte(rate >> 4)
	streaminfo[12] = byte((rate & 0x0f) << 4)
	streaminfo[13] = byte((samples >> 32) & 0x0f)
	streaminfo[14] = byte(samples >> 24)
	streaminfo[15] = byte(samples >> 16)
	streaminfo[16] = byte(samples >> 8)
	streaminfo[17] = byte(samples)

	comments := []string{
		"TITLE=" + title,
		"ARTIST=" + artist,
		"ALBUM=" + album,
		"TRACKNUMBER=" + itoa(track),
	}
	vendor := []byte("restoreweave-test")
	var vorbis bytes.Buffer
	_ = binary.Write(&vorbis, binary.LittleEndian, uint32(len(vendor)))
	vorbis.Write(vendor)
	_ = binary.Write(&vorbis, binary.LittleEndian, uint32(len(comments)))
	for _, comment := range comments {
		_ = binary.Write(&vorbis, binary.LittleEndian, uint32(len(comment)))
		vorbis.WriteString(comment)
	}

	out := []byte("fLaC")
	out = append(out, flacBlock(0, false, streaminfo)...)
	out = append(out, flacBlock(4, true, vorbis.Bytes())...)
	return out
}

func flacBlock(kind byte, last bool, payload []byte) []byte {
	header := byte(kind)
	if last {
		header |= 0x80
	}
	n := len(payload)
	return append([]byte{header, byte(n >> 16), byte(n >> 8), byte(n)}, payload...)
}

func TestOGGExtractAdmitted(t *testing.T) {
	ctx := context.Background()
	source := t.TempDir()
	if err := os.WriteFile(filepath.Join(source, "song.ogg"), buildOGG("Nightfall", "Example Artist", "Demo Album", 1), 0o644); err != nil {
		t.Fatalf("write ogg: %v", err)
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
	artifacts, err := store.ListAdmittedArtifacts(ctx, ingested.WorkspaceID, ingested.SnapshotRef)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(artifacts) != 1 || artifacts[0].CapabilityID != CapabilityAudioTags {
		t.Fatalf("artifacts = %+v", artifacts)
	}
	var track audioTrack
	if err := json.Unmarshal([]byte(artifacts[0].Body), &track); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if track.Title != "Nightfall" || track.Container != "ogg" {
		t.Fatalf("admitted ogg track = %+v", track)
	}
}

func buildOGG(title, artist, album string, track int) []byte {
	comments := []string{
		"TITLE=" + title,
		"ARTIST=" + artist,
		"ALBUM=" + album,
		"TRACKNUMBER=" + itoa(track),
	}
	vendor := []byte("restoreweave-test")
	var vorbis bytes.Buffer
	vorbis.WriteByte(3)
	vorbis.WriteString("vorbis")
	_ = binary.Write(&vorbis, binary.LittleEndian, uint32(len(vendor)))
	vorbis.Write(vendor)
	_ = binary.Write(&vorbis, binary.LittleEndian, uint32(len(comments)))
	for _, comment := range comments {
		_ = binary.Write(&vorbis, binary.LittleEndian, uint32(len(comment)))
		vorbis.WriteString(comment)
	}
	payload := vorbis.Bytes()
	var segs []byte
	remaining := len(payload)
	for remaining > 255 {
		segs = append(segs, 255)
		remaining -= 255
	}
	segs = append(segs, byte(remaining))
	header := make([]byte, 27)
	copy(header, "OggS")
	binary.LittleEndian.PutUint32(header[14:], 1)
	header[26] = byte(len(segs))
	page := append(header, segs...)
	return append(page, payload...)
}

func putSynchsafe(n int) []byte {
	return []byte{byte((n >> 21) & 0x7f), byte((n >> 14) & 0x7f), byte((n >> 7) & 0x7f), byte(n & 0x7f)}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [16]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
