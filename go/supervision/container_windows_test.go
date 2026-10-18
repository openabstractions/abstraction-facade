package supervision

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	userenvTest       = windows.NewLazySystemDLL("userenv.dll")
	createProfileTest = userenvTest.NewProc("CreateAppContainerProfile")
	deleteProfileTest = userenvTest.NewProc("DeleteAppContainerProfile")
	deriveSidTest     = userenvTest.NewProc("DeriveAppContainerSidFromAppContainerName")
)

const containerSecurityProbeOptIn = "OA_FACADE_SANDBOX_SECURITY_TESTS"

// testContainer creates an AppContainer profile for this test alone and
// deletes it when the test ends; it returns the container's SID.
func testContainer(t *testing.T) *windows.SID {
	t.Helper()
	name := fmt.Sprintf("oa.test.supervision.%d", os.Getpid())
	n, _ := windows.UTF16PtrFromString(name)
	var sid *windows.SID
	hr, _, _ := createProfileTest.Call(uintptr(unsafe.Pointer(n)), uintptr(unsafe.Pointer(n)), uintptr(unsafe.Pointer(n)), 0, 0, uintptr(unsafe.Pointer(&sid)))
	if uint32(hr) == 0x800700B7 { // already exists: a crashed earlier run of this process id
		hr, _, _ = deriveSidTest.Call(uintptr(unsafe.Pointer(n)), uintptr(unsafe.Pointer(&sid)))
	}
	if hr != 0 {
		t.Fatalf("the test profile: HRESULT 0x%08X", uint32(hr))
	}
	copied, err := sid.Copy()
	if err != nil {
		t.Fatal(err)
	}
	windows.FreeSid(sid)
	t.Cleanup(func() {
		if hr, _, _ := deleteProfileTest.Call(uintptr(unsafe.Pointer(n))); hr != 0 {
			t.Errorf("delete the test profile: HRESULT 0x%08X", uint32(hr))
		}
	})
	return copied
}

// grantRead lets sid read and run what folder holds.
func grantRead(t *testing.T, folder string, sid *windows.SID) {
	t.Helper()
	sd, err := windows.GetNamedSecurityInfo(folder, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION)
	if err != nil {
		t.Fatal(err)
	}
	old, _, err := sd.DACL()
	if err != nil {
		t.Fatal(err)
	}
	acl, err := windows.ACLFromEntries([]windows.EXPLICIT_ACCESS{{
		AccessPermissions: windows.FILE_GENERIC_READ | windows.FILE_GENERIC_EXECUTE, AccessMode: windows.GRANT_ACCESS,
		Inheritance: windows.SUB_CONTAINERS_AND_OBJECTS_INHERIT,
		Trustee: windows.TRUSTEE{TrusteeForm: windows.TRUSTEE_IS_SID, TrusteeType: windows.TRUSTEE_IS_WELL_KNOWN_GROUP,
			TrusteeValue: windows.TrusteeValueFromSID(sid)},
	}}, old)
	if err != nil {
		t.Fatal(err)
	}
	if err := windows.SetNamedSecurityInfo(folder, windows.SE_FILE_OBJECT, windows.DACL_SECURITY_INFORMATION, nil, nil, acl, nil); err != nil {
		t.Fatal(err)
	}
}

// containedChild reports, from inside the container, whether its token is an
// AppContainer's, whether TCP reaches 1.1.1.1, whether the lent file's path
// opens, and what the lent handle it reads from stdin holds.
func containedChild() {
	var isAC uint32
	var n uint32
	tok := windows.GetCurrentProcessToken()
	_ = windows.GetTokenInformation(tok, 29 /* TokenIsAppContainer */, (*byte)(unsafe.Pointer(&isAC)), 4, &n)
	tcp := "open"
	if c, err := net.DialTimeout("tcp", "1.1.1.1:443", 3*time.Second); err != nil {
		tcp = "refused"
	} else {
		c.Close()
	}
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	fields := strings.Fields(line)
	path := "absent"
	if len(fields) == 2 {
		if f, err := os.Open(fields[1]); err == nil {
			path = "open"
			f.Close()
		} else {
			path = "denied"
		}
	}
	lent := "absent"
	if len(fields) >= 1 {
		if v, err := strconv.ParseUint(fields[0], 10, 64); err == nil {
			f := os.NewFile(uintptr(v), "lent")
			if data, err := io.ReadAll(f); err == nil {
				lent = string(data)
			}
			if _, err := f.Write([]byte("x")); err == nil {
				lent += "+writable"
			}
			f.Close()
		}
	}
	fmt.Printf("appcontainer=%d capabilities=%s tcp=%s path=%s lent=%s\n", isAC, tokenCapabilities(tok), tcp, path, lent)
	os.Exit(0)
}

