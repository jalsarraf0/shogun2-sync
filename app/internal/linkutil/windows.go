//go:build windows

package linkutil

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unicode/utf16"

	"golang.org/x/sys/windows"
)

const (
	fsctlGetReparsePoint   = 0x900A8
	fsctlSetReparsePoint   = 0x900A4
	ioReparseTagMountPoint = 0xA0000003
	maxReparseBufferSize   = 16 * 1024

	// The fixed part of a mount-point REPARSE_DATA_BUFFER: ReparseTag(4) +
	// ReparseDataLength(2) + Reserved(2) + SubstituteNameOffset(2) +
	// SubstituteNameLength(2) + PrintNameOffset(2) + PrintNameLength(2).
	// A mount point has no Flags field, which is also why its target can
	// never be relative — there's nowhere to record that it is.
	reparseHeaderSize = 16
)

// createJunction makes a directory junction at link pointing to target.
//
// This is deliberately not `cmd /c mklink /J`. cmd.exe re-parses its
// command line by rules that don't match the ones Go quotes arguments
// with, and for a path containing a percent sign there is no escaping that
// works at all — cmd expands %NAME% even inside double quotes. Since
// players pick their own cloud folder, that's a real path we'd corrupt.
// Talking to the filesystem directly also gives us typed errors instead
// of localised mklink output, and avoids inheriting whatever the user's
// Command Processor AutoRun key does.
//
// Junctions, unlike symlinks, need no Administrator rights or Developer
// Mode — which is the whole reason this app prefers them.
func createJunction(link, target string) error {
	abs, err := filepath.Abs(target)
	if err != nil {
		return fmt.Errorf("resolving %s: %w", target, err)
	}
	abs = filepath.Clean(abs)

	// The substitute name is an NT object-manager path; the print name is
	// the plain one Explorer and `dir` display.
	substitute, err := windows.UTF16FromString(`\??\` + abs)
	if err != nil {
		return err
	}
	printName, err := windows.UTF16FromString(abs)
	if err != nil {
		return err
	}

	// Offsets count from the start of PathBuffer and include each string's
	// terminator; the Length fields exclude it.
	subBytes := 2 * len(substitute)
	printBytes := 2 * len(printName)
	buf := make([]byte, reparseHeaderSize+subBytes+printBytes)
	if len(buf) > maxReparseBufferSize {
		return fmt.Errorf("target path is too long for a junction: %s", abs)
	}

	binary.LittleEndian.PutUint32(buf[0:], ioReparseTagMountPoint)
	binary.LittleEndian.PutUint16(buf[4:], uint16(8+subBytes+printBytes))
	binary.LittleEndian.PutUint16(buf[6:], 0) // Reserved
	binary.LittleEndian.PutUint16(buf[8:], 0) // SubstituteNameOffset
	binary.LittleEndian.PutUint16(buf[10:], uint16(subBytes-2))
	binary.LittleEndian.PutUint16(buf[12:], uint16(subBytes))
	binary.LittleEndian.PutUint16(buf[14:], uint16(printBytes-2))
	for i, c := range substitute {
		binary.LittleEndian.PutUint16(buf[reparseHeaderSize+2*i:], c)
	}
	for i, c := range printName {
		binary.LittleEndian.PutUint16(buf[reparseHeaderSize+subBytes+2*i:], c)
	}

	// The link has to exist, and be an empty directory, before it can be
	// turned into a reparse point.
	if err := os.Mkdir(link, 0o755); err != nil {
		return err
	}
	if err := setReparsePoint(link, buf); err != nil {
		// Don't leave a stray empty directory where the game expects its
		// saves — that would look like "all my saves vanished".
		_ = os.Remove(link)
		return err
	}
	return nil
}

func setReparsePoint(link string, buf []byte) error {
	p, err := windows.UTF16PtrFromString(link)
	if err != nil {
		return err
	}
	h, err := windows.CreateFile(
		p,
		windows.GENERIC_WRITE,
		0,
		nil,
		windows.OPEN_EXISTING,
		// BACKUP_SEMANTICS to get a handle to a directory at all,
		// OPEN_REPARSE_POINT to act on the link rather than through it.
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT,
		0,
	)
	if err != nil {
		return fmt.Errorf("opening %s: %w", link, err)
	}
	defer windows.CloseHandle(h)

	var returned uint32
	if err := windows.DeviceIoControl(h, fsctlSetReparsePoint,
		&buf[0], uint32(len(buf)), nil, 0, &returned, nil); err != nil {
		return fmt.Errorf("creating junction at %s: %w", link, err)
	}
	return nil
}

// junctionTarget reads the reparse point at path, if any, and returns its
// target for a directory junction (IO_REPARSE_TAG_MOUNT_POINT). Returns
// ok=false for anything else (a real directory, a symlink, or an error).
func junctionTarget(path string) (target string, ok bool) {
	p, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return "", false
	}

	h, err := windows.CreateFile(
		p,
		windows.GENERIC_READ,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil,
		windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS|windows.FILE_FLAG_OPEN_REPARSE_POINT,
		0,
	)
	if err != nil {
		return "", false
	}
	defer windows.CloseHandle(h)

	buf := make([]byte, maxReparseBufferSize)
	var bytesReturned uint32
	err = syscall.DeviceIoControl(
		syscall.Handle(h),
		fsctlGetReparsePoint,
		nil, 0,
		&buf[0], uint32(len(buf)),
		&bytesReturned, nil,
	)
	if err != nil {
		return "", false
	}
	if bytesReturned < 8 {
		return "", false
	}

	tag := binary.LittleEndian.Uint32(buf[0:4])
	if tag != ioReparseTagMountPoint {
		return "", false
	}

	// REPARSE_DATA_BUFFER layout for mount points:
	// ULONG ReparseTag; USHORT ReparseDataLength; USHORT Reserved;
	// USHORT SubstituteNameOffset; USHORT SubstituteNameLength;
	// USHORT PrintNameOffset; USHORT PrintNameLength;
	// WCHAR PathBuffer[1];
	const headerSize = 8
	const mountPointHeaderSize = 8
	substOffset := binary.LittleEndian.Uint16(buf[headerSize:])
	substLength := binary.LittleEndian.Uint16(buf[headerSize+2:])

	pathBufferStart := headerSize + mountPointHeaderSize
	start := pathBufferStart + int(substOffset)
	end := start + int(substLength)
	if end > len(buf) {
		return "", false
	}

	raw := buf[start:end]
	u16 := make([]uint16, len(raw)/2)
	for i := range u16 {
		u16[i] = binary.LittleEndian.Uint16(raw[i*2:])
	}
	s := string(utf16.Decode(u16))

	// Junction targets are stored as NT device paths, e.g. \??\C:\Foo.
	const prefix = `\??\`
	if len(s) >= len(prefix) && s[:len(prefix)] == prefix {
		s = s[len(prefix):]
	}
	return s, true
}
