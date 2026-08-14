package processor

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"strconv"
	"strings"
	"unicode/utf8"
)

// AudioTags is a BYTE_DETERMINISTIC EXTRACT processor for ID3v2, FLAC
// Vorbis comments, and OGG Vorbis comments. It never opens paths and does
// not rewrite source bytes.
type AudioTags struct{}

func (AudioTags) CapabilityID() string { return CapabilityAudioTags }
func (AudioTags) Stage() Stage         { return StageExtract }

type audioTrack struct {
	Title      string `json:"title,omitempty"`
	Artist     string `json:"artist,omitempty"`
	Album      string `json:"album,omitempty"`
	Track      int    `json:"track,omitempty"`
	Year       string `json:"year,omitempty"`
	DurationMS int64  `json:"duration_ms,omitempty"`
	Container  string `json:"container,omitempty"`
}

func (AudioTags) RunStage(ctx context.Context, inv Invocation) (StageResult, error) {
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
	track, ok := parseAudioTags(body)
	if !ok {
		return StageResult{Status: StatusInapplicable, Reason: "no parseable audio tags"}, nil
	}
	payload, err := json.Marshal(track)
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
		SchemaRef:        SchemaRefAudioTrack(),
		MediaType:        MediaTypeAudioJSON,
		Sealed:           true,
	}, nil
}

func parseAudioTags(body []byte) (audioTrack, bool) {
	if track, ok := parseID3(body); ok {
		track.Container = "id3"
		return track, true
	}
	if track, ok := parseFLAC(body); ok {
		track.Container = "flac"
		return track, true
	}
	if track, ok := parseOGG(body); ok {
		track.Container = "ogg"
		return track, true
	}
	return audioTrack{}, false
}

func parseOGG(body []byte) (audioTrack, bool) {
	track := audioTrack{}
	for _, packet := range oggPackets(body) {
		if len(packet) < 7 || packet[0] != 3 || string(packet[1:7]) != "vorbis" {
			continue
		}
		parseVorbisComment(packet[7:], &track)
	}
	return track, track.Title != "" || track.Artist != "" || track.Album != ""
}

func oggPackets(body []byte) [][]byte {
	var packets [][]byte
	var current []byte
	offset := 0
	for offset+27 <= len(body) {
		if !bytes.Equal(body[offset:offset+4], []byte("OggS")) {
			offset++
			continue
		}
		nsegs := int(body[offset+26])
		table := offset + 27
		if nsegs < 0 || table+nsegs > len(body) {
			break
		}
		pos := table + nsegs
		if body[offset+5]&0x01 == 0 && len(current) > 0 {
			packets = append(packets, current)
			current = nil
		}
		for i := 0; i < nsegs; i++ {
			n := int(body[table+i])
			if pos+n > len(body) {
				return packets
			}
			current = append(current, body[pos:pos+n]...)
			pos += n
			if n < 255 {
				packets = append(packets, current)
				current = nil
			}
		}
		offset = pos
	}
	if len(current) > 0 {
		packets = append(packets, current)
	}
	return packets
}

func parseID3(body []byte) (audioTrack, bool) {
	if len(body) < 10 || !bytes.Equal(body[:3], []byte("ID3")) {
		return audioTrack{}, false
	}
	version := body[3]
	if version < 3 || version > 4 {
		return audioTrack{}, false
	}
	size := synchsafe(body[6:10])
	if size < 0 || 10+size > len(body) {
		size = len(body) - 10
	}
	payload := body[10 : 10+size]
	track := audioTrack{}
	for len(payload) >= 10 {
		id := string(payload[:4])
		var frameSize int
		if version == 4 {
			frameSize = synchsafe(payload[4:8])
		} else {
			frameSize = int(binary.BigEndian.Uint32(payload[4:8]))
		}
		if frameSize < 1 || 10+frameSize > len(payload) {
			break
		}
		data := payload[10 : 10+frameSize]
		payload = payload[10+frameSize:]
		if id[0] != 'T' {
			continue
		}
		text := decodeID3Text(data)
		if text == "" {
			continue
		}
		switch id {
		case "TIT2":
			track.Title = text
		case "TPE1":
			track.Artist = text
		case "TALB":
			track.Album = text
		case "TRCK":
			track.Track = parseTrackNumber(text)
		case "TYER", "TDRC":
			if len(text) >= 4 {
				track.Year = text[:4]
			} else {
				track.Year = text
			}
		}
	}
	return track, track.Title != "" || track.Artist != "" || track.Album != ""
}

