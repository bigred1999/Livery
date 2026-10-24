; Livery installer (NSIS 3). Per-user install, no admin needed for Livery
; itself. OpenRGB and PawnIO are optional and are downloaded from their
; official release pages, checked against pinned SHA-256 hashes, and run with
; their own installers (each asks for admin through Windows' UAC prompt).
Unicode true
ManifestDPIAware true
SetCompressor /SOLID lzma

!ifndef VERSION
  !define VERSION "0.0.0"
!endif
!define APP "Livery"
!define UNINST_KEY "Software\Microsoft\Windows\CurrentVersion\Uninstall\Livery"

!define ORGB_VER "1.1"
!define ORGB_URL "https://codeberg.org/OpenRGB/OpenRGB/releases/download/release_1.1/OpenRGB_1.1_Windows_64_8e16de2.msi"
!define ORGB_SHA "a4ce48a5e1532750ad4f5248424d42f7f2c710775cee88db070ccdb63c549caa"
!define PAWN_VER "2.2.0"
!define PAWN_URL "https://github.com/namazso/PawnIO.Setup/releases/download/2.2.0/PawnIO_setup.exe"
!define PAWN_SHA "1f519a22e47187f70a1379a48ca604981c4fcf694f4e65b734aaa74a9fba3032"

Name "${APP}"
OutFile "..\build\Livery-Setup-${VERSION}.exe"
InstallDir "$LOCALAPPDATA\Programs\Livery"
InstallDirRegKey HKCU "${UNINST_KEY}" "InstallLocation"
RequestExecutionLevel user
BrandingText "Livery ${VERSION}"
VIProductVersion "${VERSION}.0"
VIAddVersionKey "ProductName" "Livery"
VIAddVersionKey "FileDescription" "Livery Setup"
VIAddVersionKey "FileVersion" "${VERSION}"
VIAddVersionKey "LegalCopyright" "MIT License"

!include "MUI2.nsh"
!include "LogicLib.nsh"
!include "Sections.nsh"
!include "x64.nsh"
!include "FileFunc.nsh"

!define MUI_ICON "livery.ico"
!define MUI_UNICON "livery.ico"
!define MUI_ABORTWARNING
!define MUI_COMPONENTSPAGE_SMALLDESC
!define MUI_WELCOMEPAGE_TEXT "Livery paints every RGB light on your PC with one motorsport livery: Alienware laptops, Glorious and Logitech gear built in, and everything else through OpenRGB.$\r$\n$\r$\nSetup can also download and install OpenRGB for motherboards, RAM, graphics cards, fans and LED strips."
!define MUI_FINISHPAGE_RUN "$INSTDIR\livery.exe"
!define MUI_FINISHPAGE_RUN_TEXT "Open the Livery control panel"

!insertmacro MUI_PAGE_WELCOME
!insertmacro MUI_PAGE_COMPONENTS
!insertmacro MUI_PAGE_DIRECTORY
!insertmacro MUI_PAGE_INSTFILES
!insertmacro MUI_PAGE_FINISH
!insertmacro MUI_UNPAGE_CONFIRM
!insertmacro MUI_UNPAGE_INSTFILES
!insertmacro MUI_LANGUAGE "English"

Var Fetched

; ---------------------------------------------------------------------------
Section "Livery" SecApp
  SectionIn RO
  ; Stop a running Livery (any copy) so its files can be replaced.
  InitPluginsDir
  File "/oname=$PLUGINSDIR\livery.exe" "..\build\livery.exe"
  ; Bring settings over from a portable or older copy (never overwrites).
  nsExec::Exec '"$PLUGINSDIR\livery.exe" import-settings "$INSTDIR"'
  Pop $0
  nsExec::Exec '"$PLUGINSDIR\livery.exe" stop'
  Pop $0
  Sleep 1000

  SetOutPath "$INSTDIR"
  File "..\build\livery.exe"
  File "/oname=LICENSE.txt" "..\LICENSE"
  File "/oname=THIRD_PARTY_NOTICES.txt" "..\THIRD_PARTY_NOTICES.md"
  WriteUninstaller "$INSTDIR\Uninstall.exe"

  CreateShortcut "$SMPROGRAMS\Livery.lnk" "$INSTDIR\livery.exe" "" "$INSTDIR\livery.exe" 0 SW_SHOWNORMAL "" "Livery RGB control panel"

  WriteRegStr HKCU "${UNINST_KEY}" "DisplayName" "Livery"
  WriteRegStr HKCU "${UNINST_KEY}" "DisplayVersion" "${VERSION}"
  WriteRegStr HKCU "${UNINST_KEY}" "Publisher" "Livery contributors"
  WriteRegStr HKCU "${UNINST_KEY}" "URLInfoAbout" "https://github.com/bigred1999/Livery"
  WriteRegStr HKCU "${UNINST_KEY}" "InstallLocation" "$INSTDIR"
  WriteRegStr HKCU "${UNINST_KEY}" "DisplayIcon" "$INSTDIR\Uninstall.exe"
  WriteRegStr HKCU "${UNINST_KEY}" "UninstallString" '"$INSTDIR\Uninstall.exe"'
  WriteRegStr HKCU "${UNINST_KEY}" "QuietUninstallString" '"$INSTDIR\Uninstall.exe" /S'
  WriteRegDWORD HKCU "${UNINST_KEY}" "NoModify" 1
  WriteRegDWORD HKCU "${UNINST_KEY}" "NoRepair" 1
  WriteRegDWORD HKCU "${UNINST_KEY}" "EstimatedSize" 7200
SectionEnd

Section "OpenRGB ${ORGB_VER} (download, about 23 MB)" SecOpenRGB
  DetailPrint "Downloading OpenRGB ${ORGB_VER} from codeberg.org..."
  File "/oname=$PLUGINSDIR\fetch.ps1" "fetch.ps1"
  StrCpy $Fetched "$PLUGINSDIR\OpenRGB_${ORGB_VER}.msi"
  nsExec::ExecToLog 'powershell.exe -NoProfile -ExecutionPolicy Bypass -File "$PLUGINSDIR\fetch.ps1" -Url "${ORGB_URL}" -Out "$Fetched" -Sha256 ${ORGB_SHA}'
  Pop $0
  ${If} $0 != 0
    MessageBox MB_ICONEXCLAMATION|MB_YESNO "OpenRGB could not be downloaded (code $0).$\r$\n$\r$\nLivery still works for its built-in devices. Open the OpenRGB download page to install it yourself?" IDNO orgb_done
    ExecShell "open" "https://openrgb.org/releases.html"
    Goto orgb_done
  ${EndIf}
  DetailPrint "Installing OpenRGB (Windows will ask for permission)..."
  ; ADDLOCAL=ALL also registers OpenRGB's background service, which runs the
  ; SDK server Livery talks to.
  ExecWait 'msiexec.exe /i "$Fetched" /passive /norestart ADDLOCAL=ALL' $0
  ${If} $0 == 0
  ${OrIf} $0 == 3010
    DetailPrint "OpenRGB installed."
  ${Else}
    DetailPrint "OpenRGB setup ended with code $0."
    MessageBox MB_ICONEXCLAMATION "OpenRGB was not installed (code $0). You can run Setup again or install it from openrgb.org."
  ${EndIf}
  orgb_done:
SectionEnd

Section "PawnIO driver ${PAWN_VER} (for RAM and some motherboards)" SecPawnIO
  DetailPrint "Downloading PawnIO ${PAWN_VER} from github.com..."
  File "/oname=$PLUGINSDIR\fetch.ps1" "fetch.ps1"
  StrCpy $Fetched "$PLUGINSDIR\PawnIO_setup.exe"
  nsExec::ExecToLog 'powershell.exe -NoProfile -ExecutionPolicy Bypass -File "$PLUGINSDIR\fetch.ps1" -Url "${PAWN_URL}" -Out "$Fetched" -Sha256 ${PAWN_SHA}'
  Pop $0
  ${If} $0 != 0
    MessageBox MB_ICONEXCLAMATION|MB_YESNO "PawnIO could not be downloaded (code $0). Open its download page?" IDNO pawn_done
    ExecShell "open" "https://pawnio.eu"
    Goto pawn_done
  ${EndIf}
  DetailPrint "Installing PawnIO (Windows will ask for permission)..."
  ExecShellWait "open" "$Fetched" "-install"
  pawn_done:
SectionEnd

Section "Start Livery when I sign in" SecAutostart
  nsExec::Exec '"$INSTDIR\livery.exe" install'
  Pop $0
SectionEnd

Section /o "Desktop shortcut" SecDesktop
  CreateShortcut "$DESKTOP\Livery.lnk" "$INSTDIR\livery.exe"
SectionEnd

; ---------------------------------------------------------------------------
LangString DESC_App ${LANG_ENGLISH} "The Livery app and control panel (installed for your account only)."
LangString DESC_Auto ${LANG_ENGLISH} "Keep your lights painted after restarts. You can turn this off in the control panel."
LangString DESC_Desk ${LANG_ENGLISH} "Put a Livery shortcut on the desktop."
LangString DESC_ORGB ${LANG_ENGLISH} "Lets Livery paint motherboards, RAM, graphics cards, fans and LED strips. Downloads the official installer (GPL-2.0) and sets up its background service."
LangString DESC_Pawn ${LANG_ENGLISH} "Driver OpenRGB needs on Windows to reach RAM and some motherboard lighting."
!insertmacro MUI_FUNCTION_DESCRIPTION_BEGIN
  !insertmacro MUI_DESCRIPTION_TEXT ${SecApp} $(DESC_App)
  !insertmacro MUI_DESCRIPTION_TEXT ${SecAutostart} $(DESC_Auto)
  !insertmacro MUI_DESCRIPTION_TEXT ${SecDesktop} $(DESC_Desk)
  !insertmacro MUI_DESCRIPTION_TEXT ${SecOpenRGB} $(DESC_ORGB)
  !insertmacro MUI_DESCRIPTION_TEXT ${SecPawnIO} $(DESC_Pawn)
!insertmacro MUI_FUNCTION_DESCRIPTION_END

; Updates started from inside Livery pass /S /RELAUNCH: start Livery again
; in the background once the new files are in place.
Function .onInstSuccess
  ${GetParameters} $R0
  ClearErrors
  ${GetOptions} $R0 "/RELAUNCH" $R1
  ${IfNot} ${Errors}
    Exec '"$INSTDIR\livery.exe" run'
  ${EndIf}
FunctionEnd

Function .onInit
  ${IfNot} ${RunningX64}
    MessageBox MB_ICONSTOP "Livery needs 64-bit Windows 10 or 11."
    Abort
  ${EndIf}
  ; Skip OpenRGB / PawnIO when they're already installed.
  ${If} ${FileExists} "$PROGRAMFILES64\OpenRGB\OpenRGB.exe"
  ${OrIf} ${FileExists} "$LOCALAPPDATA\Programs\OpenRGB\OpenRGB.exe"
    !insertmacro UnselectSection ${SecOpenRGB}
    SectionSetText ${SecOpenRGB} "OpenRGB (already installed)"
  ${EndIf}
  SetRegView 64
  ReadRegStr $0 HKLM "Software\Microsoft\Windows\CurrentVersion\Uninstall\PawnIO" "InstallLocation"
  SetRegView default
  ${If} $0 != ""
    !insertmacro UnselectSection ${SecPawnIO}
    SectionSetText ${SecPawnIO} "PawnIO (already installed)"
  ${EndIf}
FunctionEnd

; ---------------------------------------------------------------------------
Section "Uninstall"
  ; Stops background mode and removes the sign-in entry.
  nsExec::Exec '"$INSTDIR\livery.exe" uninstall'
  Pop $0
  ; Wait for background mode to let go of the exe.
  StrCpy $1 0
  ${Do}
    ClearErrors
    Delete "$INSTDIR\livery.exe"
    ${IfNot} ${Errors}
      ${Break}
    ${EndIf}
    IntOp $1 $1 + 1
    ${If} $1 >= 10
      Delete /REBOOTOK "$INSTDIR\livery.exe"
      ${Break}
    ${EndIf}
    Sleep 500
  ${Loop}
  Delete "$INSTDIR\LICENSE.txt"
  Delete "$INSTDIR\THIRD_PARTY_NOTICES.txt"
  Delete "$INSTDIR\config.json"
  RMDir /r "$INSTDIR\logs"
  Delete "$INSTDIR\Uninstall.exe"
  RMDir "$INSTDIR"
  Delete "$SMPROGRAMS\Livery.lnk"
  Delete "$DESKTOP\Livery.lnk"
  DeleteRegKey HKCU "${UNINST_KEY}"
  ; OpenRGB and PawnIO are left installed; remove them from Windows Settings > Apps if you like.
SectionEnd
