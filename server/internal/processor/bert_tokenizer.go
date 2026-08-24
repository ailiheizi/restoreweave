package processor

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// bertTokenizer is the bounded tokenizer profile used by the admitted
// bge-small-zh-v1.5 bundle. It intentionally supports only the exact
// HuggingFace fast-tokenizer shape admitted below; it is not a general
// tokenizer package.
type bertTokenizer struct {
	vocab                map[string]int
	unknownID            int
	clsID                int
	sepID                int
	continuingPrefix     string
	maxInputCharsPerWord int
	maxTokens            int
	addedTokens          []bertAddedToken
}

type bertEncoded struct {
	ids         []int64
	inputTokens int
	truncated   bool
	attention   []int64
	tokenTypes  []int64
}

type bertTokenizerJSON struct {
	Version     string           `json:"version"`
	Truncation  json.RawMessage  `json:"truncation"`
	Padding     json.RawMessage  `json:"padding"`
	AddedTokens []bertAddedToken `json:"added_tokens"`
	Normalizer  struct {
		Type               string          `json:"type"`
		CleanText          bool            `json:"clean_text"`
		HandleChineseChars bool            `json:"handle_chinese_chars"`
		StripAccents       json.RawMessage `json:"strip_accents"`
		Lowercase          bool            `json:"lowercase"`
	} `json:"normalizer"`
	PreTokenizer struct {
		Type string `json:"type"`
	} `json:"pre_tokenizer"`
	PostProcessor struct {
		Type          string                      `json:"type"`
		Single        []bertTemplatePiece         `json:"single"`
		SpecialTokens map[string]bertSpecialToken `json:"special_tokens"`
	} `json:"post_processor"`
	Decoder struct {
		Type    string `json:"type"`
		Prefix  string `json:"prefix"`
		Cleanup bool   `json:"cleanup"`
	} `json:"decoder"`
	Model struct {
		Type                    string         `json:"type"`
		Vocab                   map[string]int `json:"vocab"`
		UnkToken                string         `json:"unk_token"`
		ContinuingSubwordPrefix string         `json:"continuing_subword_prefix"`
		MaxInputCharsPerWord    int            `json:"max_input_chars_per_word"`
	} `json:"model"`
}

type bertTemplatePiece struct {
	SpecialToken *bertSpecialPiece  `json:"SpecialToken,omitempty"`
	Sequence     *bertSequencePiece `json:"Sequence,omitempty"`
}

type bertSpecialPiece struct {
	ID     string `json:"id"`
	TypeID int    `json:"type_id"`
}

type bertSequencePiece struct {
	ID     string `json:"id"`
	TypeID int    `json:"type_id"`
}

type bertSpecialToken struct {
	ID     string   `json:"id"`
	IDs    []int    `json:"ids"`
	Tokens []string `json:"tokens"`
}

type bertAddedToken struct {
	ID         int    `json:"id"`
	Content    string `json:"content"`
	SingleWord bool   `json:"single_word"`
	LStrip     bool   `json:"lstrip"`
	RStrip     bool   `json:"rstrip"`
	Normalized bool   `json:"normalized"`
	Special    bool   `json:"special"`
}

var errInvalidBertTokenizer = errors.New("invalid admitted BERT tokenizer")

