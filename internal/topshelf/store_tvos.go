//go:build ios && tvos && arm64

package topshelf

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
)

const (
	appGroupIdentifier   = "group.dev.yuchanns.MikuTV"
	keychainService      = "dev.yuchanns.MikuTV.topshelf"
	keychainAccount      = "snapshot.v1"
	keychainProbeService = "dev.yuchanns.MikuTV.topshelf.probe"
	keychainProbeAccount = "prefix"
	keychainItemNotFound = -25300
	cfStringUTF8         = 0x08000100
)

type SharedStore struct {
	mu          sync.Mutex
	appGroupDir string
	accessGroup string

	add    func(uintptr, *uintptr) int32
	copy   func(uintptr, *uintptr) int32
	update func(uintptr, uintptr) int32
	delete func(uintptr) int32

	dictionary       func(uintptr, int, uintptr, uintptr) uintptr
	put              func(uintptr, uintptr, uintptr)
	dictGet          func(uintptr, uintptr) uintptr
	cfString         func(uintptr, *byte, int, uint32, bool) uintptr
	cfStringGetCStr  func(uintptr, *byte, int, uint32) bool
	cfData           func(uintptr, *byte, int) uintptr
	cfDataLen        func(uintptr) int
	cfDataBytes      func(uintptr) *byte
	cfRelease        func(uintptr)
	keyCallbacks     uintptr
	valueCallbacks   uintptr
	classKey         uintptr
	genericPassword  uintptr
	serviceKey       uintptr
	accountKey       uintptr
	accessGroupKey   uintptr
	dataKey          uintptr
	returnDataKey    uintptr
	returnAttrsKey   uintptr
	booleanTrue      uintptr
	accessibleKey    uintptr
	afterFirstUnlock uintptr

	tvServicesOnce sync.Once
	tvServicesOK   bool
}

func NewSharedStore() (*SharedStore, error) {
	security, err := purego.Dlopen("/System/Library/Frameworks/Security.framework/Security", purego.RTLD_LAZY|purego.RTLD_LOCAL)
	if err != nil {
		return nil, err
	}
	foundation, err := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", purego.RTLD_LAZY|purego.RTLD_LOCAL)
	if err != nil {
		return nil, err
	}
	s := &SharedStore{}
	for _, sym := range []struct {
		lib    uintptr
		name   string
		target any
	}{
		{security, "SecItemAdd", &s.add},
		{security, "SecItemCopyMatching", &s.copy},
		{security, "SecItemUpdate", &s.update},
		{security, "SecItemDelete", &s.delete},
		{foundation, "CFDictionaryCreateMutable", &s.dictionary},
		{foundation, "CFDictionarySetValue", &s.put},
		{foundation, "CFDictionaryGetValue", &s.dictGet},
		{foundation, "CFStringCreateWithBytes", &s.cfString},
		{foundation, "CFStringGetCString", &s.cfStringGetCStr},
		{foundation, "CFDataCreate", &s.cfData},
		{foundation, "CFDataGetLength", &s.cfDataLen},
		{foundation, "CFDataGetBytePtr", &s.cfDataBytes},
		{foundation, "CFRelease", &s.cfRelease},
	} {
		addr, err := purego.Dlsym(sym.lib, sym.name)
		if err != nil {
			return nil, err
		}
		purego.RegisterFunc(sym.target, addr)
	}
	for _, sym := range []struct {
		lib    uintptr
		name   string
		target *uintptr
	}{
		{security, "kSecClass", &s.classKey},
		{security, "kSecClassGenericPassword", &s.genericPassword},
		{security, "kSecAttrService", &s.serviceKey},
		{security, "kSecAttrAccount", &s.accountKey},
		{security, "kSecAttrAccessGroup", &s.accessGroupKey},
		{security, "kSecValueData", &s.dataKey},
		{security, "kSecReturnData", &s.returnDataKey},
		{security, "kSecReturnAttributes", &s.returnAttrsKey},
		{security, "kSecAttrAccessible", &s.accessibleKey},
		{security, "kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly", &s.afterFirstUnlock},
		{foundation, "kCFBooleanTrue", &s.booleanTrue},
	} {
		addr, err := purego.Dlsym(sym.lib, sym.name)
		if err != nil {
			return nil, err
		}
		*sym.target = *(*uintptr)(unsafe.Pointer(addr))
	}
	if s.keyCallbacks, err = purego.Dlsym(foundation, "kCFTypeDictionaryKeyCallBacks"); err != nil {
		return nil, err
	}
	if s.valueCallbacks, err = purego.Dlsym(foundation, "kCFTypeDictionaryValueCallBacks"); err != nil {
		return nil, err
	}
	s.appGroupDir = detectAppGroupContainer()
	s.accessGroup = s.detectSharedAccessGroup()
	return s, nil
}

