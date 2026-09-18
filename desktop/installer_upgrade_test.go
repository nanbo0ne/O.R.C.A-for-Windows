package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func readWindowsInstallerSource(t *testing.T) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("build", "windows", "installer", "project.nsi"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(string(body), "\r\n", "\n")
}

func readWindowsInstallerGuardSource(t *testing.T) string {
	t.Helper()
	body, err := os.ReadFile(filepath.Join("build", "windows", "installer", "installer_guard.nsh"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.ReplaceAll(string(body), "\r\n", "\n")
}

func windowsInstallerBlock(t *testing.T, source, first, last string) string {
	t.Helper()
	var lines []string
	for _, line := range strings.Split(source, "\n") {
		if len(lines) == 0 && strings.TrimSpace(line) != first {
			continue
		}
		lines = append(lines, line)
		if strings.TrimSpace(line) == last {
			return strings.Join(lines, "\n")
		}
	}
	t.Fatalf("missing or unterminated NSIS block %q", first)
	return ""
}

func windowsInstallerMakensis(t *testing.T) string {
	t.Helper()
	if runtime.GOOS != "windows" {
		t.Skip("NSIS probe is Windows-only")
	}
	if configured := os.Getenv("MAKENSIS"); configured != "" {
		path, err := exec.LookPath(configured)
		if err != nil {
			t.Fatalf("configured MAKENSIS is unavailable: %v", err)
		}
		return path
	}
	for _, candidate := range []string{
		`C:\Program Files (x86)\NSIS\makensis.exe`,
		`C:\Program Files\NSIS\makensis.exe`,
		"makensis.exe",
	} {
		if path, err := exec.LookPath(candidate); err == nil {
			return path
		}
	}
	t.Skip("NSIS compiler unavailable")
	return ""
}

func windowsInstallerNSISPath(path string) string {
	return strings.ReplaceAll(path, "$", "$$")
}

func TestWindowsInstallerUpgradeAndUninstallContracts(t *testing.T) {
	script := readWindowsInstallerSource(t)

	for _, want := range []string{
		`InstallDirRegKey HKCU "${UNINST_KEY}" "InstallLocation"`,
		`!define ORCA_INSTALLDIR_SENTINEL "$LOCALAPPDATA\Programs\O.R.C.A for Windows.__nsis_default__"`,
		`InstallDir "${ORCA_INSTALLDIR_SENTINEL}"`,
		`StrCmp $INSTDIR "${ORCA_INSTALLDIR_SENTINEL}" use_compat_install_dir install_dir_done`,
		`!define REQUEST_EXECUTION_LEVEL "user"`,
		`ManifestDPIAware true`,
		`Call orca.closeTargetProcesses`,
		`Call un.orca.closeTargetProcesses`,
		`!include "installer_guard.nsh"`,
		`SetShellVarContext current`,
		`WriteRegStr HKCU "${UNINST_KEY}" "InstallLocation" "$INSTDIR"`,
		`WriteRegStr HKCU "${UNINST_KEY}" "QuietUninstallString" "$\"$INSTDIR\uninstall.exe$\" /S"`,
		`Delete "$INSTDIR\uninstall.bat"`,
		`RMDir "$INSTDIR"`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("installer is missing %q", want)
		}
	}
	userLevel := strings.Index(script, `!define REQUEST_EXECUTION_LEVEL "user"`)
	include := strings.Index(script, `!include "wails_tools.nsh"`)
	if userLevel < 0 || include <= userLevel {
		t.Fatal("the user execution level must override Wails before its generated include")
	}

	if strings.Contains(script, `taskkill.exe" /IM`) {
		t.Fatal("installer must not terminate every Orca.exe by image name")
	}
	if strings.Contains(script, `File /oname=uninstall.bat`) {
		t.Fatal("new installs must not ship the legacy unsafe uninstall batch")
	}
	if strings.Contains(script, `RMDir /r "$INSTDIR"`) {
		t.Fatal("uninstaller must not recursively remove the selected install directory")
	}
	for _, line := range strings.Split(script, "\n") {
		fields := strings.Fields(line)
		if len(fields) > 1 && (strings.HasPrefix(fields[0], "WriteReg") || strings.HasPrefix(fields[0], "DeleteReg")) && fields[1] != "HKCU" {
			t.Fatalf("installer registry writes must remain per-user: %s", line)
		}
	}

	onInit := strings.Index(script, "Function .onInit")
	if onInit < 0 {
		t.Fatal("installer is missing .onInit")
	}
	onInitBody := windowsInstallerBlock(t, script, "Function .onInit", "FunctionEnd")
	explicit := strings.Index(onInitBody, `StrCmp $INSTDIR "${ORCA_INSTALLDIR_SENTINEL}" use_compat_install_dir install_dir_done`)
	current := strings.Index(onInitBody, `ReadRegStr $0 HKCU "${UNINST_KEY}" "InstallLocation"`)
	currentIcon := strings.Index(onInitBody, `ReadRegStr $0 HKCU "${UNINST_KEY}" "DisplayIcon"`)
	legacy := strings.Index(onInitBody, `ReadRegStr $0 HKCU "${LEGACY_UNINST_KEY}" "InstallLocation"`)
	legacyIcon := strings.Index(onInitBody, `ReadRegStr $0 HKCU "${LEGACY_UNINST_KEY}" "DisplayIcon"`)
	fallback := strings.Index(onInitBody, "install_dir_default:")
	if explicit < 0 || current <= explicit || currentIcon <= current || legacy <= currentIcon || legacyIcon <= legacy || fallback <= legacyIcon {
		t.Fatalf("install directory precedence must be /D, current location/icon, legacy location/icon, default: %v", []int{explicit, current, currentIcon, legacy, legacyIcon, fallback})
	}
	if strings.Contains(onInitBody, `StrCmp $INSTDIR "" 0 done`) {
		t.Fatal("legacy detection is still short-circuited by the InstallDir default")
	}
	if !strings.Contains(onInitBody, "install_dir_default:") {
		t.Fatal("missing final default install path fallback")
	}

	uninstall := windowsInstallerBlock(t, script, `Section "uninstall"`, "SectionEnd")
	deleteData := windowsInstallerBlock(t, uninstall, `${If} $DeleteSavedData == ${BST_CHECKED}`, `${EndIf}`)
	for _, path := range []string{
		`$AppData\${PRODUCT_EXECUTABLE}`, `$AppData\deepseek-orca`, `$AppData\orca`,
		`$LocalAppData\deepseek-orca`, `$Profile\.deepseek-orca`, `$AppData\O.R.C.A`,
		`$LocalAppData\O.R.C.A`, `$INSTDIR\data`, `$INSTDIR\.deepseek-orca`,
	} {
		remove := `RMDir /r "` + path + `"`
		if !strings.Contains(deleteData, remove) || strings.Count(script, remove) != 1 {
			t.Errorf("saved data must be removed only in the explicit opt-in branch: %s", path)
		}
	}
	sources := script + "\n" + readWindowsInstallerGuardSource(t)
	for _, want := range []string{
		`${NSD_Uncheck} $DeleteSavedDataCheckbox`,
		`${NSD_GetState} $DeleteSavedDataCheckbox $DeleteSavedData`,
	} {
		if !strings.Contains(sources, want) {
			t.Errorf("uninstall must default to keeping saved data: missing %s", want)
		}
	}
}

func TestWindowsInstallerUsesNativeGuardBeforeWrites(t *testing.T) {
	script := readWindowsInstallerSource(t)
	guardSource := readWindowsInstallerGuardSource(t)
	for _, want := range []string{`!insertmacro orca.guard ""`, `!insertmacro orca.guard "un."`} {
		if !strings.Contains(script+guardSource, want) {
			t.Errorf("install and uninstall must share the native guard: missing %s", want)
		}
	}
	guard := windowsInstallerBlock(t, guardSource, "!macro orca.guard PREFIX", "!macroend")
	for _, want := range []string{
		`orca-install-guard.exe`, `install-files.txt`, `kernel32::CreateProcessW`,
		`--dir=$INSTDIR`, `--manifest=$PLUGINSDIR\install-files.txt`, `--status=$GuardStatus`,
		`--log=$GuardLog`, `--cancel=$PLUGINSDIR\cancel-guard`, `--parent=$GuardParent`,
		`SetErrorLevel $GuardCode`, `Abort "$GuardMessage"`,
	} {
		if !strings.Contains(guard, want) {
			t.Errorf("native guard contract is missing %q", want)
		}
	}
	for _, forbidden := range []string{"powershell.exe", "Get-TargetProcesses", "taskkill"} {
		if strings.Contains(strings.ToLower(script+guardSource), strings.ToLower(forbidden)) {
			t.Errorf("installer must use the native path-scoped guard, found %q", forbidden)
		}
	}
	install := windowsInstallerBlock(t, script, "Section", "SectionEnd")
	firstGuard := strings.Index(install, "Call orca.closeTargetProcesses")
	bootstrap := strings.Index(install, "!insertmacro wails.webview2runtime")
	lastGuard := strings.LastIndex(install, "Call orca.closeTargetProcesses")
	firstWrite := strings.Index(install, "SetOutPath $INSTDIR")
	if firstGuard < 0 || bootstrap <= firstGuard || lastGuard <= bootstrap || firstWrite <= lastGuard {
		t.Fatal("guard must run before runtime bootstrap and again before replacing payload files")
	}
	uninstall := windowsInstallerBlock(t, script, `Section "uninstall"`, "SectionEnd")
	guardAt := strings.Index(uninstall, "Call un.orca.closeTargetProcesses")
	deleteAt := strings.Index(uninstall, "Delete ")
	if guardAt < 0 || deleteAt <= guardAt {
		t.Fatal("uninstall must guard before deleting files or shortcuts")
	}
}

func TestWindowsProductNamePreservesUpgradeIdentifiers(t *testing.T) {
	data, err := os.ReadFile("wails.json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"productName": "O.R.C.A. for Windows"`) {
		t.Fatal("Windows display name must match the canonical product name")
	}
	script := readWindowsInstallerSource(t)
	for _, want := range []string{
		`!define UNINST_KEY_NAME "O.R.C.A for Windows"`,
		`IfFileExists "$DESKTOP\O.R.C.A for Windows.lnk" shortcut_choice_done 0`,
		`Delete "$DESKTOP\O.R.C.A for Windows.lnk"`,
		`StrCmp $0 $1 0 legacy_cleanup_done`,
		`Delete "$INSTDIR\deepseek-orca-desktop.exe"`,
		`DeleteRegKey HKCU "${LEGACY_UNINST_KEY}"`,
	} {
		if !strings.Contains(script, want) {
			t.Fatalf("installer lost identity compatibility: %s", want)
		}
	}
}
