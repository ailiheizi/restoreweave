package processor

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestBertTokenizerExactProfileAndEncoding(t *testing.T) {
	payload := testBertTokenizerJSON(t)
	tokenizer, err := loadBertTokenizer(payload, 512)
	if err != nil {
		t.Fatalf("load tokenizer: %v", err)
	}

	tests := []struct {
		name string
		text string
		want []int64
	}{
		{name: "Chinese", text: "测试中文语义", want: []int64{101, 3844, 6407, 704, 3152, 6427, 721, 102}},
		{name: "punctuation", text: "hello, world!", want: []int64{101, 8701, 117, 8572, 106, 102}},
		{name: "wordpiece", text: "playing", want: []int64{101, 200, 201, 102}},
		{name: "clean text", text: "hello\x00\u200b\tworld", want: []int64{101, 8701, 8572, 102}},
		{name: "unknown", text: "not-in-vocabulary", want: []int64{101, 100, 118, 100, 118, 100, 102}},
		{name: "added special tokens", text: "[CLS]hello[MASK]", want: []int64{101, 101, 8701, 103, 102}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := tokenizer.encode(tt.text)
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got.ids, tt.want) {
				t.Fatalf("ids = %v, want %v", got.ids, tt.want)
			}
			if got.inputTokens != len(tt.want) || got.truncated {
				t.Fatalf("accounting = tokens:%d truncated:%t", got.inputTokens, got.truncated)
			}
			if len(got.attention) != len(got.ids) || len(got.tokenTypes) != len(got.ids) {
				t.Fatalf("tensor lengths = ids:%d attention:%d types:%d", len(got.ids), len(got.attention), len(got.tokenTypes))
			}
			for i := range got.ids {
				if got.attention[i] != 1 || got.tokenTypes[i] != 0 {
					t.Fatalf("tensor values at %d = attention:%d type:%d", i, got.attention[i], got.tokenTypes[i])
				}
			}
		})
	}
}

func TestBertTokenizerBoundsTruncationAndLongWords(t *testing.T) {
	tokenizer, err := loadBertTokenizer(testBertTokenizerJSON(t), 5)
	if err != nil {
		t.Fatal(err)
	}
	got, err := tokenizer.encode("hello world playing !")
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got.ids, []int64{101, 8701, 8572, 200, 102}) || got.inputTokens != 7 || !got.truncated {
		t.Fatalf("truncated encoding = %+v", got)
	}
	longWord, err := tokenizer.encode(strings.Repeat("x", 101))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(longWord.ids, []int64{101, 100, 102}) || longWord.inputTokens != 3 || longWord.truncated {
		t.Fatalf("long-word encoding = %+v", longWord)
	}
	if _, err := tokenizer.encode(string([]byte{0xff})); err == nil {
		t.Fatal("invalid UTF-8 was accepted")
	}
}

func TestBertTokenizerRejectsProfileDrift(t *testing.T) {
	var profile map[string]any
	if err := json.Unmarshal(testBertTokenizerJSON(t), &profile); err != nil {
		t.Fatal(err)
	}
	normalizer := profile["normalizer"].(map[string]any)
	normalizer["lowercase"] = true
	payload, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := loadBertTokenizer(payload, 512); err == nil {
		t.Fatal("lowercase profile drift was accepted")
	}
}