func loadBertTokenizer(data []byte, maxTokens int) (*bertTokenizer, error) {
	if len(data) == 0 || maxTokens < 3 {
		return nil, fmt.Errorf("%w: empty tokenizer or token budget", errInvalidBertTokenizer)
	}
	var raw bertTokenizerJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return nil, fmt.Errorf("%w: decode JSON: %v", errInvalidBertTokenizer, err)
	}
	if raw.Version != "1.0" || string(raw.Truncation) != "null" || string(raw.Padding) != "null" {
		return nil, fmt.Errorf("%w: unsupported tokenizer envelope", errInvalidBertTokenizer)
	}
	if raw.Normalizer.Type != "BertNormalizer" || !raw.Normalizer.CleanText || !raw.Normalizer.HandleChineseChars || string(raw.Normalizer.StripAccents) != "null" || raw.Normalizer.Lowercase {
		return nil, fmt.Errorf("%w: unsupported normalizer", errInvalidBertTokenizer)
	}
	if raw.PreTokenizer.Type != "BertPreTokenizer" || raw.PostProcessor.Type != "TemplateProcessing" || raw.Decoder.Type != "WordPiece" || raw.Decoder.Prefix != "##" || !raw.Decoder.Cleanup || raw.Model.Type != "WordPiece" {
		return nil, fmt.Errorf("%w: unsupported tokenizer components", errInvalidBertTokenizer)
	}
	if raw.Model.UnkToken != "[UNK]" || raw.Model.ContinuingSubwordPrefix != "##" || raw.Model.MaxInputCharsPerWord != 100 || len(raw.Model.Vocab) != 21128 {
		return nil, fmt.Errorf("%w: unsupported WordPiece profile", errInvalidBertTokenizer)
	}
	ids := map[string]int{"[UNK]": 100, "[CLS]": 101, "[SEP]": 102}
	seenIDs := make(map[int]struct{}, len(raw.Model.Vocab))
	for token, id := range raw.Model.Vocab {
		if id < 0 || id >= len(raw.Model.Vocab) {
			return nil, fmt.Errorf("%w: token %q has out-of-range id", errInvalidBertTokenizer, token)
		}
		if _, exists := seenIDs[id]; exists {
			return nil, fmt.Errorf("%w: duplicate vocabulary id %d", errInvalidBertTokenizer, id)
		}
		seenIDs[id] = struct{}{}
	}
	for token, wantID := range ids {
		id, ok := raw.Model.Vocab[token]
		if !ok || id != wantID {
			return nil, fmt.Errorf("%w: required token %q has unexpected id", errInvalidBertTokenizer, token)
		}
	}
	wantAdded := map[string]int{"[PAD]": 0, "[UNK]": 100, "[CLS]": 101, "[SEP]": 102, "[MASK]": 103}
	if len(raw.AddedTokens) != len(wantAdded) {
		return nil, fmt.Errorf("%w: unexpected added-token set", errInvalidBertTokenizer)
	}
	seenAdded := make(map[string]struct{}, len(raw.AddedTokens))
	for _, token := range raw.AddedTokens {
		wantID, ok := wantAdded[token.Content]
		if !ok || token.ID != wantID || token.SingleWord || token.LStrip || token.RStrip || token.Normalized || !token.Special || raw.Model.Vocab[token.Content] != wantID {
			return nil, fmt.Errorf("%w: added token %q is inconsistent", errInvalidBertTokenizer, token.Content)
		}
		if _, duplicate := seenAdded[token.Content]; duplicate {
			return nil, fmt.Errorf("%w: duplicate added token %q", errInvalidBertTokenizer, token.Content)
		}
		seenAdded[token.Content] = struct{}{}
	}
	if len(raw.PostProcessor.Single) != 3 || raw.PostProcessor.Single[0].SpecialToken == nil || raw.PostProcessor.Single[1].Sequence == nil || raw.PostProcessor.Single[2].SpecialToken == nil ||
		raw.PostProcessor.Single[0].SpecialToken.ID != "[CLS]" || raw.PostProcessor.Single[0].SpecialToken.TypeID != 0 ||
		raw.PostProcessor.Single[1].Sequence.ID != "A" || raw.PostProcessor.Single[1].Sequence.TypeID != 0 ||
		raw.PostProcessor.Single[2].SpecialToken.ID != "[SEP]" || raw.PostProcessor.Single[2].SpecialToken.TypeID != 0 {
		return nil, fmt.Errorf("%w: unsupported single-sequence post processor", errInvalidBertTokenizer)
	}
	if len(raw.PostProcessor.SpecialTokens) != 2 {
		return nil, fmt.Errorf("%w: unexpected post-processor tokens", errInvalidBertTokenizer)
	}
	for token, wantID := range map[string]int{"[CLS]": ids["[CLS]"], "[SEP]": ids["[SEP]"]} {
		special, ok := raw.PostProcessor.SpecialTokens[token]
		if !ok || special.ID != token || len(special.IDs) != 1 || special.IDs[0] != wantID || len(special.Tokens) != 1 || special.Tokens[0] != token {
			return nil, fmt.Errorf("%w: post-processor token %q is inconsistent", errInvalidBertTokenizer, token)
		}
	}
	sort.Slice(raw.AddedTokens, func(i, j int) bool { return len(raw.AddedTokens[i].Content) > len(raw.AddedTokens[j].Content) })
	return &bertTokenizer{
		vocab: raw.Model.Vocab, unknownID: ids["[UNK]"], clsID: ids["[CLS]"], sepID: ids["[SEP]"],
		continuingPrefix: raw.Model.ContinuingSubwordPrefix, maxInputCharsPerWord: raw.Model.MaxInputCharsPerWord,
		maxTokens: maxTokens, addedTokens: raw.AddedTokens,
	}, nil
}

