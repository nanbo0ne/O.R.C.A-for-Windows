Unicode true

####
## O.R.C.A for Windows per-user NSIS installer.
##
## This file is COMMITTED and customized (Wails leaves an existing project.nsi
## untouched and only regenerates wails_tools.nsh). The customizations vs.
## Wails' default template:
##
##   1. REQUEST_EXECUTION_LEVEL "user" + InstallDir under $LOCALAPPDATA - install
##      without administrator rights. This is what lets the auto-updater re-run a
##      freshly downloaded installer interactively, with no UAC prompt. The
##      updater supplies `/D=<current install dir>` and intentionally leaves
##      the NSIS directory page available for a user override.
##   2. Uninstall registry under HKCU (not HKLM). Wails' wails.writeUninstaller /
##      wails.deleteUninstaller macros hard-code HKLM, which a non-admin install
##      cannot write - so we inline HKCU versions below instead.
##   3. InstallDir is remembered across updates via InstallDirRegKey +
##      InstallLocation (HKCU\...\Uninstall\InstallLocation). When upgrading from
##      a build that did not write InstallLocation yet, .onInit falls back to the
##      old DisplayIcon path before using the default. Without this, every release
##      forces the user back to %LOCALAPPDATA%\Programs\DeepSeek-Orca even if they had
##      moved the install to a different drive (e.g. D:\Tools\DeepSeek-Orca), leaving
##      the old install orphaned. The updater now passes the current directory through
##      interactive `/D=<current install dir>` invocation.
##
## Everything else mirrors Wails' generated default. Defines below override the
## ProjectInfo values that wails_tools.nsh would otherwise populate.
####

## Install per-user (no admin). Must be defined BEFORE including wails_tools.nsh,
## which only sets the "admin" default when REQUEST_EXECUTION_LEVEL is undefined.
!define REQUEST_EXECUTION_LEVEL "user"
!define UNINST_KEY_NAME "O.R.C.A for Windows"
!define PRODUCT_EXECUTABLE "Orca.exe"
!define LEGACY_UNINST_KEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\DeepSeek-Orca"

####
## Include the wails tools (auto-generated; provides INFO_* defines and the
## wails.* macros used below).
####
!include "wails_tools.nsh"
!include "FileFunc.nsh"
!include "LogicLib.nsh"

# The version information for this two must consist of 4 parts
VIProductVersion "${INFO_PRODUCTVERSION}.0"
VIFileVersion    "${INFO_PRODUCTVERSION}.0"

VIAddVersionKey "CompanyName"     "${INFO_COMPANYNAME}"
VIAddVersionKey "FileDescription" "${INFO_PRODUCTNAME} Installer"
VIAddVersionKey "ProductVersion"  "${INFO_PRODUCTVERSION}"
VIAddVersionKey "FileVersion"     "${INFO_PRODUCTVERSION}"
VIAddVersionKey "LegalCopyright"  "${INFO_COPYRIGHT}"
VIAddVersionKey "ProductName"     "${INFO_PRODUCTNAME}"

# Enable HiDPI support. https://nsis.sourceforge.io/Reference/ManifestDPIAware
ManifestDPIAware true

!include "MUI2.nsh"
!include "nsDialogs.nsh"

!define MUI_ICON "..\icon.ico"
!define MUI_UNICON "..\icon.ico"
# !define MUI_WELCOMEFINISHPAGE_BITMAP "resources\leftimage.bmp" #Include this to add a bitmap on the left side of the Welcome Page. Must be a size of 164x314
SetFont "Microsoft YaHei UI" 9
!define MUI_FINISHPAGE_RUN "$INSTDIR\${PRODUCT_EXECUTABLE}"
!define MUI_FINISHPAGE_RUN_TEXT "运行 ${INFO_PRODUCTNAME}"
!define MUI_ABORTWARNING
!define MUI_LICENSEPAGE_CHECKBOX
!define MUI_CUSTOMFUNCTION_ABORT CancelGuard
!define MUI_CUSTOMFUNCTION_UNABORT un.CancelGuard

