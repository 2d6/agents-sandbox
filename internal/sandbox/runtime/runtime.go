package runtime

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"github.com/Masterminds/semver/v3"

	sandboxmsb "github.com/inoio/agents-sandbox/internal/sandbox/msb"
	"github.com/inoio/agents-sandbox/internal/termio"

	msbSdk "github.com/superradcompany/microsandbox/sdk/go"
)

const (
	windowsOS     = "windows"
	msbBinaryName = "msb"
)

// Relation describes the relationship between the runtime selected for
// this process and the SDK version linked into agents-sandbox.
type Relation string

const (
	RuntimeEqual      Relation = "equal"
	RuntimeNewer      Relation = "newer"
	RuntimeOlder      Relation = "older"
	RuntimeUnknown    Relation = "unknown"
	RuntimeMissing    Relation = "missing"
	RuntimeIncomplete Relation = "incomplete"
)

// Inspection contains the runtime paths and version information used by
// the mismatch decision. It is collected before the SDK is allowed to load its
// FFI library or open the runtime database.
//
//exhaustruct:ignore
type Inspection struct {
	AgentsSandboxVersion string
	RequiredVersion      string
	InstalledVersion     string
	MSBPath              string
	MSBHome              string
	LibkrunfwPath        string
	Relation             Relation
}

type selectedRuntime struct {
	Home          string
	MSBPath       string
	LibkrunfwPath string
}

var runtimeState = struct {
	mutex    sync.Mutex
	prepared bool
}{mutex: sync.Mutex{}, prepared: false}

// Command seams keep runtime inspection unit-testable without touching the host
// runtime or replacing the test executable.
var (
	runMSBVersion = func(path string) (string, error) {
		output, err := exec.Command(path, "--version").Output()
		if err != nil {
			return "", err
		}
		return strings.TrimSpace(string(output)), nil
	}
	ensureRuntime = func(ctx context.Context) error {
		_, err := msbSdk.EnsureRuntime(
			ctx,
			msbSdk.RuntimeConfig{},
			msbSdk.InstallOptions{},
		)
		return err
	}
	validateRuntime = sandboxmsb.ValidateInstalled
)

// runtimeClientIsReal is a seam for testing the public preparation gate while
// keeping normal command tests on their mocked msb client.
var runtimeClientIsReal = sandboxmsb.IsRealClient

var userHomeDir = os.UserHomeDir

// InspectRuntime identifies the msb executable selected by the SDK before the
// SDK itself initializes. It intentionally does not call any SDK operation.
func InspectRuntime() (Inspection, error) {
	requiredVersion := msbSdk.SDKVersion()
	return inspectRuntime(requiredVersion)
}

// inspectRuntime contains the version-independent inspection logic so invalid
// SDK metadata can be tested without mutating the linked SDK package.
func inspectRuntime(requiredVersion string) (Inspection, error) {
	selected, err := selectRuntime()
	if err != nil {
		return Inspection{}, err
	}
	inspection := Inspection{
		RequiredVersion: requiredVersion,
		MSBHome:         selected.Home,
		MSBPath:         selected.MSBPath,
		LibkrunfwPath:   selected.LibkrunfwPath,
		Relation:        RuntimeMissing,
	}

	msbPresent, statErr := runtimeFilePresent(selected.MSBPath)
	if statErr != nil {
		return Inspection{}, fmt.Errorf("inspect msb executable %s: %w", selected.MSBPath, statErr)
	}
	libkrunfwPresent, statErr := runtimeFilePresent(selected.LibkrunfwPath)
	if statErr != nil {
		return Inspection{}, fmt.Errorf("inspect libkrunfw %s: %w", selected.LibkrunfwPath, statErr)
	}
	if !msbPresent && !libkrunfwPresent {
		return inspection, nil
	}
	if !msbPresent || !libkrunfwPresent {
		inspection.Relation = RuntimeIncomplete
		return inspection, nil
	}
	versionOutput, versionErr := runMSBVersion(inspection.MSBPath)
	if versionErr != nil {
		inspection.Relation = RuntimeUnknown
		return inspection, nil //nolint:nilerr // malformed version output is classified as unknown
	}
	installedVersion, parseErr := parseMSBVersion(versionOutput)
	if parseErr != nil {
		inspection.Relation = RuntimeUnknown
		return inspection, nil //nolint:nilerr // malformed version output is classified as unknown
	}
	inspection.InstalledVersion = installedVersion
	inspection.Relation, err = compareRuntimeVersions(requiredVersion, installedVersion)
	if err != nil {
		inspection.Relation = RuntimeUnknown
	}
	return inspection, nil
}

func selectRuntime() (selectedRuntime, error) {
	home, err := PrivateHome()
	if err != nil {
		return selectedRuntime{}, err
	}
	return selectedRuntime{
		Home:          home,
		MSBPath:       filepath.Join(home, "bin", msbFilename()),
		LibkrunfwPath: filepath.Join(home, "lib", libkrunfwFilename()),
	}, nil
}

func runtimeFilePresent(path string) (bool, error) {
	if path == "" {
		return false, nil
	}
	info, err := os.Stat(path)
	if err == nil {
		return info.Mode().IsRegular(), nil
	}
	if os.IsNotExist(err) {
		return false, nil
	}
	return false, err
}