func TestBertTokenizerConformsToPinnedBGEAsset(t *testing.T) {
	root := os.Getenv("RESTOREWEAVE_BGE_TEST_ASSETS")
	if root == "" {
		t.Skip("RESTOREWEAVE_BGE_TEST_ASSETS is not set")
	}
	payload, err := os.ReadFile(filepath.Join(root, "tokenizer.json"))
	if err != nil {
		t.Fatal(err)
	}
	tokenizer, err := loadBertTokenizer(payload, 512)
	if err != nil {
		t.Fatalf("load pinned tokenizer: %v", err)
	}
	document, err := tokenizer.encode("测试中文语义")
	if err != nil {
		t.Fatal(err)
	}
	wantDocument := []int64{101, 3844, 6407, 704, 3152, 6427, 721, 102}
	if !reflect.DeepEqual(document.ids, wantDocument) {
		t.Fatalf("document ids = %v, want %v", document.ids, wantDocument)
	}
	query, err := tokenizer.encode("为这个句子生成表示以用于检索相关文章：测试中文语义")
	if err != nil {
		t.Fatal(err)
	}
	wantQuery := []int64{101, 711, 6821, 702, 1368, 2094, 4495, 2768, 6134, 4850, 809, 4500, 754, 3466, 5164, 4685, 1068, 3152, 4995, 8038, 3844, 6407, 704, 3152, 6427, 721, 102}
	if !reflect.DeepEqual(query.ids, wantQuery) {
		t.Fatalf("query ids = %v, want %v", query.ids, wantQuery)
	}
}

func testBertTokenizerJSON(t *testing.T) []byte {
	t.Helper()
	vocab := make(map[string]int, 21128)
	for id := 0; id < 21128; id++ {
		vocab[fmt.Sprintf("token-%d", id)] = id
	}
	set := func(token string, id int) {
		delete(vocab, fmt.Sprintf("token-%d", id))
		vocab[token] = id
	}
	for token, id := range map[string]int{
		"[PAD]": 0, "[UNK]": 100, "[CLS]": 101, "[SEP]": 102, "[MASK]": 103,
		"!": 106, ",": 117, "-": 118, "play": 200, "##ing": 201,
		"中": 704, "义": 721, "文": 3152, "测": 3844, "试": 6407, "语": 6427,
		"world": 8572, "hello": 8701,
	} {
		set(token, id)
	}
	profile := map[string]any{
		"version": "1.0", "truncation": nil, "padding": nil,
		"added_tokens": []any{
			map[string]any{"id": 0, "content": "[PAD]", "single_word": false, "lstrip": false, "rstrip": false, "normalized": false, "special": true},
			map[string]any{"id": 100, "content": "[UNK]", "single_word": false, "lstrip": false, "rstrip": false, "normalized": false, "special": true},
			map[string]any{"id": 101, "content": "[CLS]", "single_word": false, "lstrip": false, "rstrip": false, "normalized": false, "special": true},
			map[string]any{"id": 102, "content": "[SEP]", "single_word": false, "lstrip": false, "rstrip": false, "normalized": false, "special": true},
			map[string]any{"id": 103, "content": "[MASK]", "single_word": false, "lstrip": false, "rstrip": false, "normalized": false, "special": true},
		},
		"normalizer":    map[string]any{"type": "BertNormalizer", "clean_text": true, "handle_chinese_chars": true, "strip_accents": nil, "lowercase": false},
		"pre_tokenizer": map[string]any{"type": "BertPreTokenizer"},
		"post_processor": map[string]any{
			"type": "TemplateProcessing",
			"single": []any{
				map[string]any{"SpecialToken": map[string]any{"id": "[CLS]", "type_id": 0}},
				map[string]any{"Sequence": map[string]any{"id": "A", "type_id": 0}},
				map[string]any{"SpecialToken": map[string]any{"id": "[SEP]", "type_id": 0}},
			},
			"special_tokens": map[string]any{
				"[CLS]": map[string]any{"id": "[CLS]", "ids": []int{101}, "tokens": []string{"[CLS]"}},
				"[SEP]": map[string]any{"id": "[SEP]", "ids": []int{102}, "tokens": []string{"[SEP]"}},
			},
		},
		"decoder": map[string]any{"type": "WordPiece", "prefix": "##", "cleanup": true},
		"model":   map[string]any{"type": "WordPiece", "vocab": vocab, "unk_token": "[UNK]", "continuing_subword_prefix": "##", "max_input_chars_per_word": 100},
	}
	payload, err := json.Marshal(profile)
	if err != nil {
		t.Fatal(err)
	}
	return payload
}
