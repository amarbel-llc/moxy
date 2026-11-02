//go:build linux

package toolbox

import (
	"os"
	"syscall"
	"unsafe"
)

// sysOpenat2 is openat2(2)'s number, the same on every Linux architecture
// (it postdates the per-architecture tables). Go's syscall package does not
// name it.
const sysOpenat2 = 437

const resolveNoSymlinks = 0x04 // RESOLVE_NO_SYMLINKS

// atFdcwd is AT_FDCWD; a variable, since a negative constant cannot convert
// to the uintptr a raw syscall takes.
var atFdcwd = -100

type openHow struct {
	flags   uint64
	mode    uint64
	resolve uint64
}

// openNoSymlinks opens path read-only and fails if resolving it would follow
// any symlink, the leaf or a component above it. The file opened is therefore
// exactly the path the gate judged, even if a component was swapped for a
// link after the verdict.
func openNoSymlinks(path string) (*os.File, error) {
	p, err := syscall.BytePtrFromString(path)
	if err != nil {
		return nil, err
	}
	how := openHow{flags: uint64(syscall.O_RDONLY | syscall.O_CLOEXEC), resolve: resolveNoSymlinks}
	for {
		fd, _, errno := syscall.Syscall6(sysOpenat2,
			uintptr(atFdcwd), uintptr(unsafe.Pointer(p)),
			uintptr(unsafe.Pointer(&how)), unsafe.Sizeof(how), 0, 0)
		switch errno {
		case 0:
			return os.NewFile(fd, path), nil
		case syscall.EINTR, syscall.EAGAIN:
			continue
		}
		return nil, &os.PathError{Op: "open", Path: path, Err: errno}
	}
}
