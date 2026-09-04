// Package qualificationcorpus builds the small, deterministic corpus used by
// repository qualification experiments. It is test infrastructure, not a
// product data model or a user-facing command.
package qualificationcorpus

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

const (
	Schema        = "restoreweave.qualification-corpus.v1"
	DefaultSeed   = "restoreweave-phase5-heterogeneous-v1"
	SelfGenerated = "self-generated"
)

// Options controls deterministic generation. Seed is recorded in the
// manifest and changing it intentionally changes the corpus digest.
type Options struct {
	Seed    string
	SizeMiB int
}

// Entry records one generated regular file and its role in the corpus.
type Entry struct {
	Path            string `json:"path"`
	Category        string `json:"category"`
	Bytes           int64  `json:"bytes"`
	SHA256          string `json:"sha256"`
	DuplicateOf     string `json:"duplicate_of,omitempty"`
	NearDuplicateOf string `json:"near_duplicate_of,omitempty"`
}

// Manifest is the reviewable descriptor for one generated corpus. Digest is
// SHA-256 over the canonical schema/seed/provenance/license/entries payload.
type Manifest struct {
	Schema     string  `json:"schema"`
	Seed       string  `json:"seed"`
	Provenance string  `json:"provenance"`
	License    string  `json:"license"`
	Entries    []Entry `json:"entries"`
	Digest     string  `json:"digest"`
}

type manifestPayload struct {
	Schema     string  `json:"schema"`
	Seed       string  `json:"seed"`
	Provenance string  `json:"provenance"`
	License    string  `json:"license"`
	Entries    []Entry `json:"entries"`
}

// Generate creates a new corpus at root and returns its deterministic
// manifest. The destination and every parent component must be a new path
// under real directories; existing destinations and symlink parents fail.
func Generate(root string) (Manifest, error) {
	return GenerateWithOptions(root, Options{Seed: DefaultSeed})
}

// GenerateWithOptions is Generate with an explicit deterministic seed.
func GenerateWithOptions(root string, options Options) (Manifest, error) {
	absolute, err := prepareNewRoot(root)
	if err != nil {
		return Manifest{}, err
	}
	if strings.TrimSpace(options.Seed) == "" {
		options.Seed = DefaultSeed
	}

	files, err := buildFiles(options.Seed, options.SizeMiB)
	if err != nil {
		return Manifest{}, err
	}
	if err := os.Mkdir(absolute, 0o755); err != nil {
		return Manifest{}, fmt.Errorf("create corpus root: %w", err)
	}
	removeOnError := true
	defer func() {
		if removeOnError {
			_ = os.RemoveAll(absolute)
		}
	}()
	for _, file := range files {
		path := filepath.Join(absolute, filepath.FromSlash(file.path))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return Manifest{}, fmt.Errorf("create parent for %q: %w", file.path, err)
		}
		if err := os.WriteFile(path, file.body, 0o644); err != nil {
			return Manifest{}, fmt.Errorf("write %q: %w", file.path, err)
		}
	}
	manifest, err := BuildManifest(absolute, options.Seed)
	if err != nil {
		return Manifest{}, err
	}
	removeOnError = false
	return manifest, nil
}

// GeneratePair creates two independent copies and rejects overlapping target
// paths before either target is created.
func GeneratePair(first, second string) (Manifest, Manifest, error) {
	firstAbs, err := resolveDestination(first)
	if err != nil {
		return Manifest{}, Manifest{}, err
	}
	secondAbs, err := resolveDestination(second)
	if err != nil {
		return Manifest{}, Manifest{}, err
	}
	if pathsOverlap(firstAbs, secondAbs) {
		return Manifest{}, Manifest{}, errors.New("corpus destinations overlap")
	}
	firstAbs, err = prepareNewRoot(firstAbs)
	if err != nil {
		return Manifest{}, Manifest{}, err
	}
	secondAbs, err = prepareNewRoot(secondAbs)
	if err != nil {
		return Manifest{}, Manifest{}, err
	}
	a, err := Generate(firstAbs)
	if err != nil {
		return Manifest{}, Manifest{}, err
	}
	b, err := Generate(secondAbs)
	if err != nil {
		_ = os.RemoveAll(firstAbs)
		return Manifest{}, Manifest{}, err
	}
	return a, b, nil
}