Var DeleteSavedDataCheckbox
Var DeleteSavedData
Var DesktopShortcutCheckbox
Var CreateDesktopShortcut

!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_LICENSE "resources\eula.txt"
!insertmacro MUI_PAGE_DIRECTORY
Page custom InstallOptionsPage InstallOptionsPageLeave
!insertmacro MUI_PAGE_INSTFILES # Installing page.
!insertmacro MUI_PAGE_FINISH

!insertmacro MUI_UNPAGE_CONFIRM
UninstPage custom un.DeleteDataPage un.DeleteDataPageLeave
!insertmacro MUI_UNPAGE_INSTFILES # Uninstalling page

!insertmacro MUI_LANGUAGE "SimpChinese" # Set the Language of the installer

## The following two statements can be used to sign the installer and the uninstaller. The path to the binaries are provided in %1
#!uninstfinalize 'signtool --file "%1"'
#!finalize 'signtool --file "%1"'

Name "${INFO_PRODUCTNAME}"
!ifdef ORCA_PREVIEW
Caption "${INFO_PRODUCTNAME} ${INFO_PRODUCTVERSION} 预览版安装"
!endif
OutFile "..\..\bin\O.R.C.A-for-Windows-windows-${ARCH}-installer.exe" # Name of the installer's file.
!define ORCA_DEFAULT_INSTALLDIR "$LOCALAPPDATA\Programs\O.R.C.A for Windows"
!define ORCA_INSTALLDIR_SENTINEL "$LOCALAPPDATA\Programs\O.R.C.A for Windows.__nsis_default__"
InstallDirRegKey HKCU "${UNINST_KEY}" "InstallLocation" # Reuse the previous install path on update; .onInit falls back to the default on first install.
InstallDir "${ORCA_INSTALLDIR_SENTINEL}" # .onInit replaces this sentinel when no /D or registry path exists.
ShowInstDetails hide
ShowUninstDetails hide
AllowSkipFiles off
!include "installer_guard.nsh"

####
## Per-user uninstaller registry (HKCU). Replaces wails.writeUninstaller /
## wails.deleteUninstaller, which write HKLM and would fail without admin rights.
####
!macro orca.writeUninstaller
    ClearErrors
    WriteUninstaller "$INSTDIR\uninstall.exe"

    WriteRegStr HKCU "${UNINST_KEY}" "Publisher" "${INFO_COMPANYNAME}"
    WriteRegStr HKCU "${UNINST_KEY}" "DisplayName" "${INFO_PRODUCTNAME}"
    WriteRegStr HKCU "${UNINST_KEY}" "DisplayVersion" "${INFO_PRODUCTVERSION}"
    WriteRegStr HKCU "${UNINST_KEY}" "DisplayIcon" "$INSTDIR\${PRODUCT_EXECUTABLE}"
    WriteRegStr HKCU "${UNINST_KEY}" "UninstallString" "$\"$INSTDIR\uninstall.exe$\""
    WriteRegStr HKCU "${UNINST_KEY}" "QuietUninstallString" "$\"$INSTDIR\uninstall.exe$\" /S"
    # Persist the resolved install path so a subsequent update picks it up
    # via InstallDirRegKey above. Without this, every release would force the
    # user back to %LOCALAPPDATA%\Programs\DeepSeek-Orca even if they had moved
    # the install to a different drive (e.g. D:\Tools\DeepSeek-Orca). The auto-
    # updater supplies the current executable directory with /D and trusts the
    # persisted path as the fallback for manually opened installers.
    WriteRegStr HKCU "${UNINST_KEY}" "InstallLocation" "$INSTDIR"

    ${GetSize} "$INSTDIR" "/S=0K" $0 $1 $2
    IntFmt $0 "0x%08X" $0
    WriteRegDWORD HKCU "${UNINST_KEY}" "EstimatedSize" "$0"
    ${If} ${Errors}
        SetErrorLevel 25
        Abort "无法保存卸载信息，安装未完成。"
    ${EndIf}
!macroend

