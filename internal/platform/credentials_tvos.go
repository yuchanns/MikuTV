//go:build ios && tvos && arm64

package platform

import (
	"bytes"
	"fmt"
	"unsafe"

	"github.com/ebitengine/purego"
)

const keychainItemNotFound = -25300

type CredentialKeychain struct {
	add    func(uintptr, *uintptr) int32
	copy   func(uintptr, *uintptr) int32
	update func(uintptr, uintptr) int32
	delete func(uintptr) int32

	dictionary func(uintptr, int, uintptr, uintptr) uintptr
	put        func(uintptr, uintptr, uintptr)
	string     func(uintptr, *byte, int, uint32, bool) uintptr
	data       func(uintptr, *byte, int) uintptr
	length     func(uintptr) int
	dataBytes  func(uintptr) *byte
	release    func(uintptr)

	keyCallbacks, valueCallbacks                           uintptr
	class, genericPassword, service, account, dataKey      uintptr
	returnData, booleanTrue, accessible, deviceAfterUnlock uintptr

	store isolatedCredentialStore
}

func NewCredentialKeychain() (*CredentialKeychain, error) {
	security, err := purego.Dlopen("/System/Library/Frameworks/Security.framework/Security", purego.RTLD_LAZY|purego.RTLD_LOCAL)
	if err != nil {
		return nil, err
	}
	foundation, err := purego.Dlopen("/System/Library/Frameworks/CoreFoundation.framework/CoreFoundation", purego.RTLD_LAZY|purego.RTLD_LOCAL)
	if err != nil {
		return nil, err
	}
	k := &CredentialKeychain{}
	for _, symbol := range []struct {
		library uintptr
		name    string
		target  any
	}{
		{security, "SecItemAdd", &k.add}, {security, "SecItemCopyMatching", &k.copy},
		{security, "SecItemUpdate", &k.update}, {security, "SecItemDelete", &k.delete},
		{foundation, "CFDictionaryCreateMutable", &k.dictionary}, {foundation, "CFDictionarySetValue", &k.put},
		{foundation, "CFStringCreateWithBytes", &k.string}, {foundation, "CFDataCreate", &k.data},
		{foundation, "CFDataGetLength", &k.length}, {foundation, "CFDataGetBytePtr", &k.dataBytes},
		{foundation, "CFRelease", &k.release},
	} {
		address, err := purego.Dlsym(symbol.library, symbol.name)
		if err != nil {
			return nil, err
		}
		purego.RegisterFunc(symbol.target, address)
	}
	for _, symbol := range []struct {
		library uintptr
		name    string
		target  *uintptr
	}{
		{security, "kSecClass", &k.class}, {security, "kSecClassGenericPassword", &k.genericPassword},
		{security, "kSecAttrService", &k.service}, {security, "kSecAttrAccount", &k.account},
		{security, "kSecValueData", &k.dataKey}, {security, "kSecReturnData", &k.returnData},
		{security, "kSecAttrAccessible", &k.accessible}, {security, "kSecAttrAccessibleAfterFirstUnlockThisDeviceOnly", &k.deviceAfterUnlock},
		{foundation, "kCFBooleanTrue", &k.booleanTrue},
	} {
		address, err := purego.Dlsym(symbol.library, symbol.name)
		if err != nil {
			return nil, err
		}
		*symbol.target = *(*uintptr)(unsafe.Pointer(address))
	}
	k.keyCallbacks, err = purego.Dlsym(foundation, "kCFTypeDictionaryKeyCallBacks")
	if err != nil {
		return nil, err
	}
	k.valueCallbacks, err = purego.Dlsym(foundation, "kCFTypeDictionaryValueCallBacks")
	if err != nil {
		return nil, err
	}
	k.store = isolatedCredentialStore{backend: k}
	return k, nil
}

func (k *CredentialKeychain) Load(pluginID, entryID string) ([]byte, error) {
	return k.store.Load(pluginID, entryID)
}

func (k *CredentialKeychain) Save(pluginID, entryID string, data []byte) error {
	return k.store.Save(pluginID, entryID, data)
}

func (k *CredentialKeychain) get(service, account string) ([]byte, error) {
	query := k.query(service, account)
	defer k.release(query)
	k.put(query, k.returnData, k.booleanTrue)
	var item uintptr
	switch status := k.copy(query, &item); status {
	case keychainItemNotFound:
		return nil, nil
	case 0:
		defer k.release(item)
		return bytes.Clone(unsafe.Slice(k.dataBytes(item), k.length(item))), nil
	default:
		return nil, fmt.Errorf("read credential item: OSStatus %d", status)
	}
}

func (k *CredentialKeychain) set(service, account string, data []byte) error {
	query := k.query(service, account)
	defer k.release(query)
	value := k.data(0, unsafe.SliceData(data), len(data))
	defer k.release(value)
	attributes := k.newDictionary()
	defer k.release(attributes)
	k.put(attributes, k.dataKey, value)
	status := k.update(query, attributes)
	if status == keychainItemNotFound {
		item := k.query(service, account)
		defer k.release(item)
		k.put(item, k.accessible, k.deviceAfterUnlock)
		k.put(item, k.dataKey, value)
		status = k.add(item, nil)
	}
	if status != 0 {
		return fmt.Errorf("save credential item: OSStatus %d", status)
	}
	return nil
}

func (k *CredentialKeychain) del(service, account string) error {
	query := k.query(service, account)
	defer k.release(query)
	status := k.delete(query)
	if status == 0 || status == keychainItemNotFound {
		return nil
	}
	return fmt.Errorf("delete credential item: OSStatus %d", status)
}

func (k *CredentialKeychain) newDictionary() uintptr {
	return k.dictionary(0, 0, k.keyCallbacks, k.valueCallbacks)
}

func (k *CredentialKeychain) query(service, account string) uintptr {
	const utf8 = 0x08000100
	serviceName := k.string(0, unsafe.StringData(service), len(service), utf8, false)
	defer k.release(serviceName)
	accountName := k.string(0, unsafe.StringData(account), len(account), utf8, false)
	defer k.release(accountName)
	query := k.newDictionary()
	k.put(query, k.class, k.genericPassword)
	k.put(query, k.service, serviceName)
	k.put(query, k.account, accountName)
	return query
}
