Unicode true
!define REQUEST_EXECUTION_LEVEL "user"
!define UNINST_KEY_NAME "ORCA beta"
!define PRODUCT_EXECUTABLE "OrcaBeta.exe"
!include "wails_tools.nsh"
!include "FileFunc.nsh"
!include "LogicLib.nsh"
!include "MUI2.nsh"
!include "nsDialogs.nsh"

VIProductVersion "3.0.14.2"
VIFileVersion "3.0.14.2"
VIAddVersionKey "CompanyName" "${INFO_COMPANYNAME}"
VIAddVersionKey "FileDescription" "ORCA beta Installer"
VIAddVersionKey "ProductVersion" "3.0.14-beta.2"
VIAddVersionKey "FileVersion" "3.0.14.2"
VIAddVersionKey "ProductName" "ORCA beta"
VIAddVersionKey "LegalCopyright" "${INFO_COPYRIGHT}"
ManifestDPIAware true
SetFont "Microsoft YaHei UI" 9
!define MUI_ICON "..\icon.ico"
!define MUI_UNICON "..\icon.ico"
!define MUI_ABORTWARNING
!define MUI_LICENSEPAGE_CHECKBOX
!define MUI_FINISHPAGE_RUN "$INSTDIR\${PRODUCT_EXECUTABLE}"
!define MUI_FINISHPAGE_RUN_TEXT "Run ORCA beta"
!define MUI_DIRECTORYPAGE_VERIFYONLEAVE
!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_LICENSE "resources\eula.txt"
!define MUI_PAGE_CUSTOMFUNCTION_LEAVE CheckBetaTarget
!insertmacro MUI_PAGE_DIRECTORY
Page custom InstallOptionsPage InstallOptionsPageLeave
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "SimpChinese"

Name "ORCA beta"
Caption "ORCA beta 3.0.14-beta.2"
OutFile "..\..\bin\ORCA-beta-3.0.14-beta.2-windows-${ARCH}-installer.exe"
InstallDir "$LOCALAPPDATA\Programs\ORCA beta"
InstallDirRegKey HKCU "${UNINST_KEY}" "InstallLocation"
ShowInstDetails hide
ShowUninstDetails hide
AllowSkipFiles off
Var DesktopShortcutCheckbox
Var CreateDesktopShortcut

!macro beta.preflight PREFIX
Function ${PREFIX}CheckBetaTarget
    nsExec::ExecToStack /TIMEOUT=15000 '"$PLUGINSDIR\orca-beta-guard.exe" "$INSTDIR"'
    Pop $0
    Pop $1
    ${If} $0 != 0
        SetErrorLevel 21
        IfSilent +2
        MessageBox MB_OK|MB_ICONEXCLAMATION "$1"
        Abort
    ${EndIf}
FunctionEnd
!macroend
!insertmacro beta.preflight ""
!insertmacro beta.preflight "un."

Function .onInit
    !insertmacro wails.checkArchitecture
    SetShellVarContext current
    SetRegView 32
    InitPluginsDir
    File /oname=$PLUGINSDIR\orca-beta-guard.exe "orca-beta-guard.exe"
    StrCpy $CreateDesktopShortcut ${BST_CHECKED}
    ReadRegStr $0 HKCU "${UNINST_KEY}" "DisplayName"
    StrCmp $0 "" choice_done
    IfFileExists "$DESKTOP\ORCA beta.lnk" choice_done
    StrCpy $CreateDesktopShortcut ${BST_UNCHECKED}
choice_done:
FunctionEnd

Function InstallOptionsPage
    !insertmacro MUI_HEADER_TEXT "ORCA beta" "Independent beta installation"
    nsDialogs::Create 1018
    Pop $0
    ${If} $0 == error
        Abort
    ${EndIf}
    ${NSD_CreateCheckbox} 0 6u 100% 20u "Create desktop shortcut"
    Pop $DesktopShortcutCheckbox
    ${NSD_SetState} $DesktopShortcutCheckbox $CreateDesktopShortcut
    ${NSD_CreateLabel} 0 40u 100% 50u "Close ORCA beta before upgrading. Official ORCA remains untouched. Beta data is retained during upgrades and uninstall."
    Pop $0
    nsDialogs::Show
