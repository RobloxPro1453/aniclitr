package pathx

import (
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

func userPath() (string, uint32, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Environment`, registry.QUERY_VALUE)
	if err != nil {
		return "", registry.EXPAND_SZ, err
	}
	defer k.Close()
	v, typ, err := k.GetStringValue("Path")
	if err == registry.ErrNotExist {
		return "", registry.EXPAND_SZ, nil
	}
	return v, typ, err
}

func setUserPath(v string, typ uint32) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Environment`, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if typ == registry.SZ {
		err = k.SetStringValue("Path", v)
	} else {
		err = k.SetExpandStringValue("Path", v)
	}
	if err == nil {
		announce()
	}
	return err
}

// announce tells Explorer (and so new terminals) that the environment
// changed; without it only new logons see the new PATH.
func announce() {
	env, _ := syscall.UTF16PtrFromString("Environment")
	user32 := windows.NewLazySystemDLL("user32.dll")
	send := user32.NewProc("SendMessageTimeoutW")
	const hwndBroadcast, wmSettingChange, smtoAbortIfHung = 0xffff, 0x001A, 0x0002
	var res uintptr
	send.Call(hwndBroadcast, wmSettingChange, 0, uintptr(unsafe.Pointer(env)), smtoAbortIfHung, 3000, uintptr(unsafe.Pointer(&res)))
}

func has(dir string) bool {
	v, _, err := userPath()
	if err != nil {
		return false
	}
	want := strings.ToLower(clean(dir))
	for _, e := range split(v) {
		if strings.ToLower(clean(e)) == want {
			return true
		}
	}
	return false
}

func add(dir string) error {
	v, typ, err := userPath()
	if err != nil {
		return err
	}
	if has(dir) {
		return nil
	}
	if v != "" && !strings.HasSuffix(v, ";") {
		v += ";"
	}
	return setUserPath(v+dir, typ)
}

func remove(dir string) error {
	v, typ, err := userPath()
	if err != nil {
		return err
	}
	want := strings.ToLower(clean(dir))
	var keep []string
	for _, e := range split(v) {
		if strings.ToLower(clean(e)) != want {
			keep = append(keep, e)
		}
	}
	return setUserPath(strings.Join(keep, ";"), typ)
}