var inspectRuntimeFunc = inspectRuntime

// PrepareRuntime inspects and resolves the runtime before any SDK operation.
// Mock msb clients used by command tests do not need host runtime setup.
func PrepareRuntime(ctx context.Context, ui termio.UI, launcherVersion string) error {
	if !runtimeClientIsReal() {
		return nil
	}
	return prepareRuntime(ctx, ui, launcherVersion)
}

func prepareRuntime(ctx context.Context, ui termio.UI, launcherVersion string) error {
	runtimeState.mutex.Lock()
	defer runtimeState.mutex.Unlock()
	if runtimeState.prepared {
		return nil
	}

	if _, err := ApplyHome(); err != nil {
		return fmt.Errorf("apply microsandbox home: %w", err)
	}
	inspection, err := inspectRuntimeFunc(msbSdk.SDKVersion())
	if err != nil {
		return err
	}
	inspection.AgentsSandboxVersion = launcherVersion
	ui.Verbosef(
		"microsandbox runtime: required=%s installed=%s path=%s home=%s",
		inspection.RequiredVersion,
		valueOr(inspection.InstalledVersion, "missing"),
		inspection.MSBPath,
		inspection.MSBHome,
	)

	switch inspection.Relation {
	case RuntimeEqual:
		if err := validateRuntime(ctx); err != nil {
			return fmt.Errorf("validate msb runtime: %w", err)
		}
		sandboxmsb.SkipRuntimeInstall()
		runtimeState.prepared = true
		return nil
	case RuntimeMissing:
		if err := ensureRuntime(ctx); err != nil {
			return fmt.Errorf("install msb runtime: %w", err)
		}
		if err := validatePreparedRuntime(ctx); err != nil {
			return err
		}
		sandboxmsb.SkipRuntimeInstall()
		runtimeState.prepared = true
		return nil
	case RuntimeNewer, RuntimeOlder, RuntimeUnknown, RuntimeIncomplete:
		return runtimeMismatchError(
			inspection,
			"runtime is not the SDK-pinned version; reinstall agents-sandbox runtime",
		)
	default:
		return fmt.Errorf("unknown msb runtime relation %q", inspection.Relation)
	}
}

func runtimeMismatchError(inspection Inspection, reason string) error {
	return fmt.Errorf(
		"msb runtime mismatch: required %s, installed %s at %s: %s",
		inspection.RequiredVersion,
		valueOr(inspection.InstalledVersion, "unknown"),
		sanitizeRuntimePath(inspection.MSBPath, inspection.MSBHome),
		reason,
	)
}

func validatePreparedRuntime(ctx context.Context) error {
	inspection, err := inspectRuntimeFunc(msbSdk.SDKVersion())
	if err != nil {
		return err
	}
	if inspection.Relation != RuntimeEqual {
		return runtimeMismatchError(inspection, "runtime alignment did not complete")
	}
	if err := validateRuntime(ctx); err != nil {
		return fmt.Errorf("validate aligned msb runtime: %w", err)
	}
	return nil
}

func sanitizeRuntimePath(path, home string) string {
	if path == "" {
		return "<unset>"
	}
	cleanPath := filepath.Clean(path)
	if home != "" {
		if relative, err := filepath.Rel(
			filepath.Clean(home),
			cleanPath,
		); err == nil && relative != ".." &&
			!strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return filepath.Join("$MSB_HOME", relative)
		}
	}
	if userHome, err := userHomeDir(); err == nil {
		if relative, relErr := filepath.Rel(
			filepath.Clean(userHome),
			cleanPath,
		); relErr == nil && relative != ".." &&
			!strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return filepath.Join("$HOME", relative)
		}
	}
	return filepath.Base(cleanPath)
}

func msbFilename() string {
	return msbFilenameFor(runtime.GOOS)
}

func libkrunfwFilename() string {
	return libkrunfwFilenameFor(runtime.GOOS)
}

func msbFilenameFor(goos string) string {
	if goos == windowsOS {
		return "msb.exe"
	}
	return msbBinaryName
}

func libkrunfwFilenameFor(goos string) string {
	switch goos {
	case "darwin":
		return "libkrunfw.5.dylib"
	case windowsOS:
		return "libkrunfw.dll"
	default:
		return "libkrunfw.so.5.6.1"
	}
}

func parseMSBVersion(output string) (string, error) {
	output = strings.TrimSpace(output)
	if !strings.HasPrefix(output, "msb ") {
		return "", fmt.Errorf("unexpected msb version output %q", output)
	}
	version := strings.TrimSpace(strings.TrimPrefix(output, "msb "))
	if _, err := semver.NewVersion(version); err != nil {
		return "", fmt.Errorf("invalid msb version %q: %w", version, err)
	}
	return version, nil
}

func compareRuntimeVersions(required, installed string) (Relation, error) {
	requiredVersion, err := semver.NewVersion(required)
	if err != nil {
		return RuntimeUnknown, err
	}
	installedVersion, err := semver.NewVersion(installed)
	if err != nil {
		return RuntimeUnknown, err
	}
	switch {
	case installedVersion.Equal(requiredVersion):
		return RuntimeEqual, nil
	case installedVersion.GreaterThan(requiredVersion):
		return RuntimeNewer, nil
	default:
		return RuntimeOlder, nil
	}
}

func valueOr(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