func (s *SharedStore) Mode() string {
	if s.appGroupDir != "" {
		return "app-group:" + appGroupIdentifier
	}
	if s.accessGroup != "" {
		return "keychain-group:" + s.accessGroup
	}
	return "keychain-default"
}

func (s *SharedStore) Load() (Snapshot, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.appGroupDir != "" {
		data, err := os.ReadFile(filepath.Join(s.appGroupDir, "Library", "Caches", "topshelf.json"))
		if err == nil {
			return Decode(data)
		}
		if !os.IsNotExist(err) {
			return Snapshot{}, err
		}
	}
	data, err := s.readKeychain()
	if err != nil {
		return Snapshot{}, err
	}
	return Decode(data)
}

func (s *SharedStore) Save(snapshot Snapshot) error {
	encoded, err := Encode(snapshot)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.appGroupDir != "" {
		dir := filepath.Join(s.appGroupDir, "Library", "Caches")
		if err := os.MkdirAll(dir, 0o755); err == nil {
			target := filepath.Join(dir, "topshelf.json")
			tmp := target + ".tmp"
			if err := os.WriteFile(tmp, encoded, 0o600); err == nil {
				if err := os.Rename(tmp, target); err == nil {
					_ = s.writeKeychain(encoded)
					return nil
				}
				_ = os.Remove(tmp)
			}
		}
	}
	return s.writeKeychain(encoded)
}

func (s *SharedStore) NotifyChanged() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	pool := objc.ID(objc.GetClass("NSAutoreleasePool")).Send(objc.RegisterName("new"))
	defer pool.Send(objc.RegisterName("drain"))

	s.tvServicesOnce.Do(func() {
		if _, err := purego.Dlopen("/System/Library/Frameworks/TVServices.framework/TVServices", purego.RTLD_GLOBAL|purego.RTLD_NOW); err == nil {
			s.tvServicesOK = true
		}
	})
	if !s.tvServicesOK {
		return
	}
	cls := objc.GetClass("TVTopShelfContentProvider")
	if cls == 0 {
		return
	}
	objc.ID(cls).Send(objc.RegisterName("topShelfContentDidChange"))
}

func detectAppGroupContainer() string {
	var dir string
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	pool := objc.ID(objc.GetClass("NSAutoreleasePool")).Send(objc.RegisterName("new"))
	defer pool.Send(objc.RegisterName("drain"))
	fmCls := objc.GetClass("NSFileManager")
	if fmCls == 0 {
		return ""
	}
	fm := objc.ID(fmCls).Send(objc.RegisterName("defaultManager"))
	if fm == 0 {
		return ""
	}
	groupID := objc.ID(objc.GetClass("NSString")).Send(objc.RegisterName("stringWithUTF8String:"), appGroupIdentifier)
	if groupID == 0 {
		return ""
	}
	container := fm.Send(objc.RegisterName("containerURLForSecurityApplicationGroupIdentifier:"), groupID)
	if container == 0 {
		return ""
	}
	pathObj := container.Send(objc.RegisterName("path"))
	if pathObj == 0 {
		return ""
	}
	dir = strings.TrimSpace(objc.Send[string](pathObj, objc.RegisterName("UTF8String")))
	return dir
}