!macro orca.deleteUninstaller
    Delete "$INSTDIR\uninstall.exe"
    Delete "$INSTDIR\uninstall.bat"
    DeleteRegKey HKCU "${UNINST_KEY}"
!macroend

Function .onInit
   !insertmacro wails.checkArchitecture
   SetShellVarContext current
   Call InitGuard

   ; NSIS consumes /D before $CMDLINE is exposed, so use a compile-time
   ; sentinel to distinguish "no /D" from an explicit path. /D therefore keeps
   ; its documented highest priority even when it equals the normal default.
   StrCmp $INSTDIR "${ORCA_INSTALLDIR_SENTINEL}" use_compat_install_dir install_dir_done

use_compat_install_dir:
   StrCpy $INSTDIR "${ORCA_DEFAULT_INSTALLDIR}"
   ClearErrors
   ReadRegStr $0 HKCU "${UNINST_KEY}" "InstallLocation"
   IfErrors current_display_icon
   StrCmp $0 "" current_display_icon
   StrCpy $INSTDIR $0
   Goto install_dir_done

current_display_icon:
   ClearErrors
   ReadRegStr $0 HKCU "${UNINST_KEY}" "DisplayIcon"
   IfErrors legacy_install_dir
   StrCmp $0 "" legacy_install_dir
   ${GetParent} "$0" $INSTDIR
   StrCmp $INSTDIR "" legacy_install_dir install_dir_done

legacy_install_dir:
   ; V2 used a separate uninstall key. Reuse its path for an in-place upgrade.
   ClearErrors
   ReadRegStr $0 HKCU "${LEGACY_UNINST_KEY}" "InstallLocation"
   IfErrors legacy_display_icon
   StrCmp $0 "" legacy_display_icon
   StrCpy $INSTDIR $0
   Goto install_dir_done

legacy_display_icon:
   ClearErrors
   ReadRegStr $0 HKCU "${LEGACY_UNINST_KEY}" "DisplayIcon"
   IfErrors install_dir_default
   StrCmp $0 "" install_dir_default
   ${GetParent} "$0" $INSTDIR
   StrCmp $INSTDIR "" install_dir_default install_dir_done

install_dir_default:
   StrCpy $INSTDIR "${ORCA_DEFAULT_INSTALLDIR}"

install_dir_done:

   ; First installs create a desktop shortcut by default. Upgrades preserve the
   ; user's existing choice, including a shortcut they deliberately removed.
   StrCpy $CreateDesktopShortcut ${BST_CHECKED}
   ClearErrors
   ReadRegStr $1 HKCU "${UNINST_KEY}" "DisplayName"
   IfErrors shortcut_choice_done
   StrCmp $1 "" shortcut_choice_done
   IfFileExists "$DESKTOP\${INFO_PRODUCTNAME}.lnk" shortcut_choice_done 0
   IfFileExists "$DESKTOP\O.R.C.A for Windows.lnk" shortcut_choice_done 0
   StrCpy $CreateDesktopShortcut ${BST_UNCHECKED}

shortcut_choice_done:
FunctionEnd


