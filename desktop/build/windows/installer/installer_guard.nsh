; Native preflight shared by the standard install and uninstall wizards.
!include "WinMessages.nsh"
Var GuardHandle
Var GuardPID
Var GuardCode
Var GuardReady
Var GuardStatus
Var GuardLog
Var GuardMessage
Var GuardFile
Var GuardOwners
Var GuardParent
Var GuardStarted
Var StatusLabel
Var GuardDetails

!macro orca.guard PREFIX
Function ${PREFIX}InitGuard
    InitPluginsDir
    File /oname=$PLUGINSDIR\orca-install-guard.exe "..\installer-go\orca-install-guard.exe"
    File /oname=$PLUGINSDIR\install-files.txt "..\installer-go\install-files.txt"
    File /oname=$PLUGINSDIR\orca.ico "..\icon.ico"
    File /oname=$PLUGINSDIR\license.txt "resources\eula.txt"
    StrCpy $GuardStatus "$PLUGINSDIR\guard.ini"
    System::Call 'kernel32::GetCurrentProcessId() i.s'
    Pop $GuardParent
    CreateDirectory "$LOCALAPPDATA\O.R.C.A\Installer"
    StrCpy $GuardLog "$LOCALAPPDATA\O.R.C.A\Installer\install-$GuardParent.jsonl"
FunctionEnd

Function ${PREFIX}StartGuard
    Delete "$GuardStatus"
    Delete "$PLUGINSDIR\cancel-guard"
    StrCpy $GuardCode -1
    StrCpy $GuardReady 0
    StrCpy $GuardHandle 0
    System::Call 'kernel32::GetTickCount() i.s'
    Pop $GuardStarted
    System::Alloc 68
    Pop $0
    System::Call '*$0(i 68)'
    System::Alloc 16
    Pop $1
    StrCpy $4 '$\"$PLUGINSDIR\orca-install-guard.exe$\" $\"--dir=$INSTDIR$\" $\"--manifest=$PLUGINSDIR\install-files.txt$\" $\"--status=$GuardStatus$\" $\"--log=$GuardLog$\" $\"--cancel=$PLUGINSDIR\cancel-guard$\" --parent=$GuardParent'
    System::Call 'kernel32::CreateProcessW(p 0, w r4, p 0, p 0, i 0, i 0, p 0, p 0, p r0, p r1) i.r2'
    ${If} $2 != 0
        System::Call '*$1(p.r2, p.r3, i.r4, i)'
        StrCpy $GuardHandle $2
        StrCpy $GuardPID $4
        System::Call 'kernel32::CloseHandle(p r3)'
    ${Else}
        StrCpy $GuardCode 23
        StrCpy $GuardMessage "无法启动安装检查程序，请重新下载安装器。"
    ${EndIf}
    System::Free $0
    System::Free $1
FunctionEnd

Function ${PREFIX}ReadGuard
    ${If} $GuardHandle == 0
        Return
    ${EndIf}
    ReadINIStr $GuardMessage "$GuardStatus" "guard" "message"
    ReadINIStr $GuardFile "$GuardStatus" "guard" "file"
    ReadINIStr $GuardOwners "$GuardStatus" "guard" "owners"
    System::Call 'kernel32::WaitForSingleObject(p $GuardHandle, i 0) i.r0'
    ${If} $0 == 0
        System::Call 'kernel32::GetExitCodeProcess(p $GuardHandle, *i.s)'
        Pop $GuardCode
        System::Call 'kernel32::CloseHandle(p $GuardHandle)'
        StrCpy $GuardHandle 0
        ReadINIStr $0 "$GuardStatus" "guard" "code"
        ${If} $0 == ""
        ${OrIf} $0 != $GuardCode
            StrCpy $GuardCode 23
            StrCpy $GuardMessage "检查程序未返回有效结果，请重新检查。"
        ${EndIf}
    ${Else}
        System::Call 'kernel32::GetTickCount() i.r0'
        IntOp $0 $0 - $GuardStarted
        ${If} $0 > 15000
            ; Only our retained helper handle is stopped on timeout, never a PID lookup.
            System::Call 'kernel32::TerminateProcess(p $GuardHandle, i 23)'
            System::Call 'kernel32::CloseHandle(p $GuardHandle)'
            StrCpy $GuardHandle 0
            StrCpy $GuardCode 23
            StrCpy $GuardMessage "安装检查超时，尚未覆盖文件。请重新检查。"
        ${EndIf}
    ${EndIf}
FunctionEnd

Function ${PREFIX}CancelGuard
    ${If} $GuardHandle != 0
        FileOpen $0 "$PLUGINSDIR\cancel-guard" w
        FileClose $0
        System::Call 'kernel32::WaitForSingleObject(p $GuardHandle, i 1500) i.r0'
        ${If} $0 != 0
            System::Call 'kernel32::TerminateProcess(p $GuardHandle, i 24)'
        ${EndIf}
        System::Call 'kernel32::CloseHandle(p $GuardHandle)'
        StrCpy $GuardHandle 0
    ${EndIf}
FunctionEnd

