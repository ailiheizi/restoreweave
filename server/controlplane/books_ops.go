package controlplane

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/ailiheizi/restoreweave/client/command"
	"github.com/ailiheizi/restoreweave/server/internal/processor"
)

type bookListInput struct {
	WorkspaceID string `json:"workspace_id"`
	SnapshotRef string `json:"snapshot_ref,omitempty"`
}

func (d *Dispatcher) handleBooksList(ctx context.Context, env command.Envelope, started time.Time) command.Result {
	if d.exact == nil {
		return unimplementedResult(env, started)
	}
	var input bookListInput
	if err := decodeInput(env.Input, &input); err != nil {
		return invalidInputResult(env, started, err)
	}
	if err := requireStableID("workspace_id", input.WorkspaceID); err != nil {
		return invalidInputResult(env, started, err)
	}
	artifacts, err := d.store.ListAdmittedArtifacts(ctx, input.WorkspaceID, input.SnapshotRef)
	if err != nil {
		return catalogErrorResult(env, started, err)
	}
	works := make([]command.BookWork, 0)
	for _, artifact := range artifacts {
		name := ""
		if entry, err := d.store.GetNamespaceEntry(ctx, input.WorkspaceID, artifact.SubjectRef); err == nil {
			name = entry.DisplayName
		}
		switch artifact.CapabilityID {
		case processor.CapabilityBookMeta:
			var parsed struct {
				Title  string `json:"title"`
				Author string `json:"author"`
				Year   string `json:"year"`
			}
			_ = json.Unmarshal([]byte(artifact.Body), &parsed)
			works = append(works, command.BookWork{
				SubjectRef: artifact.SubjectRef,
				Name:       name,
				Title:      parsed.Title,
				Author:     parsed.Author,
				Year:       parsed.Year,
				Kind:       "epub",
				ArtifactID: artifact.ID,
			})
		case processor.CapabilityTextExtract:
			if !isCatalogTextName(name) {
				continue
			}
			works = append(works, command.BookWork{
				SubjectRef: artifact.SubjectRef,
				Name:       name,
				Title:      titleFromExtractedText(artifact.Body, name),
				Kind:       "text",
				ArtifactID: artifact.ID,
			})
		}
	}
	sortBookWorks(works)
	return succeeded(env, started, command.BookListData{
		WorkspaceID: input.WorkspaceID,
		SnapshotRef: input.SnapshotRef,
		Authors:     groupBookAuthors(works),
		Works:       works,
	})
}

func isCatalogTextName(name string) bool {
	lower := strings.ToLower(name)
	return strings.HasSuffix(lower, ".txt") || strings.HasSuffix(lower, ".md")
}

func titleFromExtractedText(body, fallback string) string {
	line := ""
	for _, candidate := range strings.Split(body, "\n") {
		candidate = strings.TrimSpace(candidate)
		if candidate == "" {
			continue
		}
		line = candidate
		break
	}
	line = strings.TrimSpace(strings.TrimLeftFunc(line, func(r rune) bool {
		return r == '#' || unicode.IsSpace(r)
	}))
	if line == "" {
		return fallback
	}
	if utf8.RuneCountInString(line) > 120 {
		runes := []rune(line)
		return string(runes[:120])
	}
	return line
}

func sortBookWorks(works []command.BookWork) {
	sort.Slice(works, func(i, j int) bool {
		a, b := works[i], works[j]
		if a.Author != b.Author {
			return catalogLabelLess(a.Author, b.Author)
		}
		if a.Title != b.Title {
			return catalogLabelLess(a.Title, b.Title)
		}
		if a.Name != b.Name {
			return a.Name < b.Name
		}
		return a.SubjectRef < b.SubjectRef
	})
}

func groupBookAuthors(works []command.BookWork) []command.BookAuthor {
	order := make([]string, 0)
	byName := make(map[string]*command.BookAuthor)
	for _, work := range works {
		author, ok := byName[work.Author]
		if !ok {
			author = &command.BookAuthor{
				Name:        work.Author,
				SubjectRefs: make([]string, 0, 1),
			}
			byName[work.Author] = author
			order = append(order, work.Author)
		}
		author.SubjectRefs = append(author.SubjectRefs, work.SubjectRef)
	}
	authors := make([]command.BookAuthor, 0, len(order))
	for _, name := range order {
		authors = append(authors, *byName[name])
	}
	sort.Slice(authors, func(i, j int) bool {
		return catalogLabelLess(authors[i].Name, authors[j].Name)
	})
	return authors
}
