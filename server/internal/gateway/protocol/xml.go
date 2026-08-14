package protocol

import (
	"encoding/xml"
	"fmt"
	"io"
	"sort"
)

func writeXMLTree(w io.Writer, extra map[string]any, status, codeMsg string, code int) error {
	start := xml.StartElement{
		Name: xml.Name{Space: "http://subsonic.org/restapi", Local: "subsonic-response"},
		Attr: []xml.Attr{
			{Name: xml.Name{Local: "status"}, Value: status},
			{Name: xml.Name{Local: "version"}, Value: subsonicVersion},
			{Name: xml.Name{Local: "type"}, Value: serverType},
			{Name: xml.Name{Local: "serverVersion"}, Value: serverVersion},
			{Name: xml.Name{Local: "openSubsonic"}, Value: "true"},
		},
	}
	enc := xml.NewEncoder(w)
	if err := enc.EncodeToken(start); err != nil {
		return err
	}
	if status != "ok" {
		errEl := xml.StartElement{
			Name: xml.Name{Local: "error"},
			Attr: []xml.Attr{
				{Name: xml.Name{Local: "code"}, Value: fmt.Sprint(code)},
				{Name: xml.Name{Local: "message"}, Value: codeMsg},
			},
		}
		if err := enc.EncodeToken(errEl); err != nil {
			return err
		}
		if err := enc.EncodeToken(errEl.End()); err != nil {
			return err
		}
	}
	keys := make([]string, 0, len(extra))
	for key := range extra {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if err := writeXMLValue(enc, key, extra[key]); err != nil {
			return err
		}
	}
	if err := enc.EncodeToken(start.End()); err != nil {
		return err
	}
	return enc.Flush()
}

func writeXMLValue(enc *xml.Encoder, name string, value any) error {
	switch typed := value.(type) {
	case nil:
		return nil
	case []any:
		for _, item := range typed {
			if err := writeXMLValue(enc, name, item); err != nil {
				return err
			}
		}
		return nil
	case map[string]any:
		start := xml.StartElement{Name: xml.Name{Local: name}}
		childKeys := make([]string, 0)
		for key, child := range typed {
			if isXMLAttr(child) {
				start.Attr = append(start.Attr, xml.Attr{
					Name:  xml.Name{Local: key},
					Value: fmt.Sprint(child),
				})
				continue
			}
			childKeys = append(childKeys, key)
		}
		sort.Slice(start.Attr, func(i, j int) bool { return start.Attr[i].Name.Local < start.Attr[j].Name.Local })
		sort.Strings(childKeys)
		if err := enc.EncodeToken(start); err != nil {
			return err
		}
		for _, key := range childKeys {
			if err := writeXMLValue(enc, key, typed[key]); err != nil {
				return err
			}
		}
		return enc.EncodeToken(start.End())
	default:
		start := xml.StartElement{Name: xml.Name{Local: name}}
		if err := enc.EncodeToken(start); err != nil {
			return err
		}
		if err := enc.EncodeToken(xml.CharData([]byte(fmt.Sprint(typed)))); err != nil {
			return err
		}
		return enc.EncodeToken(start.End())
	}
}

func isXMLAttr(value any) bool {
	switch value.(type) {
	case string, bool, int, int64, float64:
		return true
	default:
		return false
	}
}