// BuildManifest scans a generated corpus without changing it. The seed must
// be supplied by the caller because the filesystem contents do not carry it.
func BuildManifest(root, seed string) (Manifest, error) {
	if strings.TrimSpace(seed) == "" {
		seed = DefaultSeed
	}
	info, err := os.Lstat(root)
	if err != nil {
		return Manifest{}, fmt.Errorf("stat corpus: %w", err)
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
		return Manifest{}, errors.New("corpus root must be a real directory")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return Manifest{}, fmt.Errorf("resolve corpus: %w", err)
	}
	entries := make([]Entry, 0)
	err = filepath.WalkDir(absolute, func(path string, item os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == absolute {
			return nil
		}
		if item.Type()&os.ModeSymlink != 0 {
			return fmt.Errorf("corpus contains symlink %q", path)
		}
		if item.IsDir() {
			return nil
		}
		if !item.Type().IsRegular() {
			return fmt.Errorf("corpus contains non-regular file %q", path)
		}
		relative, err := filepath.Rel(absolute, path)
		if err != nil {
			return err
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		digest := sha256.Sum256(body)
		entries = append(entries, Entry{
			Path:     filepath.ToSlash(relative),
			Category: categoryForPath(relative),
			Bytes:    int64(len(body)),
			SHA256:   hex.EncodeToString(digest[:]),
		})
		return nil
	})
	if err != nil {
		return Manifest{}, err
	}
	if len(entries) == 0 {
		return Manifest{}, errors.New("corpus contains no regular files")
	}
	sort.Slice(entries, func(i, j int) bool { return entries[i].Path < entries[j].Path })
	annotateRelationships(entries)
	manifest := Manifest{
		Schema:     Schema,
		Seed:       seed,
		Provenance: "qualificationcorpus.GenerateWithOptions",
		License:    SelfGenerated,
		Entries:    entries,
	}
	digest, err := digestManifest(manifest)
	if err != nil {
		return Manifest{}, err
	}
	manifest.Digest = digest
	return manifest, nil
}

// WriteManifest writes a manifest outside the corpus using an atomic rename.
func WriteManifest(path string, manifest Manifest) error {
	if strings.TrimSpace(path) == "" {
		return errors.New("manifest path is required")
	}
	body, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	body = append(body, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".qualification-corpus-*.tmp")
	if err != nil {
		return err
	}
	temporaryName := temporary.Name()
	defer func() {
		_ = temporary.Close()
		_ = os.Remove(temporaryName)
	}()
	if _, err := temporary.Write(body); err != nil {
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryName, path)
}

func prepareNewRoot(root string) (string, error) {
	absolute, err := resolveDestination(root)
	if err != nil {
		return "", fmt.Errorf("resolve corpus destination: %w", err)
	}
	if filepath.Base(absolute) == "." || filepath.Base(absolute) == ".." {
		return "", errors.New("invalid corpus destination")
	}
	if err := rejectSymlinkComponents(filepath.Dir(absolute)); err != nil {
		return "", err
	}
	if info, err := os.Lstat(absolute); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return "", errors.New("corpus destination must not be a symlink")
		}
		return "", errors.New("corpus destination already exists")
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("inspect corpus destination: %w", err)
	}
	return absolute, nil
}

func resolveDestination(root string) (string, error) {
	if strings.TrimSpace(root) == "" {
		return "", errors.New("corpus destination is required")
	}
	absolute, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	return absolute, nil
}