// tokenCapabilities is the token's capability SIDs, comma-separated.
func tokenCapabilities(tok windows.Token) string {
	const tokenCapabilitiesClass = 30
	var n uint32
	_ = windows.GetTokenInformation(tok, tokenCapabilitiesClass, nil, 0, &n)
	if n == 0 {
		return "unreadable"
	}
	buf := make([]byte, n)
	if err := windows.GetTokenInformation(tok, tokenCapabilitiesClass, &buf[0], n, &n); err != nil {
		return "unreadable"
	}
	var sids []string
	for _, g := range (*windows.Tokengroups)(unsafe.Pointer(&buf[0])).AllGroups() {
		sids = append(sids, g.Sid.String())
	}
	return strings.Join(sids, ",")
}

// TestContainedTokenCarriesOneCapability: a contained child's token is an
// AppContainer's with ContainerCapabilitySID as its only capability. The
// child reads its own token and nothing else (FAC-P3).
func TestContainedTokenCarriesOneCapability(t *testing.T) {
	if os.Getenv("OA_SUPERVISION_TOKEN_CHILD") == "1" {
		var isAC, n uint32
		tok := windows.GetCurrentProcessToken()
		_ = windows.GetTokenInformation(tok, 29 /* TokenIsAppContainer */, (*byte)(unsafe.Pointer(&isAC)), 4, &n)
		fmt.Printf("appcontainer=%d capabilities=%s\n", isAC, tokenCapabilities(tok))
		os.Exit(0)
	}
	sid := testContainer(t)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(bin, "child.exe")
	if err := os.WriteFile(child, data, 0o700); err != nil {
		t.Fatal(err)
	}
	grantRead(t, bin, sid)
	outR, outW, _ := os.Pipe()
	defer outR.Close()
	p, err := Start(context.Background(), Options{
		Executable: child, Args: []string{"-test.run=^TestContainedTokenCarriesOneCapability$"}, Dir: bin,
		Env: append(os.Environ(), "OA_SUPERVISION_TOKEN_CHILD=1"), Stdout: outW,
		ForceTimeout: 5 * time.Second, Container: sid.String(),
	})
	outW.Close()
	if err != nil {
		t.Fatal(err)
	}
	report, _ := io.ReadAll(outR)
	if code, err := p.Wait(); err != nil || code != 0 {
		t.Fatalf("child: exit %d, %v (%s)", code, err, report)
	}
	if got := strings.TrimSpace(string(report)); got != "appcontainer=1 capabilities="+ContainerCapabilitySID {
		t.Fatalf("the contained child reported %q", got)
	}
}

var deriveCapabilityTest = windows.NewLazySystemDLL("kernelbase.dll").NewProc("DeriveCapabilitySidsFromName")

// TestContainerCapabilitySID: the capability every contained start carries
// is the one Windows derives from its name (FAC-P3).
func TestContainerCapabilitySID(t *testing.T) {
	if err := deriveCapabilityTest.Find(); err != nil {
		t.Skipf("DeriveCapabilitySidsFromName: %v", err)
	}
	name, _ := windows.UTF16PtrFromString(ContainerCapabilityName)
	var groups, caps **windows.SID
	var groupCount, capCount uint32
	ok, _, err := deriveCapabilityTest.Call(uintptr(unsafe.Pointer(name)), uintptr(unsafe.Pointer(&groups)), uintptr(unsafe.Pointer(&groupCount)),
		uintptr(unsafe.Pointer(&caps)), uintptr(unsafe.Pointer(&capCount)))
	if ok == 0 || capCount != 1 {
		t.Fatalf("DeriveCapabilitySidsFromName: %v (%d capability SIDs)", err, capCount)
	}
	got := (*caps).String()
	for _, list := range []struct {
		p **windows.SID
		n uint32
	}{{groups, groupCount}, {caps, capCount}} {
		for _, sid := range unsafe.Slice(list.p, list.n) {
			windows.LocalFree(windows.Handle(unsafe.Pointer(sid)))
		}
		windows.LocalFree(windows.Handle(unsafe.Pointer(list.p)))
	}
	if got != ContainerCapabilitySID {
		t.Fatalf("%s derives %s, not %s", ContainerCapabilityName, got, ContainerCapabilitySID)
	}
	if !strings.HasPrefix(got, "S-1-15-3-1024-") {
		t.Fatalf("%s is not a named capability's SID", got)
	}
}