FunctionEnd

Function InstallOptionsPageLeave
    ${NSD_GetState} $DesktopShortcutCheckbox $CreateDesktopShortcut
    Call CheckBetaTarget
FunctionEnd

Section
    SetShellVarContext current
    Call CheckBetaTarget
    !insertmacro wails.webview2runtime
    SetRegView 32
    Call CheckBetaTarget
    ClearErrors
    SetOutPath "$INSTDIR"
    FileOpen $0 "$INSTDIR\.orca-beta-install" w
    FileWrite $0 "ORCA beta"
    FileClose $0
    ${If} ${Errors}
        SetErrorLevel 25
        Abort "Cannot mark this directory as an ORCA beta installation."
    ${EndIf}
    !insertmacro wails.files
    File /oname=node.exe "..\installer-go\payload\node.exe"
    File /oname=LICENSE.node.txt "..\installer-go\payload\LICENSE.node.txt"
    File /oname=THIRD-PARTY-NOTICES.txt "..\..\..\..\THIRD-PARTY-NOTICES.txt"
    SetOutPath "$INSTDIR\codegraph"
    File /r "..\installer-go\payload\codegraph\*.*"
    SetOutPath "$INSTDIR"
    WriteUninstaller "$INSTDIR\uninstall-beta.exe"
    CreateShortcut "$SMPROGRAMS\ORCA beta.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"
    CreateShortcut "$SMPROGRAMS\Uninstall ORCA beta.lnk" "$INSTDIR\uninstall-beta.exe"
    ${If} $CreateDesktopShortcut == ${BST_CHECKED}
        CreateShortcut "$DESKTOP\ORCA beta.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"
    ${Else}
        Delete "$DESKTOP\ORCA beta.lnk"
    ${EndIf}
    WriteRegStr HKCU "${UNINST_KEY}" "Publisher" "${INFO_COMPANYNAME}"
    WriteRegStr HKCU "${UNINST_KEY}" "DisplayName" "ORCA beta"
    WriteRegStr HKCU "${UNINST_KEY}" "DisplayVersion" "3.0.14-beta.2"
    WriteRegStr HKCU "${UNINST_KEY}" "DisplayIcon" "$INSTDIR\${PRODUCT_EXECUTABLE}"
    WriteRegStr HKCU "${UNINST_KEY}" "InstallLocation" "$INSTDIR"
    WriteRegStr HKCU "${UNINST_KEY}" "UninstallString" '$\"$INSTDIR\uninstall-beta.exe$\"'
    WriteRegStr HKCU "${UNINST_KEY}" "QuietUninstallString" '$\"$INSTDIR\uninstall-beta.exe$\" /S'
    ${If} ${Errors}
        SetErrorLevel 25
        Abort "ORCA beta installation could not finish."
    ${EndIf}
SectionEnd

Function un.onInit
    SetShellVarContext current
    SetRegView 32
    InitPluginsDir
    File /oname=$PLUGINSDIR\orca-beta-guard.exe "orca-beta-guard.exe"
FunctionEnd

Section "uninstall"
    SetShellVarContext current
    Call un.CheckBetaTarget
    Delete "$SMPROGRAMS\ORCA beta.lnk"
    Delete "$SMPROGRAMS\Uninstall ORCA beta.lnk"
    Delete "$DESKTOP\ORCA beta.lnk"
    Delete "$INSTDIR\${PRODUCT_EXECUTABLE}"
    Delete "$INSTDIR\node.exe"
    Delete "$INSTDIR\LICENSE.node.txt"
    Delete "$INSTDIR\THIRD-PARTY-NOTICES.txt"
    RMDir /r "$INSTDIR\codegraph"
    Delete "$INSTDIR\.orca-beta-install"
    Delete "$INSTDIR\uninstall-beta.exe"
    DeleteRegKey HKCU "${UNINST_KEY}"
    ; Deliberately retain all beta user data and never recursively remove INSTDIR.
    RMDir "$INSTDIR"
SectionEnd