func rejectSymlinkComponents(path string) error {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	volume := filepath.VolumeName(absolute)
	remainder := strings.TrimPrefix(absolute, volume)
	current := volume + string(filepath.Separator)
	for _, component := range strings.Split(strings.TrimPrefix(remainder, string(filepath.Separator)), string(filepath.Separator)) {
		if component == "" {
			continue
		}
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return fmt.Errorf("corpus parent does not exist: %s", current)
			}
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			// macOS commonly exposes temporary directories through /tmp or
			// /var. Those stable system aliases are safe; user-created links
			// below them remain rejected.
			if !allowedTemporaryAlias(current) {
				return fmt.Errorf("corpus destination parent contains symlink %q", current)
			}
			continue
		}
		if !info.IsDir() {
			return fmt.Errorf("corpus destination parent is not a directory: %q", current)
		}
	}
	return nil
}

func allowedTemporaryAlias(path string) bool {
	for _, alias := range []string{"/tmp", "/var"} {
		if filepath.Clean(path) == alias {
			return true
		}
	}
	return false
}

func pathsOverlap(first, second string) bool {
	first = filepath.Clean(first)
	second = filepath.Clean(second)
	if first == second {
		return true
	}
	firstToSecond, err := filepath.Rel(first, second)
	if err == nil && firstToSecond != ".." && !strings.HasPrefix(firstToSecond, ".."+string(filepath.Separator)) {
		return true
	}
	secondToFirst, err := filepath.Rel(second, first)
	return err == nil && secondToFirst != ".." && !strings.HasPrefix(secondToFirst, ".."+string(filepath.Separator))
}

type generatedFile struct {
	path string
	body []byte
}

func buildFiles(seed string, sizeMiB int) ([]generatedFile, error) {
	textBody := []byte("RestoreWeave qualification corpus.\nThis is deterministic self-generated text for lossless storage tests.\n")
	textBody = bytes.Repeat(textBody, 48)
	nearText := append([]byte(nil), textBody...)
	nearText[len(nearText)/2] = 'X'

	sourceBody := []byte("package sample\n\nfunc Add(a, b int) int { return a + b }\n")
	jsonBody, err := json.MarshalIndent(struct {
		Seed  string           `json:"seed"`
		Items []map[string]any `json:"items"`
	}{Seed: seed, Items: []map[string]any{{"id": 1, "kind": "text", "active": true}, {"id": 2, "kind": "binary", "active": false}}}, "", "  ")
	if err != nil {
		return nil, err
	}
	pdfBody := minimalPDF()
	pngBody, err := encodedPNG(false)
	if err != nil {
		return nil, err
	}
	nearPNG, err := encodedPNG(true)
	if err != nil {
		return nil, err
	}
	jpegBody, err := encodedJPEG()
	if err != nil {
		return nil, err
	}
	gifBody, err := encodedGIF()
	if err != nil {
		return nil, err
	}
	wavBody := encodedWAV()
	zipBody, err := encodedZIP()
	if err != nil {
		return nil, err
	}
	tarGZBody, err := encodedTARGZ()
	if err != nil {
		return nil, err
	}
	opaqueBytes := 96 << 10
	if sizeMiB > 0 {
		if sizeMiB > 64 {
			return nil, errors.New("corpus size must not exceed 64 MiB")
		}
		opaqueBytes = sizeMiB << 20
	}
	opaque := seededBytes(seed, opaqueBytes)
	return []generatedFile{
		{path: "docs/readme.txt", body: textBody},
		{path: "docs/readme-copy.txt", body: append([]byte(nil), textBody...)},
		{path: "docs/readme-edited.txt", body: nearText},
		{path: "src/example.go", body: sourceBody},
		{path: "src/example-copy.go", body: append([]byte(nil), sourceBody...)},
		{path: "data/records.json", body: jsonBody},
		{path: "documents/sample.pdf", body: pdfBody},
		{path: "media/sample.png", body: pngBody},
		{path: "media/sample-near.png", body: nearPNG},
		{path: "media/sample.jpg", body: jpegBody},
		{path: "media/sample.gif", body: gifBody},
		{path: "media/sample.wav", body: wavBody},
		{path: "archives/sample.zip", body: zipBody},
		{path: "archives/sample.tar.gz", body: tarGZBody},
		{path: "binary/opaque.bin", body: opaque},
	}, nil
}

