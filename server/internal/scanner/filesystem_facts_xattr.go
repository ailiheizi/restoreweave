package scanner

import (
	"errors"
	"fmt"
	"os"
	"sort"
)

const (
	maxXAttrListBytes  = 16 << 20
	maxXAttrValueBytes = 16 << 20
)

type xattrListFunc func([]byte) (int, error)
type xattrValueFunc func(string, []byte) (int, error)
type xattrNameParser func([]byte) ([]string, error)

func captureXAttrs(list xattrListFunc, get xattrValueFunc, parse xattrNameParser) XAttrFacts {
	rawNames, err := readSizedXAttr(list, maxXAttrListBytes)
	if err != nil {
		return xattrFailureFacts(err)
	}
	names, err := parse(rawNames)
	if err != nil {
		return XAttrFacts{State: CaptureFactInconsistent, Attributes: []ExtendedAttribute{}, ReasonCode: "XATTR_NAME_LIST_INVALID"}
	}
	if len(names) == 0 {
		return XAttrFacts{State: CaptureFactObserved, Attributes: []ExtendedAttribute{}}
	}
	sort.Strings(names)
	attributes := make([]ExtendedAttribute, 0, len(names))
	for index, name := range names {
		if name == "" || (index > 0 && name == names[index-1]) {
			return XAttrFacts{State: CaptureFactInconsistent, Attributes: attributes, ReasonCode: "XATTR_NAME_LIST_INVALID"}
		}
		value, err := readSizedXAttr(func(buffer []byte) (int, error) {
			return get(name, buffer)
		}, maxXAttrValueBytes)
		if err != nil {
			return xattrFailureFacts(err)
		}
		attributes = append(attributes, ExtendedAttribute{Name: name, Value: value})
	}
	return XAttrFacts{State: CaptureFactObserved, Attributes: attributes}
}

func readSizedXAttr(read func([]byte) (int, error), max int) ([]byte, error) {
	size, err := read(nil)
	if err != nil {
		return nil, err
	}
	if size < 0 || size > max {
		return nil, fmt.Errorf("xattr size %d exceeds capture limit %d", size, max)
	}
	if size == 0 {
		return []byte{}, nil
	}
	buffer := make([]byte, size)
	for attempt := 0; attempt < 2; attempt++ {
		readSize, readErr := read(buffer)
		if readErr == nil {
			if readSize < 0 || readSize > len(buffer) {
				return nil, errors.New("xattr read returned an invalid size")
			}
			return append([]byte(nil), buffer[:readSize]...), nil
		}
		if attempt == 0 {
			newSize, sizeErr := read(nil)
			if sizeErr != nil || newSize <= len(buffer) || newSize > max {
				if sizeErr != nil {
					return nil, sizeErr
				}
				return nil, readErr
			}
			buffer = make([]byte, newSize)
			continue
		}
		return nil, readErr
	}
	return nil, errors.New("xattr read did not complete")
}

func xattrFailureFacts(err error) XAttrFacts {
	if isUnsupportedXAttrError(err) {
		return unsupportedXAttrFacts("XATTR_CAPABILITY_UNSUPPORTED")
	}
	if os.IsPermission(err) {
		return XAttrFacts{State: CaptureFactUnobserved, Attributes: []ExtendedAttribute{}, ReasonCode: "XATTR_PERMISSION_DENIED"}
	}
	return XAttrFacts{State: CaptureFactUnobserved, Attributes: []ExtendedAttribute{}, ReasonCode: "XATTR_CAPTURE_FAILED"}
}

func unsupportedACLFacts(reason string) ACLFacts {
	return ACLFacts{State: CaptureFactUnsupported, Records: []ACLRecord{}, ReasonCode: reason}
}

func unsupportedXAttrFacts(reason string) XAttrFacts {
	return XAttrFacts{State: CaptureFactUnsupported, Attributes: []ExtendedAttribute{}, ReasonCode: reason}
}

func parseNULXAttrNames(raw []byte) ([]string, error) {
	if len(raw) == 0 {
		return []string{}, nil
	}
	var names []string
	start := 0
	for index, value := range raw {
		if value != 0 {
			continue
		}
		if index == start {
			return nil, errors.New("empty xattr name")
		}
		names = append(names, string(raw[start:index]))
		start = index + 1
	}
	if start != len(raw) {
		return nil, errors.New("unterminated xattr name")
	}
	return names, nil
}
