package processor

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"io"
	"path"
	"strings"
)

// BookMeta is a BYTE_DETERMINISTIC EXTRACT processor for EPUB OPF metadata.
// It never opens paths and does not rewrite source bytes.
type BookMeta struct{}

func (BookMeta) CapabilityID() string { return CapabilityBookMeta }
func (BookMeta) Stage() Stage         { return StageExtract }

type bookWork struct {
	Title     string `json:"title,omitempty"`
	Author    string `json:"author,omitempty"`
	Year      string `json:"year,omitempty"`
	Language  string `json:"language,omitempty"`
	Container string `json:"container,omitempty"`
}

func (BookMeta) RunStage(ctx context.Context, inv Invocation) (StageResult, error) {
	if err := ctx.Err(); err != nil {
		return StageResult{Status: StatusCancelled, Reason: err.Error()}, err
	}
	if inv.Source == nil || inv.Staging == nil {
		return StageResult{Status: StatusFailed, Reason: "source and staging handles are required"}, nil
	}
	body, err := inv.Source.ReadAll(ctx)
	if err != nil {
		return StageResult{Status: StatusFailed, Reason: err.Error()}, nil
	}
	work, ok := parseEPUB(body)
	if !ok {
		return StageResult{Status: StatusInapplicable, Reason: "no parseable EPUB metadata"}, nil
	}
	payload, err := json.Marshal(work)
	if err != nil {
		return StageResult{Status: StatusFailed, Reason: err.Error()}, nil
	}
	if _, err := inv.Staging.Write(payload); err != nil {
		return StageResult{Status: StatusFailed, Reason: err.Error()}, nil
	}
	if err := inv.Staging.Seal(); err != nil {
		return StageResult{Status: StatusFailed, Reason: err.Error()}, nil
	}
	return StageResult{
		Status:           StatusSucceeded,
		DeterminismClass: DeterminismByteExact,
		SchemaRef:        SchemaRefBookWork(),
		MediaType:        MediaTypeBookJSON,
		Sealed:           true,
	}, nil
}

func parseEPUB(body []byte) (bookWork, bool) {
	reader, err := zip.NewReader(bytes.NewReader(body), int64(len(body)))
	if err != nil {
		return bookWork{}, false
	}
	if mime, ok := zipNamedFile(reader, "mimetype"); ok {
		if strings.TrimSpace(string(mime)) != "application/epub+zip" {
			return bookWork{}, false
		}
	}
	container, ok := zipNamedFile(reader, "META-INF/container.xml")
	if !ok {
		return bookWork{}, false
	}
	opfPath := containerRootfile(container)
	if opfPath == "" {
		return bookWork{}, false
	}
	opf, ok := zipNamedFile(reader, opfPath)
	if !ok {
		return bookWork{}, false
	}
	work := parseOPF(opf)
	work.Container = "epub"
	return work, work.Title != "" || work.Author != ""
}

func zipNamedFile(reader *zip.Reader, name string) ([]byte, bool) {
	want := strings.TrimPrefix(path.Clean("/"+strings.ReplaceAll(name, "\\", "/")), "/")
	if want == "" || strings.HasPrefix(want, "../") {
		return nil, false
	}
	for _, file := range reader.File {
		got := strings.TrimPrefix(path.Clean("/"+strings.ReplaceAll(file.Name, "\\", "/")), "/")
		if !strings.EqualFold(got, want) {
			continue
		}
		src, err := file.Open()
		if err != nil {
			return nil, false
		}
		payload, err := io.ReadAll(io.LimitReader(src, defaultMaxSourceBytes))
		_ = src.Close()
		if err != nil {
			return nil, false
		}
		return payload, true
	}
	return nil, false
}

func containerRootfile(body []byte) string {
	decoder := xml.NewDecoder(bytes.NewReader(body))
	for {
		token, err := decoder.Token()
		if err != nil {
			return ""
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "rootfile" {
			continue
		}
		for _, attr := range start.Attr {
			if attr.Name.Local == "full-path" && strings.TrimSpace(attr.Value) != "" {
				return strings.TrimSpace(attr.Value)
			}
		}
	}
}

func parseOPF(body []byte) bookWork {
	work := bookWork{}
	decoder := xml.NewDecoder(bytes.NewReader(body))
	for {
		token, err := decoder.Token()
		if err != nil {
			return work
		}
		start, ok := token.(xml.StartElement)
		if !ok {
			continue
		}
		switch start.Name.Local {
		case "title":
			if work.Title == "" {
				work.Title = xmlText(decoder, start)
			}
		case "creator":
			if work.Author == "" {
				work.Author = xmlText(decoder, start)
			}
		case "date":
			if work.Year == "" {
				work.Year = yearPrefix(xmlText(decoder, start))
			}
		case "language":
			if work.Language == "" {
				work.Language = xmlText(decoder, start)
			}
		}
	}
}

func xmlText(decoder *xml.Decoder, start xml.StartElement) string {
	var value string
	if err := decoder.DecodeElement(&value, &start); err != nil {
		return ""
	}
	return strings.TrimSpace(value)
}

func yearPrefix(value string) string {
	value = strings.TrimSpace(value)
	if len(value) >= 4 {
		return value[:4]
	}
	return value
}