func containedSpawner() {
	cmd := exec.Command(os.Args[0], "-test.run=^TestContainedStart$")
	cmd.Env = append(os.Environ(), "OA_SUPERVISION_CONTAINED=grandchild")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if err := cmd.Start(); err != nil {
		fmt.Printf("spawn-error=%v\n", err)
		os.Exit(1)
	}
	fmt.Printf("pid=%d\n", cmd.Process.Pid)
	if err := cmd.Wait(); err != nil {
		os.Exit(1)
	}
	os.Exit(0)
}

func containedGrandchild() {
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	v, err := strconv.ParseUint(strings.TrimSpace(line), 10, 64)
	if err != nil {
		fmt.Printf("bad-handle=%v\n", err)
		os.Exit(1)
	}
	f := os.NewFile(uintptr(v), "lent")
	data, err := io.ReadAll(f)
	f.Close()
	if err != nil {
		fmt.Printf("read-error=%v\n", err)
		os.Exit(1)
	}
	fmt.Printf("lent=%s\n", data)
	os.Exit(0)
}

// TestContainedStart: a child Options.Container names starts inside that
// AppContainer in the job's own CreateProcess, reaches no network, cannot
// open a file outside its grants by path, and reads the file a lent handle
// names with read access alone (FAC-P3, FAC-P4).
func TestContainedStart(t *testing.T) {
	if os.Getenv(containerSecurityProbeOptIn) != "1" {
		if child := os.Getenv("OA_SUPERVISION_CONTAINED"); child != "" {
			t.Fatalf("refusing sandbox security child mode %q without %s=1 after explicit Daybreak clearance", child, containerSecurityProbeOptIn)
		}
		t.Skipf("sandbox security probes are paused until the owner confirms Daybreak access is clear; set %s=1 only after that confirmation", containerSecurityProbeOptIn)
	}
	switch os.Getenv("OA_SUPERVISION_CONTAINED") {
	case "1":
		containedChild()
	case "spawn":
		containedSpawner()
	case "grandchild":
		containedGrandchild()
	}
	sid := testContainer(t)
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// The test binary lives where the container may not read; a copy in a
	// folder granted to it runs.
	bin := filepath.Join(t.TempDir(), "bin")
	if err := os.MkdirAll(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(exe)
	if err != nil {
		t.Fatal(err)
	}
	child := filepath.Join(bin, "child.exe")
	if err := os.WriteFile(child, data, 0o700); err != nil {
		t.Fatal(err)
	}
	grantRead(t, bin, sid)
	secret := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(secret, []byte("lent bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	inR, inW, _ := os.Pipe()
	outR, outW, _ := os.Pipe()
	defer outR.Close()
	p, err := Start(context.Background(), Options{
		Executable: child, Args: []string{"-test.run=^TestContainedStart$"}, Dir: bin,
		Env: append(os.Environ(), "OA_SUPERVISION_CONTAINED=1"), Stdin: inR, Stdout: outW,
		ForceTimeout: 5 * time.Second, Container: sid.String(),
	})
	inR.Close()
	outW.Close()
	if err != nil {
		inW.Close()
		t.Fatal(err)
	}
	f, err := os.Open(secret)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := p.LendTo(f.Fd(), windows.FILE_GENERIC_READ, os.Getpid()); err == nil {
		f.Close()
		inW.Close()
		p.Stop()
		p.Wait()
		t.Fatal("a lend to a process outside the owned job was accepted")
	}
	value, err := p.Lend(f.Fd(), windows.FILE_GENERIC_READ)
	f.Close()
	if err != nil {
		inW.Close()
		p.Stop()
		p.Wait()
		t.Fatal(err)
	}
	fmt.Fprintf(inW, "%d %s\n", value, secret)
	inW.Close()
	report, _ := io.ReadAll(outR)
	if code, err := p.Wait(); err != nil || code != 0 {
		t.Fatalf("child: exit %d, %v (%s)", code, err, report)
	}
	got := strings.TrimSpace(string(report))
	if got != "appcontainer=1 capabilities="+ContainerCapabilitySID+" tcp=refused path=denied lent=lent bytes" {
		t.Fatalf("the contained child reported %q", got)
	}
	if _, err := p.Lend(uintptr(windows.Stdout), windows.FILE_GENERIC_READ); err == nil {
		t.Fatal("a lend to an ended child was accepted")
	}
	// The interpreter-style launcher may spawn the actual receiver. The
	// reported PID is only a selector; LendTo proves this descendant's job
	// membership and container token on its opened process handle.
	inR, inW, _ = os.Pipe()
	outR, outW, _ = os.Pipe()
	defer outR.Close()
	spawner, err := Start(context.Background(), Options{
		Executable: child, Args: []string{"-test.run=^TestContainedStart$"}, Dir: bin,
		Env: append(os.Environ(), "OA_SUPERVISION_CONTAINED=spawn"), Stdin: inR, Stdout: outW,
		ForceTimeout: 5 * time.Second, Container: sid.String(),
	})
	inR.Close()
	outW.Close()
	if err != nil {
		inW.Close()
		t.Fatal(err)
	}
	reader := bufio.NewReader(outR)
	first, err := reader.ReadString('\n')
	if err != nil || !strings.HasPrefix(first, "pid=") {
		inW.Close()
		spawner.Stop()
		spawner.Wait()
		t.Fatalf("descendant selector %q: %v", first, err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(first, "pid=")))
	if err != nil {
		t.Fatal(err)
	}
	f, err = os.Open(secret)
	if err != nil {
		t.Fatal(err)
	}
	value, err = spawner.LendTo(f.Fd(), windows.FILE_GENERIC_READ, pid)
	f.Close()
	if err != nil {
		inW.Close()
		spawner.Stop()
		spawner.Wait()
		t.Fatalf("same-job descendant lend: %v", err)
	}
	fmt.Fprintf(inW, "%d\n", value)
	inW.Close()
	second, err := reader.ReadString('\n')
	if err != nil || strings.TrimSpace(second) != "lent=lent bytes" {
		t.Fatalf("descendant read %q: %v", second, err)
	}
	if code, err := spawner.Wait(); err != nil || code != 0 {
		t.Fatalf("spawner exit %d: %v", code, err)
	}
	if _, err := spawner.LendTo(uintptr(windows.Stdout), windows.FILE_GENERIC_READ, pid); err == nil {
		t.Fatal("an exited descendant accepted a lend")
	}
}

// TestContainerRefusals: Start refuses a SID that is not one AppContainer's
// and a contained start without LOCALAPPDATA, before any job or process
// exists (FAC-P3).
func TestContainerRefusals(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	user, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil {
		t.Fatal(err)
	}
	container := "S-1-15-2-46109015-3609682755-774105003-2280802390-3893791985-4224396588-3737174395"
	for name, c := range map[string]struct {
		sid string
		env []string
	}{
		"all application packages":            {"S-1-15-2-1", os.Environ()},
		"all restricted application packages": {"S-1-15-2-2", os.Environ()},
		"a user":                              {user.User.Sid.String(), os.Environ()},
		"a capability":                        {"S-1-15-3-1", os.Environ()},
		"no SID":                              {"oa.engine.pytorch.torch", os.Environ()},
		"no LOCALAPPDATA":                     {container, []string{"SYSTEMROOT=" + os.Getenv("SYSTEMROOT")}},
	} {
		p, err := Start(context.Background(), Options{Executable: exe, Args: []string{"-test.run=^$"}, Env: c.env,
			ForceTimeout: time.Second, Container: c.sid})
		if err == nil {
			p.Stop()
			p.Wait()
		}
		if !errors.Is(err, ErrContainer) {
			t.Fatalf("%s: %v", name, err)
		}
	}
}
