; Time Tracking Agent NSIS Installer Script
; Requires NSIS 3.0 or later

!define PRODUCT_NAME "Averox Time Tracking Agent"
!define PRODUCT_VERSION "1.0.0"
!define PRODUCT_PUBLISHER "Averox Pvt ltd"
!define PRODUCT_WEB_SITE "https://desktime.averox.com"
!define PRODUCT_UNINST_KEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\${PRODUCT_NAME}"
!define PRODUCT_UNINST_ROOT_KEY "HKLM"
!define PRODUCT_STARTMENU_REGVAL "NSIS:StartMenuDir"

; Installer attributes
Name "${PRODUCT_NAME}"
OutFile "TimeTrackingAgent-Setup-${PRODUCT_VERSION}.exe"
InstallDir "$LOCALAPPDATA\TimeTrackingAgent"
RequestExecutionLevel user ; User-level install (no admin required)
ShowInstDetails show
ShowUnInstDetails show

; Modern UI
!include "MUI2.nsh"

; Interface Settings
!define MUI_ABORTWARNING
!define MUI_ICON "${NSISDIR}\Contrib\Graphics\Icons\modern-install.ico"
!define MUI_UNICON "${NSISDIR}\Contrib\Graphics\Icons\modern-uninstall.ico"

; Welcome page
!insertmacro MUI_PAGE_WELCOME