func (s *SharedStore) detectSharedAccessGroup() string {
	query := s.baseQuery(keychainProbeService, keychainProbeAccount, "")
	defer s.cfRelease(query)
	s.put(query, s.returnAttrsKey, s.booleanTrue)
	var item uintptr
	status := s.copy(query, &item)
	if status == keychainItemNotFound {
		addQuery := s.baseQuery(keychainProbeService, keychainProbeAccount, "")
		defer s.cfRelease(addQuery)
		val := s.cfData(0, unsafe.StringData("1"), 1)
		defer s.cfRelease(val)
		s.put(addQuery, s.accessibleKey, s.afterFirstUnlock)
		s.put(addQuery, s.dataKey, val)
		s.put(addQuery, s.returnAttrsKey, s.booleanTrue)
		status = s.add(addQuery, &item)
	}
	if status != 0 || item == 0 {
		return ""
	}
	defer s.cfRelease(item)
	groupVal := s.dictGet(item, s.accessGroupKey)
	if groupVal == 0 {
		return ""
	}
	var buf [256]byte
	if !s.cfStringGetCStr(groupVal, &buf[0], len(buf), cfStringUTF8) {
		return ""
	}
	raw := string(buf[:bytes.IndexByte(buf[:], 0)])
	return selectSharedAccessGroup(raw, s.canAccessKeychainGroup)
}

func (s *SharedStore) canAccessKeychainGroup(accessGroup string) bool {
	query := s.baseQuery(keychainService, keychainAccount, accessGroup)
	defer s.cfRelease(query)
	s.put(query, s.returnAttrsKey, s.booleanTrue)
	var item uintptr
	status := s.copy(query, &item)
	if status == 0 && item != 0 {
		s.cfRelease(item)
	}
	return status == 0 || status == keychainItemNotFound
}

func (s *SharedStore) readKeychain() ([]byte, error) {
	query := s.baseQuery(keychainService, keychainAccount, s.accessGroup)
	defer s.cfRelease(query)
	s.put(query, s.returnDataKey, s.booleanTrue)
	var item uintptr
	switch status := s.copy(query, &item); status {
	case keychainItemNotFound:
		return nil, nil
	case 0:
		defer s.cfRelease(item)
		return bytes.Clone(unsafe.Slice(s.cfDataBytes(item), s.cfDataLen(item))), nil
	default:
		return nil, fmt.Errorf("read topshelf keychain (%s): OSStatus %d", s.accessGroup, status)
	}
}

func (s *SharedStore) writeKeychain(data []byte) error {
	query := s.baseQuery(keychainService, keychainAccount, s.accessGroup)
	defer s.cfRelease(query)
	value := s.cfData(0, unsafe.SliceData(data), len(data))
	defer s.cfRelease(value)
	attrs := s.newDictionary()
	defer s.cfRelease(attrs)
	s.put(attrs, s.dataKey, value)
	status := s.update(query, attrs)
	if status == keychainItemNotFound {
		item := s.baseQuery(keychainService, keychainAccount, s.accessGroup)
		defer s.cfRelease(item)
		s.put(item, s.accessibleKey, s.afterFirstUnlock)
		s.put(item, s.dataKey, value)
		status = s.add(item, nil)
	}
	if status != 0 {
		return fmt.Errorf("save topshelf keychain (%s): OSStatus %d", s.accessGroup, status)
	}
	return nil
}

func (s *SharedStore) newDictionary() uintptr {
	return s.dictionary(0, 0, s.keyCallbacks, s.valueCallbacks)
}

func (s *SharedStore) baseQuery(service, account, accessGroup string) uintptr {
	serviceName := s.cfString(0, unsafe.StringData(service), len(service), cfStringUTF8, false)
	defer s.cfRelease(serviceName)
	accountName := s.cfString(0, unsafe.StringData(account), len(account), cfStringUTF8, false)
	defer s.cfRelease(accountName)
	q := s.newDictionary()
	s.put(q, s.classKey, s.genericPassword)
	s.put(q, s.serviceKey, serviceName)
	s.put(q, s.accountKey, accountName)
	if accessGroup != "" {
		groupName := s.cfString(0, unsafe.StringData(accessGroup), len(accessGroup), cfStringUTF8, false)
		defer s.cfRelease(groupName)
		s.put(q, s.accessGroupKey, groupName)
	}
	return q
}