func categoryForPath(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".txt":
		return "text"
	case ".go":
		return "source"
	case ".json":
		return "json"
	case ".pdf":
		return "pdf"
	case ".png":
		return "png"
	case ".jpg", ".jpeg":
		return "jpeg"
	case ".gif":
		return "gif"
	case ".wav":
		return "wav"
	case ".zip":
		return "zip"
	case ".gz":
		return "tar.gz"
	default:
		return "opaque"
	}
}

func annotateRelationships(entries []Entry) {
	byDigest := make(map[string]string, len(entries))
	for i := range entries {
		if previous, ok := byDigest[entries[i].SHA256]; ok {
			entries[i].DuplicateOf = previous
		} else {
			byDigest[entries[i].SHA256] = entries[i].Path
		}
	}
	for i := range entries {
		if strings.HasSuffix(entries[i].Path, "-edited.txt") {
			entries[i].NearDuplicateOf = strings.TrimSuffix(entries[i].Path, "-edited.txt") + ".txt"
		}
		if strings.HasSuffix(entries[i].Path, "-near.png") {
			entries[i].NearDuplicateOf = strings.TrimSuffix(entries[i].Path, "-near.png") + ".png"
		}
	}
}

func digestManifest(manifest Manifest) (string, error) {
	payload, err := json.Marshal(manifestPayload{Schema: manifest.Schema, Seed: manifest.Seed, Provenance: manifest.Provenance, License: manifest.License, Entries: manifest.Entries})
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}

func seededBytes(seed string, size int) []byte {
	state := sha256.Sum256([]byte(seed))
	value := binary.LittleEndian.Uint64(state[:8])
	result := make([]byte, size)
	for i := range result {
		value = value*6364136223846793005 + 1442695040888963407
		result[i] = byte(value >> 56)
	}
	return result
}

