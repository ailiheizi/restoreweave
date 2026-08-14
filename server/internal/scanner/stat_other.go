//go:build dragonfly || freebsd || netbsd || openbsd

package scanner

import (
	"io/fs"
	"os"
	"reflect"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

// statRelative returns no-follow metadata for relPath via fstatat with
// AT_SYMLINK_NOFOLLOW.
//
// Weaker than the Linux and darwin paths: these platforms expose neither
// O_PATH nor O_SYMLINK, so the result is a hand-built FileInfo whose Sys()
// value is a converted syscall.Stat_t. os.SameFile rejects it, which degrades
// the scanner's sameSnapshot stability comparison to false, so rooted scans on
// these platforms will conservatively mark entries UNSTABLE. The metadata
// facts themselves (device, inode, mode, times) remain accurate, and all
// escape rejections still fail closed.
func statRelative(rootFd int, relPath string) (fs.FileInfo, error) {
	parentFd, name, err := resolveParent(rootFd, relPath)
	if err != nil {
		return nil, err
	}
	defer unix.Close(parentFd)

	var stat unix.Stat_t
	if err := unix.Fstatat(parentFd, name, &stat, unix.AT_SYMLINK_NOFOLLOW); err != nil {
		return nil, os.NewSyscallError("fstatat", err)
	}
	converted, err := convertStat(stat)
	if err != nil {
		return nil, err
	}
	return &bsdStatFileInfo{name: name, stat: converted}, nil
}

type bsdStatFileInfo struct {
	name string
	stat syscall.Stat_t
}

func (info *bsdStatFileInfo) Name() string       { return info.name }
func (info *bsdStatFileInfo) Size() int64        { return info.stat.Size }
func (info *bsdStatFileInfo) Mode() fs.FileMode  { return modeFromNative(uint32(info.stat.Mode)) }
func (info *bsdStatFileInfo) ModTime() time.Time { return modTimeFromStat(info.stat) }
func (info *bsdStatFileInfo) IsDir() bool        { return info.Mode().IsDir() }
func (info *bsdStatFileInfo) Sys() any           { return &info.stat }

func modTimeFromStat(stat syscall.Stat_t) time.Time {
	value := reflect.ValueOf(&stat).Elem()
	for _, name := range []string{"Mtim", "Mtimespec", "Ctimespec"} {
		field := value.FieldByName(name)
		if !field.IsValid() {
			continue
		}
		timespec, ok := field.Interface().(syscall.Timespec)
		if !ok {
			continue
		}
		return time.Unix(timespec.Sec, timespec.Nsec)
	}
	return time.Time{}
}

// modeFromNative mirrors the standard-library conversion of a native st_mode
// to fs.FileMode so that Kind detection and IsDir behave identically.
func modeFromNative(mode uint32) fs.FileMode {
	result := fs.FileMode(mode & 0777)
	switch mode & syscall.S_IFMT {
	case syscall.S_IFREG:
	case syscall.S_IFDIR:
		result |= fs.ModeDir
	case syscall.S_IFBLK:
		result |= fs.ModeDevice
	case syscall.S_IFCHR:
		result |= fs.ModeDevice | fs.ModeCharDevice
	case syscall.S_IFLNK:
		result |= fs.ModeSymlink
	case syscall.S_IFIFO:
		result |= fs.ModeNamedPipe
	case syscall.S_IFSOCK:
		result |= fs.ModeSocket
	default:
		result |= fs.ModeIrregular
	}
	if mode&syscall.S_ISUID != 0 {
		result |= fs.ModeSetuid
	}
	if mode&syscall.S_ISGID != 0 {
		result |= fs.ModeSetgid
	}
	if mode&syscall.S_ISVTX != 0 {
		result |= fs.ModeSticky
	}
	return result
}

// statFieldAliases maps unix.Stat_t names to the equivalent syscall.Stat_t
// names: the BSDs name the timespec fields differently in the two packages.
var statFieldAliases = map[string]string{
	"Atim": "Atimespec",
	"Mtim": "Mtimespec",
	"Ctim": "Ctimespec",
	"Btim": "Birthtimespec",
}

// convertStat copies the structurally identical but distinct unix.Stat_t into
// the syscall.Stat_t that the rest of the scanner expects. Field names are
// shared across the BSDs (Dev, Mode, Ino, ...) apart from the timespec
// aliases, so a reflective same-name copy with numeric and nested-struct
// coercion is portable.
func convertStat(source unix.Stat_t) (syscall.Stat_t, error) {
	var target syscall.Stat_t
	if err := copyStat(reflect.ValueOf(&source).Elem(), reflect.ValueOf(&target).Elem()); err != nil {
		return target, err
	}
	return target, nil
}

func copyStat(source, target reflect.Value) error {
	for index := 0; index < target.NumField(); index++ {
		targetField := target.Type().Field(index)
		if targetField.PkgPath != "" {
			continue
		}
		sourceField := source.FieldByName(targetField.Name)
		if !sourceField.IsValid() {
			if alias, ok := statFieldAliases[targetField.Name]; ok {
				sourceField = source.FieldByName(alias)
			}
		}
		if !sourceField.IsValid() {
			continue
		}
		if err := copyStatField(target.Field(index), sourceField); err != nil {
			return err
		}
	}
	return nil
}

func copyStatField(target, source reflect.Value) error {
	switch {
	case source.Type().AssignableTo(target.Type()):
		target.Set(source)
	case target.Kind() == reflect.Struct && source.Kind() == reflect.Struct:
		return copyStat(source, target)
	case target.Kind() == reflect.Int64:
		switch source.Kind() {
		case reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			target.SetInt(source.Int())
		case reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			target.SetInt(int64(source.Uint()))
		}
	case target.Kind() == reflect.Uint64:
		switch source.Kind() {
		case reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
			target.SetUint(uint64(source.Int()))
		case reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
			target.SetUint(source.Uint())
		}
	}
	return nil
}
