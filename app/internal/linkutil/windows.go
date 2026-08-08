//go:build windows

package linkutil

import (
	"encoding/binary"
	"syscall"
	"unicode/utf16"

	"golang.org/x/sys/windows"
)

const (
	fsctlGetReparsePoint    = 0x900A8
	ioReparseTagMountPoint  = 0xA0000003
	maxReparseBufferSize    = 16 * 1024
)

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
