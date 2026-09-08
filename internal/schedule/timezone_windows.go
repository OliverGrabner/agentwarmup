package schedule

import (
	"fmt"
	"syscall"
	"unsafe"
)

func DetectTimezone() (string, error) {
	// GetDynamicTimeZoneInformation returns the stable Windows zone key, not a localized name.
	type dynamicZone struct {
		Bias         int32
		StandardName [32]uint16
		StandardDate syscall.Systemtime
		StandardBias int32
		DaylightName [32]uint16
		DaylightDate syscall.Systemtime
		DaylightBias int32
		KeyName      [128]uint16
		Disabled     byte
	}
	var z dynamicZone
	proc := syscall.NewLazyDLL("kernel32.dll").NewProc("GetDynamicTimeZoneInformation")
	result, _, err := proc.Call(uintptr(unsafe.Pointer(&z)))
	if result == 0xffffffff {
		return "", fmt.Errorf("detect timezone: %w", err)
	}
	if z.Disabled != 0 {
		return "", fmt.Errorf("automatic daylight saving is disabled; choose an IANA timezone")
	}
	key := syscall.UTF16ToString(z.KeyName[:])
	zone, ok := windowsZones[key]
	if !ok {
		return "", fmt.Errorf("unknown Windows timezone %q; choose an IANA timezone", key)
	}
	return zone, nil
}
