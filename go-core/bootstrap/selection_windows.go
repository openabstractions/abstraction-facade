package bootstrap

import (
	"context"
	"errors"
	"fmt"
	identity "github.com/openabstractions/abstraction-identity"
	"github.com/openabstractions/abstraction-identity/listen"
	"golang.org/x/sys/windows"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"unsafe"
)

const runtimeUpgradeCode = "{CEEF6550-5E44-4E83-8E18-A855C9D941CA}"
const maxRelatedProducts = 64

// MSI registration scopes: managed user, unmanaged user, machine.
var installContexts = [...]uint32{1, 2, 4}
var msiDLL = windows.NewLazySystemDLL("msi.dll")
var enumRelatedProc = msiDLL.NewProc("MsiEnumRelatedProductsW")
var productInfoProc = msiDLL.NewProc("MsiGetProductInfoExW")

type installationQueries struct {
	related  func(string, uint32) (string, error)
	property func(string, uint32, string) (string, error)
}

func relatedProduct(upgrade string, index uint32) (string, error) {
	code, err := windows.UTF16PtrFromString(upgrade)
	if err != nil {
		return "", err
	}
	var product [39]uint16
	result, _, _ := enumRelatedProc.Call(uintptr(unsafe.Pointer(code)), 0, uintptr(index), uintptr(unsafe.Pointer(&product[0])))
	if result != 0 {
		return "", syscall.Errno(result)
	}
	return windows.UTF16ToString(product[:]), nil
}

func installedProperty(product string, scope uint32, property string) (string, error) {
	code, err := windows.UTF16PtrFromString(product)
	if err != nil {
		return "", err
	}
	key, err := windows.UTF16PtrFromString(property)
	if err != nil {
		return "", err
	}
	// A single bounded read also refuses concurrent growth with ERROR_MORE_DATA.
	value := make([]uint16, 32768)
	size := uint32(len(value))
	result, _, _ := productInfoProc.Call(uintptr(unsafe.Pointer(code)), 0, uintptr(scope), uintptr(unsafe.Pointer(key)), uintptr(unsafe.Pointer(&value[0])), uintptr(unsafe.Pointer(&size)))
	if result != 0 {
		return "", syscall.Errno(result)
	}
	if size >= uint32(len(value)) {
		return "", errors.New("installation property exceeds bound")
	}
	return windows.UTF16ToString(value[:size+1]), nil
}

func selectInstalled(ctx context.Context) (Selection, error) {
	// MSI requires a single thread for successive related-product queries.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	for _, proc := range []*windows.LazyProc{enumRelatedProc, productInfoProc} {
		if err := proc.Find(); err != nil {
			return Selection{}, err
		}
	}
	thread, err := windows.GetCurrentThread()
	if err != nil {
		return Selection{}, err
	}
	var token windows.Token
	err = windows.OpenThreadToken(thread, windows.TOKEN_QUERY, true, &token)
	if err == nil {
		token.Close()
		return Selection{}, fmt.Errorf("%w: select outside thread impersonation", ErrNoTrustedInstallation)
	}
	if !errors.Is(err, windows.ERROR_NO_TOKEN) {
		return Selection{}, err
	}
	sid, err := currentUserSID()
	if err != nil {
		return Selection{}, err
	}
	endpointName := os.Getenv("ABSTRACTION_RUNTIME_ENDPOINT")
	if endpointName == "" {
		endpointName, err = Endpoint("runtime-v1")
		if err != nil {
			return Selection{}, err
		}
	}
	return selectWindowsInstallation(ctx, sid, endpointName, installationQueries{relatedProduct, installedProperty})
}

func selectWindowsInstallation(ctx context.Context, sid, endpointName string, q installationQueries) (Selection, error) {
	if _, err := windows.StringToSid(sid); err != nil {
		return Selection{}, fmt.Errorf("%w: invalid current principal", ErrNoTrustedInstallation)
	}
	var location string
	count := 0
	seen := map[string]bool{}
	for index := uint32(0); ; index++ {
		if err := ctx.Err(); err != nil {
			return Selection{}, err
		}
		if index >= maxRelatedProducts {
			return Selection{}, fmt.Errorf("%w: related product enumeration exceeds bound", ErrAmbiguousInstallation)
		}
		product, err := q.related(runtimeUpgradeCode, index)
		if errors.Is(err, windows.ERROR_NO_MORE_ITEMS) {
			break
		}
		if err != nil {
			return Selection{}, fmt.Errorf("enumerate registered product: %w", err)
		}
		if product == "" || seen[product] {
			return Selection{}, fmt.Errorf("%w: ambiguous product enumeration", ErrNoTrustedInstallation)
		}
		seen[product] = true
		for _, scope := range installContexts {
			if err := ctx.Err(); err != nil {
				return Selection{}, err
			}
			state, err := q.property(product, scope, "State")
			if errors.Is(err, windows.ERROR_UNKNOWN_PRODUCT) {
				continue
			}
			if err != nil {
				return Selection{}, fmt.Errorf("read registered product state: %w", err)
			}
			if state == "1" {
				continue
			} // Advertised payload supplies no installed image.
			if state != "5" {
				return Selection{}, fmt.Errorf("%w: unexpected product state", ErrNoTrustedInstallation)
			}
			dir, err := q.property(product, scope, "InstallLocation")
			if err != nil {
				return Selection{}, fmt.Errorf("read registered installation location: %w", err)
			}
			if !filepath.IsAbs(dir) || strings.ContainsRune(dir, 0) || strings.HasPrefix(dir, `\\`) {
				return Selection{}, fmt.Errorf("%w: installation location is not an absolute local path", ErrNoTrustedInstallation)
			}
			count++
			if count > 1 {
				return Selection{}, ErrAmbiguousInstallation
			}
			location = filepath.Join(dir, "tools", "openabstractions.exe")
		}
	}
	if count == 0 {
		return Selection{}, ErrNoTrustedInstallation
	}
	if err := ctx.Err(); err != nil {
		return Selection{}, err
	}
	return Selection{Endpoint: endpointName, Server: listen.ServerExpectation{Principal: identity.User{Kind: "windows", SID: sid, UID: -1, GID: -1}, Program: location}}, nil
}
