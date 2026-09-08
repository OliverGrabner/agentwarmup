package config

import (
	"syscall"
	"time"
	"unsafe"
)

func replaceFile(source, destination string) error {
	from, err := syscall.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	to, err := syscall.UTF16PtrFromString(destination)
	if err != nil {
		return err
	}
	// MOVEFILE_REPLACE_EXISTING | MOVEFILE_WRITE_THROUGH.
	proc := syscall.NewLazyDLL("kernel32.dll").NewProc("MoveFileExW")
	for attempt := 0; ; attempt++ {
		result, _, callErr := proc.Call(uintptr(unsafe.Pointer(from)), uintptr(unsafe.Pointer(to)), uintptr(0x1|0x8))
		if result != 0 {
			return nil
		}
		// A simultaneous replacement or antivirus reader can briefly hold the
		// destination without delete sharing. Retry only these transient errors.
		if attempt >= 20 || (callErr != syscall.ERROR_ACCESS_DENIED && callErr != syscall.Errno(32)) {
			return callErr
		}
		time.Sleep(10 * time.Millisecond)
	}
}