func (t *bertTokenizer) encode(text string) (bertEncoded, error) {
	if t == nil || t.maxTokens < 3 {
		return bertEncoded{}, fmt.Errorf("%w: tokenizer is unavailable", errInvalidBertTokenizer)
	}
	if !utf8.ValidString(text) {
		return bertEncoded{}, fmt.Errorf("%w: text is not UTF-8", errInvalidBertTokenizer)
	}
	maxBody := t.maxTokens - 2
	wordIDs := make([]int64, 0, maxBody)
	inputBodyTokens := 0
	appendPieces := func(pieces []int64) {
		inputBodyTokens += len(pieces)
		remaining := maxBody - len(wordIDs)
		if remaining > len(pieces) {
			remaining = len(pieces)
		}
		if remaining > 0 {
			wordIDs = append(wordIDs, pieces[:remaining]...)
		}
	}
	emitWord := func(word string, overlong bool) {
		pieces := []int64{int64(t.unknownID)}
		if !overlong {
			pieces = t.wordPiece(word)
		}
		appendPieces(pieces)
	}
	t.scan(text, emitWord, func(id int) { appendPieces([]int64{int64(id)}) })
	inputTokens := inputBodyTokens + 2
	truncated := inputBodyTokens > maxBody
	ids := make([]int64, 0, len(wordIDs)+2)
	ids = append(ids, int64(t.clsID))
	ids = append(ids, wordIDs...)
	ids = append(ids, int64(t.sepID))
	attention := make([]int64, len(ids))
	for i := range attention {
		attention[i] = 1
	}
	types := make([]int64, len(ids))
	return bertEncoded{ids: ids, inputTokens: inputTokens, truncated: truncated, attention: attention, tokenTypes: types}, nil
}

func (t *bertTokenizer) scan(text string, emitWord func(string, bool), emitAdded func(int)) {
	for len(text) > 0 {
		index := -1
		var matched bertAddedToken
		for _, token := range t.addedTokens {
			candidate := strings.Index(text, token.Content)
			if candidate >= 0 && (index < 0 || candidate < index) {
				index = candidate
				matched = token
			}
		}
		if index < 0 {
			t.scanOrdinary(text, emitWord)
			return
		}
		t.scanOrdinary(text[:index], emitWord)
		emitAdded(matched.ID)
		text = text[index+len(matched.Content):]
	}
}

func (t *bertTokenizer) scanOrdinary(text string, emit func(string, bool)) {
	current := make([]rune, 0, t.maxInputCharsPerWord)
	overlong := false
	flush := func() {
		if len(current) > 0 || overlong {
			emit(string(current), overlong)
		}
		current = current[:0]
		overlong = false
	}
	for _, r := range text {
		if r == 0 || r == '\ufffd' || isBertControl(r) {
			continue
		}
		if isBertWhitespace(r) {
			flush()
			continue
		}
		if isBertChineseChar(r) || isBertPunctuation(r) {
			flush()
			emit(string(r), false)
			continue
		}
		if len(current) < t.maxInputCharsPerWord {
			current = append(current, r)
		} else {
			overlong = true
		}
	}
	flush()
}

func isBertWhitespace(r rune) bool {
	return r == ' ' || r == '\t' || r == '\n' || r == '\r' || unicode.Is(unicode.Zs, r)
}

func isBertControl(r rune) bool {
	if r == '\t' || r == '\n' || r == '\r' {
		return false
	}
	return unicode.IsControl(r) || unicode.In(r, unicode.Cf, unicode.Co, unicode.Cs)
}

func (t *bertTokenizer) wordPiece(word string) []int64 {
	if word == "" {
		return nil
	}
	runes := []rune(word)
	if len(runes) > t.maxInputCharsPerWord {
		return []int64{int64(t.unknownID)}
	}
	pieces := make([]int64, 0, len(runes))
	for start := 0; start < len(runes); {
		end := len(runes)
		found := ""
		for start < end {
			candidate := string(runes[start:end])
			if start > 0 {
				candidate = t.continuingPrefix + candidate
			}
			if _, ok := t.vocab[candidate]; ok {
				found = candidate
				break
			}
			end--
		}
		if found == "" {
			return []int64{int64(t.unknownID)}
		}
		pieces = append(pieces, int64(t.vocab[found]))
		start = end
	}
	return pieces
}

func isBertPunctuation(r rune) bool {
	if (r >= 33 && r <= 47) || (r >= 58 && r <= 64) || (r >= 91 && r <= 96) || (r >= 123 && r <= 126) {
		return true
	}
	return unicode.IsPunct(r)
}

func isBertChineseChar(r rune) bool {
	return (r >= 0x4e00 && r <= 0x9fff) || (r >= 0x3400 && r <= 0x4dbf) ||
		(r >= 0x20000 && r <= 0x2a6df) || (r >= 0x2a700 && r <= 0x2b73f) ||
		(r >= 0x2b740 && r <= 0x2b81f) || (r >= 0x2b820 && r <= 0x2ceaf) ||
		(r >= 0xf900 && r <= 0xfaff) || (r >= 0x2f800 && r <= 0x2fa1f)
}