Section
    !insertmacro wails.setShellContext

    SetDetailsPrint both
    DetailPrint "正在准备安装"
    Call orca.closeTargetProcesses

    DetailPrint "正在检查 WebView2 运行环境"
    !insertmacro wails.webview2runtime

    ; Runtime bootstrap may take time: recheck immediately before replacing files.
    Call orca.closeTargetProcesses
    DetailPrint "正在安装 O.R.C.A."
    SetOutPath $INSTDIR
    Delete "$INSTDIR\uninstall.bat"

    !insertmacro wails.files
    File /oname=node.exe "..\installer-go\payload\node.exe"
    File /oname=LICENSE.node.txt "..\installer-go\payload\LICENSE.node.txt"
    File /oname=THIRD-PARTY-NOTICES.txt "..\..\..\..\THIRD-PARTY-NOTICES.txt"
    SetOutPath "$INSTDIR\codegraph"
    File /r "..\installer-go\payload\codegraph\*.*"
    SetOutPath "$INSTDIR"
    DetailPrint "正在保存快捷方式与卸载信息"

    Delete "$SMPROGRAMS\O.R.C.A for Windows.lnk"
    Delete "$SMPROGRAMS\Uninstall O.R.C.A for Windows.lnk"
    Delete "$DESKTOP\O.R.C.A for Windows.lnk"
    CreateShortcut "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"
    ${If} $CreateDesktopShortcut == ${BST_CHECKED}
        CreateShortCut "$DESKTOP\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"
    ${Else}
        Delete "$DESKTOP\${INFO_PRODUCTNAME}.lnk"
    ${EndIf}
    CreateShortcut "$SMPROGRAMS\Uninstall ${INFO_PRODUCTNAME}.lnk" "$INSTDIR\uninstall.exe"

    !insertmacro wails.associateFiles
    !insertmacro wails.associateCustomProtocols

    Delete "$SMPROGRAMS\DeepSeek-Orca.lnk"
    Delete "$DESKTOP\DeepSeek-Orca.lnk"
    !insertmacro orca.writeUninstaller

    ; Retire V2 only when this install actually replaced its executable folder.
    ReadRegStr $0 HKCU "${LEGACY_UNINST_KEY}" "InstallLocation"
    StrCmp $0 "" 0 legacy_cleanup_check
    ReadRegStr $0 HKCU "${LEGACY_UNINST_KEY}" "DisplayIcon"
    StrCmp $0 "" legacy_cleanup_done
    ${GetParent} "$0" $0
legacy_cleanup_check:
    GetFullPathName $0 "$0"
    GetFullPathName $1 "$INSTDIR"
    StrCmp $0 $1 0 legacy_cleanup_done
    Delete "$INSTDIR\deepseek-orca-desktop.exe"
    Delete "$SMPROGRAMS\Uninstall DeepSeek-Orca.lnk"
    DeleteRegKey HKCU "${LEGACY_UNINST_KEY}"
legacy_cleanup_done:
SectionEnd

Section "uninstall"
    !insertmacro wails.setShellContext

    Call un.orca.closeTargetProcesses

    Delete "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk"
    Delete "$SMPROGRAMS\Uninstall ${INFO_PRODUCTNAME}.lnk"
    Delete "$DESKTOP\${INFO_PRODUCTNAME}.lnk"
    Delete "$SMPROGRAMS\O.R.C.A for Windows.lnk"
    Delete "$SMPROGRAMS\Uninstall O.R.C.A for Windows.lnk"
    Delete "$DESKTOP\O.R.C.A for Windows.lnk"
    Delete "$SMPROGRAMS\DeepSeek-Orca.lnk"
    Delete "$DESKTOP\DeepSeek-Orca.lnk"

    !insertmacro wails.unassociateFiles
    !insertmacro wails.unassociateCustomProtocols

    ${If} $DeleteSavedData == ${BST_CHECKED}
        RMDir /r "$AppData\${PRODUCT_EXECUTABLE}" # Remove the WebView2 DataPath with saved data
        RMDir /r "$AppData\deepseek-orca"
        RMDir /r "$AppData\orca"
        RMDir /r "$LocalAppData\deepseek-orca"
        RMDir /r "$Profile\.deepseek-orca"
        RMDir /r "$AppData\O.R.C.A"
        RMDir /r "$LocalAppData\O.R.C.A"
        RMDir /r "$INSTDIR\data"
        RMDir /r "$INSTDIR\.deepseek-orca"
    ${EndIf}

    Delete "$INSTDIR\${PRODUCT_EXECUTABLE}"
    Delete "$INSTDIR\node.exe"
    Delete "$INSTDIR\LICENSE.node.txt"
    Delete "$INSTDIR\THIRD-PARTY-NOTICES.txt"
    RMDir /r "$INSTDIR\codegraph"
    !insertmacro orca.deleteUninstaller

    ; Never recursively remove a user-selected install directory: it may contain
    ; unrelated files. Known app-owned entries above are removed explicitly.
    RMDir "$INSTDIR"
SectionEnd