Function ${PREFIX}orca.closeTargetProcesses
    Call ${PREFIX}StartGuard
    ${DoWhile} $GuardHandle != 0
        Sleep 50
        Call ${PREFIX}ReadGuard
    ${Loop}
    ${If} $GuardCode != 0
        SetDetailsPrint both
        DetailPrint "$GuardMessage"
        DetailPrint "$GuardFile $GuardOwners"
        DetailPrint "检查日志：$GuardLog"
        SetErrorLevel $GuardCode
        SetErrors
        Abort "$GuardMessage"
    ${EndIf}
FunctionEnd
!macroend
!insertmacro orca.guard ""
!insertmacro orca.guard "un."

!macro orca.preflightPage PREFIX
Function ${PREFIX}BeginPreflight
    ${If} $GuardReady == 1
        Return
    ${EndIf}
    GetDlgItem $0 $HWNDPARENT 1
    EnableWindow $0 0
    GetDlgItem $0 $HWNDPARENT 3
    EnableWindow $0 0
    ${NSD_SetText} $StatusLabel "正在检查进程和安装文件..."
    ${NSD_SetText} $GuardDetails ""
    ShowWindow $GuardDetails ${SW_HIDE}
    Call ${PREFIX}StartGuard
    ${NSD_CreateTimer} ${PREFIX}PreflightTimer 100
FunctionEnd

Function ${PREFIX}PreflightTimer
    Call ${PREFIX}ReadGuard
    ${NSD_SetText} $StatusLabel "$GuardMessage"
    ${If} $GuardHandle != 0
        Return
    ${EndIf}
    ${NSD_KillTimer} ${PREFIX}PreflightTimer
    GetDlgItem $0 $HWNDPARENT 3
    EnableWindow $0 1
    GetDlgItem $0 $HWNDPARENT 1
    EnableWindow $0 1
    ${If} $GuardCode == 0
        StrCpy $GuardReady 1
        SendMessage $0 ${BM_CLICK} 0 0
    ${Else}
        SendMessage $0 ${WM_SETTEXT} 0 "STR:重新检查"
        ShowWindow $GuardDetails ${SW_SHOW}
        ${NSD_SetText} $GuardDetails "$GuardFile$\r$\n$GuardOwners$\r$\n日志：$GuardLog"
    ${EndIf}
FunctionEnd
!macroend
!insertmacro orca.preflightPage ""
!insertmacro orca.preflightPage "un."

Function InstallOptionsPage
    StrCpy $GuardReady 0
    !insertmacro MUI_HEADER_TEXT "安装选项" "确认选项后，安装器会自动检查并关闭此目录的旧程序。"
    nsDialogs::Create 1018
    Pop $0
    ${If} $0 == error
        Abort
    ${EndIf}
    ${NSD_CreateCheckbox} 0 6u 100% 20u "创建桌面快捷方式"
    Pop $DesktopShortcutCheckbox
    ${NSD_SetState} $DesktopShortcutCheckbox $CreateDesktopShortcut
    ${NSD_CreateLabel} 0 36u 100% 32u "升级将保留配置、会话、草稿和模型文件。$\r$\n如果旧程序没有退出，5 秒后自动结束其残留进程。"
    Pop $0
    ${NSD_CreateLabel} 0 80u 100% 30u "没有运行中的程序且文件可替换时，检查会立即完成。"
    Pop $StatusLabel
    ${NSD_CreateMLText} 0 114u 100% 53u ""
    Pop $GuardDetails
    SendMessage $GuardDetails ${EM_SETREADONLY} 1 0
    ShowWindow $GuardDetails ${SW_HIDE}
    nsDialogs::Show
FunctionEnd

Function InstallOptionsPageLeave
    ${NSD_GetState} $DesktopShortcutCheckbox $CreateDesktopShortcut
    ${If} $GuardReady == 1
        Return
    ${EndIf}
    Call BeginPreflight
    Abort
FunctionEnd

Function un.onInit
    SetShellVarContext current
    Call un.InitGuard
FunctionEnd

Function un.DeleteDataPage
    StrCpy $GuardReady 0
    !insertmacro MUI_HEADER_TEXT "保留用户数据" "默认保留配置、会话、草稿和模型文件。"
    nsDialogs::Create 1018
    Pop $0
    ${If} $0 == error
        Abort
    ${EndIf}
    ${NSD_CreateCheckbox} 0 6u 100% 32u "删除配置、会话、记忆、缓存等保存的数据（无法撤销）"
    Pop $DeleteSavedDataCheckbox
    ${NSD_Uncheck} $DeleteSavedDataCheckbox
    ${NSD_CreateLabel} 0 52u 100% 28u "不勾选时，重新安装后可继续使用原有数据。"
    Pop $0
    ${NSD_CreateLabel} 0 84u 100% 28u "卸载前会检查并关闭此目录的程序。"
    Pop $StatusLabel
    ${NSD_CreateMLText} 0 114u 100% 53u ""
    Pop $GuardDetails
    SendMessage $GuardDetails ${EM_SETREADONLY} 1 0
    ShowWindow $GuardDetails ${SW_HIDE}
    nsDialogs::Show
FunctionEnd

Function un.DeleteDataPageLeave
    ${NSD_GetState} $DeleteSavedDataCheckbox $DeleteSavedData
    ${If} $GuardReady == 1
        Return
    ${EndIf}
    Call un.BeginPreflight
    Abort
FunctionEnd