; License page (optional - comment out if you don't have a license)
; !insertmacro MUI_PAGE_LICENSE "license.txt"

; Components page
!insertmacro MUI_PAGE_COMPONENTS

; Directory page
!insertmacro MUI_PAGE_DIRECTORY

; Start menu folder page
var ICONS_GROUP
!define MUI_STARTMENUPAGE_REGISTRY_ROOT "HKCU"
!define MUI_STARTMENUPAGE_REGISTRY_KEY "Software\${PRODUCT_NAME}"
!define MUI_STARTMENUPAGE_REGISTRY_VALUENAME "${PRODUCT_STARTMENU_REGVAL}"
!insertmacro MUI_PAGE_STARTMENU Application $ICONS_GROUP

; Instfiles page
!insertmacro MUI_PAGE_INSTFILES

; Finish page
!define MUI_FINISHPAGE_RUN "$INSTDIR\bin\time-tracking.exe"
!define MUI_FINISHPAGE_RUN_TEXT "Start Time Tracking Agent"
!insertmacro MUI_PAGE_FINISH

; Uninstaller pages
!insertmacro MUI_UNPAGE_INSTFILES

; Language files
!insertmacro MUI_LANGUAGE "English"

; Reserve files
!insertmacro MUI_RESERVEFILE_LANGDLL

; MUI end

; Installer sections
Section "!Time Tracking Agent" SecMain
    SectionIn RO ; Read-only (required)
    
    SetOutPath "$INSTDIR"
    
    ; Create directories
    CreateDirectory "$INSTDIR\bin"
    CreateDirectory "$INSTDIR\config"
    CreateDirectory "$INSTDIR\logs"
    CreateDirectory "$INSTDIR\storage"
    
    ; Copy agent binary into bin subfolder
    SetOutPath "$INSTDIR\bin"
    File "..\bin\time-tracking.exe"
    
    ; Copy config template (only if config doesn't exist)
    SetOutPath "$INSTDIR"
    IfFileExists "$INSTDIR\config\config.yaml" SkipConfigCopy
        File "config-template.yaml"
        Rename "$INSTDIR\config-template.yaml" "$INSTDIR\config\config.yaml"
    SkipConfigCopy:
    
    ; Create Start Menu shortcuts
    !insertmacro MUI_STARTMENU_WRITE_BEGIN Application
        CreateDirectory "$SMPROGRAMS\$ICONS_GROUP"
        CreateShortcut "$SMPROGRAMS\$ICONS_GROUP\Time Tracking Agent.lnk" "$INSTDIR\bin\time-tracking.exe"
        CreateShortcut "$SMPROGRAMS\$ICONS_GROUP\Uninstall.lnk" "$INSTDIR\Uninstall.exe"
    !insertmacro MUI_STARTMENU_WRITE_END
    
    ; Create uninstaller
    WriteUninstaller "$INSTDIR\Uninstall.exe"
    
    ; Write registry keys for Add/Remove Programs
    WriteRegStr HKCU "Software\${PRODUCT_NAME}" "" $INSTDIR
    WriteRegStr ${PRODUCT_UNINST_ROOT_KEY} "${PRODUCT_UNINST_KEY}" "DisplayName" "$(^Name)"
    WriteRegStr ${PRODUCT_UNINST_ROOT_KEY} "${PRODUCT_UNINST_KEY}" "UninstallString" "$INSTDIR\Uninstall.exe"
    WriteRegStr ${PRODUCT_UNINST_ROOT_KEY} "${PRODUCT_UNINST_KEY}" "DisplayIcon" "$INSTDIR\bin\time-tracking.exe"
    WriteRegStr ${PRODUCT_UNINST_ROOT_KEY} "${PRODUCT_UNINST_KEY}" "DisplayVersion" "${PRODUCT_VERSION}"
    WriteRegStr ${PRODUCT_UNINST_ROOT_KEY} "${PRODUCT_UNINST_KEY}" "URLInfoAbout" "${PRODUCT_WEB_SITE}"
    WriteRegStr ${PRODUCT_UNINST_ROOT_KEY} "${PRODUCT_UNINST_KEY}" "Publisher" "${PRODUCT_PUBLISHER}"
    
SectionEnd

Section "Start with Windows" SecStartup
    ; Add to startup (registry Run key)
    WriteRegStr HKCU "Software\Microsoft\Windows\CurrentVersion\Run" "TimeTrackingAgent" "$INSTDIR\bin\time-tracking.exe"
SectionEnd

Section "Desktop Shortcut" SecDesktop
    CreateShortcut "$DESKTOP\Time Tracking Agent.lnk" "$INSTDIR\bin\time-tracking.exe"
SectionEnd

; Section descriptions
!insertmacro MUI_FUNCTION_DESCRIPTION_BEGIN
    !insertmacro MUI_DESCRIPTION_TEXT ${SecMain} "Core Time Tracking Agent files (required)"
    !insertmacro MUI_DESCRIPTION_TEXT ${SecStartup} "Automatically start Time Tracking Agent when Windows starts"
    !insertmacro MUI_DESCRIPTION_TEXT ${SecDesktop} "Create a desktop shortcut for easy access"
!insertmacro MUI_FUNCTION_DESCRIPTION_END

; Uninstaller section
Section "Uninstall"
    ; Stop agent if running
    ExecWait 'taskkill /F /IM time-tracking.exe /T' $0
    
    ; Remove files
    Delete "$INSTDIR\bin\time-tracking.exe"
    Delete "$INSTDIR\time-tracking.exe"  ; Clean up from older installer versions
    Delete "$INSTDIR\config\config.yaml"
    Delete "$INSTDIR\Uninstall.exe"
    
    ; Remove directories (only if empty or user chooses to remove data)
    RMDir /r "$INSTDIR\bin"
    ; Keep config, logs, and storage by default (user data)
    ; RMDir /r "$INSTDIR\config"
    ; RMDir /r "$INSTDIR\logs"
    ; RMDir /r "$INSTDIR\storage"
    RMDir "$INSTDIR"
    
    ; Remove Start Menu shortcuts
    !insertmacro MUI_STARTMENU_GETFOLDER Application $ICONS_GROUP
    Delete "$SMPROGRAMS\$ICONS_GROUP\Time Tracking Agent.lnk"
    Delete "$SMPROGRAMS\$ICONS_GROUP\Uninstall.lnk"
    RMDir "$SMPROGRAMS\$ICONS_GROUP"
    
    ; Remove desktop shortcut
    Delete "$DESKTOP\Time Tracking Agent.lnk"
    
    ; Remove startup entry
    DeleteRegValue HKCU "Software\Microsoft\Windows\CurrentVersion\Run" "TimeTrackingAgent"
    
    ; Remove registry keys
    DeleteRegKey HKCU "Software\${PRODUCT_NAME}"
    DeleteRegKey ${PRODUCT_UNINST_ROOT_KEY} "${PRODUCT_UNINST_KEY}"
    
    SetAutoClose true
SectionEnd