func encodedPNG(near bool) ([]byte, error) {
	canvas := image.NewRGBA(image.Rect(0, 0, 4, 3))
	colors := []color.RGBA{{R: 24, G: 32, B: 48, A: 255}, {R: 116, G: 72, B: 196, A: 255}, {R: 65, G: 190, B: 150, A: 255}}
	for y := 0; y < 3; y++ {
		for x := 0; x < 4; x++ {
			c := colors[(x+y)%len(colors)]
			if near && x == 2 && y == 1 {
				c = color.RGBA{R: 240, G: 120, B: 70, A: 255}
			}
			canvas.SetRGBA(x, y, c)
		}
	}
	var output bytes.Buffer
	if err := png.Encode(&output, canvas); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func encodedJPEG() ([]byte, error) {
	canvas := image.NewRGBA(image.Rect(0, 0, 4, 3))
	for y := 0; y < 3; y++ {
		for x := 0; x < 4; x++ {
			canvas.SetRGBA(x, y, color.RGBA{R: uint8(40 + x*30), G: uint8(80 + y*30), B: 160, A: 255})
		}
	}
	var output bytes.Buffer
	if err := jpeg.Encode(&output, canvas, &jpeg.Options{Quality: 80}); err != nil {
		return nil, err
	}
	body := output.Bytes()
	if _, err := jpeg.Decode(bytes.NewReader(body)); err != nil {
		return nil, fmt.Errorf("embedded JPEG fixture is invalid: %w", err)
	}
	return body, nil
}

func encodedGIF() ([]byte, error) {
	canvas := image.NewPaletted(image.Rect(0, 0, 4, 3), color.Palette{color.Black, color.RGBA{R: 116, G: 72, B: 196, A: 255}, color.RGBA{R: 65, G: 190, B: 150, A: 255}})
	for i := range canvas.Pix {
		canvas.Pix[i] = uint8(i % 3)
	}
	var output bytes.Buffer
	if err := gif.Encode(&output, canvas, nil); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func encodedWAV() []byte {
	const sampleRate = 8000
	samples := make([]byte, sampleRate/10*2)
	for i := 0; i < len(samples); i += 2 {
		value := int16((i / 2) % 256 * 128)
		binary.LittleEndian.PutUint16(samples[i:i+2], uint16(value))
	}
	var output bytes.Buffer
	output.WriteString("RIFF")
	binary.Write(&output, binary.LittleEndian, uint32(36+len(samples)))
	output.WriteString("WAVEfmt ")
	binary.Write(&output, binary.LittleEndian, uint32(16))
	binary.Write(&output, binary.LittleEndian, uint16(1))
	binary.Write(&output, binary.LittleEndian, uint16(1))
	binary.Write(&output, binary.LittleEndian, uint32(sampleRate))
	binary.Write(&output, binary.LittleEndian, uint32(sampleRate*2))
	binary.Write(&output, binary.LittleEndian, uint16(2))
	binary.Write(&output, binary.LittleEndian, uint16(16))
	output.WriteString("data")
	binary.Write(&output, binary.LittleEndian, uint32(len(samples)))
	output.Write(samples)
	return output.Bytes()
}

func encodedZIP() ([]byte, error) {
	var output bytes.Buffer
	archive := zip.NewWriter(&output)
	header := &zip.FileHeader{Name: "inside.txt", Method: zip.Deflate}
	header.SetModTime(time.Unix(0, 0).UTC())
	writer, err := archive.CreateHeader(header)
	if err != nil {
		return nil, err
	}
	if _, err := io.WriteString(writer, "ZIP member from the deterministic corpus.\n"); err != nil {
		return nil, err
	}
	if err := archive.Close(); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func encodedTARGZ() ([]byte, error) {
	var output bytes.Buffer
	compressor := gzip.NewWriter(&output)
	compressor.Header.ModTime = time.Unix(0, 0).UTC()
	archive := tar.NewWriter(compressor)
	body := []byte("TAR.GZ member from the deterministic corpus.\n")
	header := &tar.Header{Name: "inside.txt", Mode: 0o644, Size: int64(len(body)), ModTime: time.Unix(0, 0).UTC(), Format: tar.FormatUSTAR}
	if err := archive.WriteHeader(header); err != nil {
		return nil, err
	}
	if _, err := archive.Write(body); err != nil {
		return nil, err
	}
	if err := archive.Close(); err != nil {
		return nil, err
	}
	if err := compressor.Close(); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func minimalPDF() []byte {
	stream := "BT /F1 12 Tf 20 50 Td (RestoreWeave) Tj ET\n"
	objects := []string{
		"1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n",
		"2 0 obj\n<< /Type /Pages /Kids [3 0 R] /Count 1 >>\nendobj\n",
		"3 0 obj\n<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 100] /Contents 4 0 R /Resources << >> >>\nendobj\n",
		fmt.Sprintf("4 0 obj\n<< /Length %d >>\nstream\n%sendstream\nendobj\n", len(stream), stream),
	}
	var output bytes.Buffer
	output.WriteString("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n")
	offsets := make([]int, 0, len(objects))
	for _, object := range objects {
		offsets = append(offsets, output.Len())
		output.WriteString(object)
	}
	xref := output.Len()
	fmt.Fprintf(&output, "xref\n0 %d\n0000000000 65535 f \n", len(objects)+1)
	for _, offset := range offsets {
		fmt.Fprintf(&output, "%010d 00000 n \n", offset)
	}
	fmt.Fprintf(&output, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objects)+1, xref)
	return output.Bytes()
}