func parseFLAC(body []byte) (audioTrack, bool) {
	if len(body) < 42 || !bytes.Equal(body[:4], []byte("fLaC")) {
		return audioTrack{}, false
	}
	offset := 4
	track := audioTrack{}
	for offset+4 <= len(body) {
		header := body[offset]
		length := int(body[offset+1])<<16 | int(body[offset+2])<<8 | int(body[offset+3])
		offset += 4
		if offset+length > len(body) {
			break
		}
		block := body[offset : offset+length]
		offset += length
		kind := header & 0x7f
		switch kind {
		case 0:
			if len(block) >= 18 {
				rate := (uint32(block[10])<<12 | uint32(block[11])<<4 | uint32(block[12])>>4) & 0xfffff
				samples := uint64(block[13]&0x0f)<<32 | uint64(block[14])<<24 | uint64(block[15])<<16 | uint64(block[16])<<8 | uint64(block[17])
				if rate > 0 && samples > 0 {
					track.DurationMS = int64(samples * 1000 / uint64(rate))
				}
			}
		case 4:
			parseVorbisComment(block, &track)
		}
		if header&0x80 != 0 {
			break
		}
	}
	return track, track.Title != "" || track.Artist != "" || track.Album != "" || track.DurationMS > 0
}

func parseVorbisComment(block []byte, track *audioTrack) {
	if len(block) < 4 {
		return
	}
	vendorLen := int(binary.LittleEndian.Uint32(block[:4]))
	offset := 4 + vendorLen
	if offset+4 > len(block) {
		return
	}
	count := int(binary.LittleEndian.Uint32(block[offset : offset+4]))
	offset += 4
	for i := 0; i < count && offset+4 <= len(block); i++ {
		n := int(binary.LittleEndian.Uint32(block[offset : offset+4]))
		offset += 4
		if n < 0 || offset+n > len(block) {
			return
		}
		entry := string(block[offset : offset+n])
		offset += n
		key, value, ok := strings.Cut(entry, "=")
		if !ok {
			continue
		}
		switch strings.ToUpper(key) {
		case "TITLE":
			track.Title = value
		case "ARTIST":
			track.Artist = value
		case "ALBUM":
			track.Album = value
		case "TRACKNUMBER":
			track.Track = parseTrackNumber(value)
		case "DATE":
			if len(value) >= 4 {
				track.Year = value[:4]
			} else {
				track.Year = value
			}
		}
	}
}

func decodeID3Text(data []byte) string {
	if len(data) == 0 {
		return ""
	}
	encoding, payload := data[0], data[1:]
	payload = bytes.TrimRight(payload, "\x00")
	switch encoding {
	case 0:
		return string(payload)
	case 3:
		if utf8.Valid(payload) {
			return string(payload)
		}
	case 1, 2:
		if len(payload) >= 2 && payload[0] == 0xff && payload[1] == 0xfe {
			payload = payload[2:]
			runes := make([]rune, 0, len(payload)/2)
			for i := 0; i+1 < len(payload); i += 2 {
				runes = append(runes, rune(binary.LittleEndian.Uint16(payload[i:i+2])))
			}
			return strings.TrimRight(string(runes), "\x00")
		}
	}
	if utf8.Valid(payload) {
		return string(payload)
	}
	return ""
}

func parseTrackNumber(value string) int {
	value, _, _ = strings.Cut(value, "/")
	n, _ := strconv.Atoi(strings.TrimSpace(value))
	if n < 0 {
		return 0
	}
	return n
}

func synchsafe(b []byte) int {
	if len(b) < 4 {
		return 0
	}
	return int(b[0]&0x7f)<<21 | int(b[1]&0x7f)<<14 | int(b[2]&0x7f)<<7 | int(b[3]&0x7f)
}
