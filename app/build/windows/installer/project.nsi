Unicode true

####
## Please note: Template replacements don't work in this file. They are provided with default defines like
## mentioned underneath.
## If the keyword is not defined, "wails_tools.nsh" will populate them with the values from ProjectInfo.
## If they are defined here, "wails_tools.nsh" will not touch them. This allows to use this project.nsi manually
## from outside of Wails for debugging and development of the installer.
##
## For development first make a wails nsis build to populate the "wails_tools.nsh":
## > wails build --target windows/amd64 --nsis
## Then you can call makensis on this file with specifying the path to your binary:
## For a AMD64 only installer:
## > makensis -DARG_WAILS_AMD64_BINARY=..\..\bin\app.exe
## For a ARM64 only installer:
## > makensis -DARG_WAILS_ARM64_BINARY=..\..\bin\app.exe
## For a installer with both architectures:
## > makensis -DARG_WAILS_AMD64_BINARY=..\..\bin\app-amd64.exe -DARG_WAILS_ARM64_BINARY=..\..\bin\app-arm64.exe
####
## The following information is taken from the ProjectInfo file, but they can be overwritten here.
####
## !define INFO_PROJECTNAME    "MyProject" # Default "{{.Name}}"
## !define INFO_COMPANYNAME    "MyCompany" # Default "{{.Info.CompanyName}}"
## !define INFO_PRODUCTNAME    "MyProduct" # Default "{{.Info.ProductName}}"
## !define INFO_PRODUCTVERSION "1.0.0"     # Default "{{.Info.ProductVersion}}"
## !define INFO_COPYRIGHT      "Copyright" # Default "{{.Info.Copyright}}"
###
## !define PRODUCT_EXECUTABLE  "Application.exe"      # Default "${INFO_PROJECTNAME}.exe"
## !define UNINST_KEY_NAME     "UninstKeyInRegistry"  # Default "${INFO_COMPANYNAME}${INFO_PRODUCTNAME}"
####
## !define REQUEST_EXECUTION_LEVEL "admin"            # Default "admin"  see also https://nsis.sourceforge.io/Docs/Chapter4.html
####
## Include the wails tools
####
!include "wails_tools.nsh"
!include "WordFunc.nsh"
!insertmacro VersionCompare
!define WEBVIEW2_MINIMUM_VERSION "94.0.992.31"

# Wails' stock macro starts the Microsoft bootstrapper but ignores its exit
# status, so setup can report success even when the required runtime failed.
# Keep the same per-machine/per-user detection while making failure visible.
# Microsoft documents the registry version as the authoritative check for an
# installed runtime, per-machine first and then per-user. Jumps to webview_ok,
# which the including macro defines.
!macro shogun2sync.webview2registrycheck
    ReadRegStr $0 HKLM "SOFTWARE\WOW6432Node\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}" "pv"
    ${If} $0 != ""
    ${AndIf} $0 != "0.0.0.0"
        ${VersionCompare} "$0" "${WEBVIEW2_MINIMUM_VERSION}" $1
        ${If} $1 != 2
            Goto webview_ok
        ${EndIf}
    ${EndIf}

    ReadRegStr $0 HKCU "Software\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}" "pv"
    ${If} $0 != ""
    ${AndIf} $0 != "0.0.0.0"
        ${VersionCompare} "$0" "${WEBVIEW2_MINIMUM_VERSION}" $1
        ${If} $1 != 2
            Goto webview_ok
        ${EndIf}
    ${EndIf}
!macroend

# Reports a fatal condition. MessageBox is not suppressed by /S, so a silent
# install that hit one of these would hang on a dialog nobody can click.
!macro shogun2sync.webview2fail message
    SetDetailsPrint both
    DetailPrint "${message}"
    ${IfNot} ${Silent}
        MessageBox MB_OK|MB_ICONSTOP "${message}"
    ${EndIf}
!macroend

!macro shogun2sync.webview2runtime
    SetRegView 64
    !insertmacro shogun2sync.webview2registrycheck

    SetDetailsPrint both
    DetailPrint "Installing: Microsoft Edge WebView2 Runtime"
    SetDetailsPrint listonly
    InitPluginsDir
    CreateDirectory "$pluginsdir\webview2bootstrapper"
    SetOutPath "$pluginsdir\webview2bootstrapper"
    File "tmp\MicrosoftEdgeWebView2RuntimeInstallerX64.exe"
    ClearErrors
    ExecWait '"$pluginsdir\webview2bootstrapper\MicrosoftEdgeWebView2RuntimeInstallerX64.exe" /silent /install' $2
    ${If} ${Errors}
        !insertmacro shogun2sync.webview2fail "Windows could not start the bundled WebView2 setup. Restart Windows and run this installer again."
        SetErrorLevel 1
        Abort
    ${EndIf}
    SetDetailsPrint both

    # Do not trust a subprocess exit alone: Microsoft documents the registry
    # version as the authoritative installed-runtime check, and setup is known
    # to return before it has finished (WebView2Feedback issue 1349). Poll for a
    # minute so a slow machine is not failed for a race, and so an "already
    # installed" nonzero exit is not treated as fatal.
    StrCpy $3 0
    webview_poll:
    !insertmacro shogun2sync.webview2registrycheck
    IntOp $3 $3 + 1
    ${If} $3 < 30
        Sleep 2000
        Goto webview_poll
    ${EndIf}

    !insertmacro shogun2sync.webview2fail "The bundled offline WebView2 setup did not install the required runtime (setup exit code $2). Restart Windows and run setup again."
    ${If} $2 == 0
        SetErrorLevel 1
    ${Else}
        SetErrorLevel $2
    ${EndIf}
    Abort

    webview_ok:
!macroend

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

!include "MUI.nsh"

!define MUI_ICON "..\icon.ico"
!define MUI_UNICON "..\icon.ico"
# !define MUI_WELCOMEFINISHPAGE_BITMAP "resources\leftimage.bmp" #Include this to add a bitmap on the left side of the Welcome Page. Must be a size of 164x314
!define MUI_FINISHPAGE_NOAUTOCLOSE # Wait on the INSTFILES page so the user can take a look into the details of the installation steps
!define MUI_FINISHPAGE_RUN "$INSTDIR\${PRODUCT_EXECUTABLE}"
!define MUI_ABORTWARNING # This will warn the user if they exit from the installer.

!insertmacro MUI_PAGE_WELCOME # Welcome to the installer page.
!insertmacro MUI_PAGE_LICENSE "..\..\..\..\LICENSE"
!insertmacro MUI_PAGE_LICENSE "..\..\..\packaging\WebView2-NOTICE.txt"
!insertmacro MUI_PAGE_DIRECTORY # In which folder install page.
!insertmacro MUI_PAGE_INSTFILES # Installing page.
!insertmacro MUI_PAGE_FINISH # Finished installation page.

!insertmacro MUI_UNPAGE_INSTFILES # Uinstalling page

!insertmacro MUI_LANGUAGE "English" # Set the Language of the installer

## The following two statements can be used to sign the installer and the uninstaller. The path to the binaries are provided in %1
#!uninstfinalize 'signtool --file "%1"'
#!finalize 'signtool --file "%1"'

Name "${INFO_PRODUCTNAME}"
OutFile "..\..\bin\${INFO_PROJECTNAME}-${ARCH}-installer.exe" # Name of the installer's file.
!ifdef WAILS_INSTALL_SCOPE
  !if "${WAILS_INSTALL_SCOPE}" == "user"
    InstallDir "$LOCALAPPDATA\Programs\${INFO_PRODUCTNAME}"
  !else
    InstallDir "$PROGRAMFILES64\${INFO_COMPANYNAME}\${INFO_PRODUCTNAME}"
  !endif
!else
  InstallDir "$PROGRAMFILES64\${INFO_COMPANYNAME}\${INFO_PRODUCTNAME}"
!endif # Default installing folder ($PROGRAMFILES is Program Files folder).
ShowInstDetails show # This will always show the installation details.

Function .onInit
   !insertmacro wails.checkArchitecture
FunctionEnd

Section
    !insertmacro wails.setShellContext

    !insertmacro shogun2sync.webview2runtime

    SetOutPath $INSTDIR

    !insertmacro wails.files
    # rclone drives every sync. Windows has no package manager to pull it
    # from, and the app looks for it next to its own executable, so the
    # checksum-verified copy must land here or a clean install cannot sync.
    File "/oname=rclone.exe" "..\..\bin\rclone.exe"
    File "/oname=LICENSE.txt" "..\..\..\..\LICENSE"
    File "/oname=rclone-COPYING.txt" "..\..\..\packaging\rclone-COPYING"
    File "/oname=WebView2-LICENSE.html" "tmp\WebView2-LICENSE.html"
    File "/oname=GO-THIRD-PARTY-NOTICES.txt" "tmp\GO-THIRD-PARTY-NOTICES.txt"

    # Fail the install if the private sync engine did not land runnable.
    # A successful UI with no working rclone is worse than a loud abort.
    SetDetailsPrint both
    DetailPrint "Verifying bundled rclone..."
    ClearErrors
    ExecWait '"$INSTDIR\rclone.exe" version' $0
    ${If} ${Errors}
    ${OrIf} $0 != 0
        !insertmacro shogun2sync.webview2fail "The installer could not run the bundled rclone.exe (exit $0). Antivirus may have blocked it — allow the file and re-run setup."
        SetErrorLevel 1
        Abort
    ${EndIf}

    CreateShortcut "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"
    CreateShortCut "$DESKTOP\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"

    !insertmacro wails.associateFiles
    !insertmacro wails.associateCustomProtocols

    !insertmacro wails.writeUninstaller
SectionEnd

Section "uninstall"
    !insertmacro wails.setShellContext

    RMDir /r "$AppData\${PRODUCT_EXECUTABLE}" # Remove the WebView2 DataPath

    # RMDir /r below already clears the tree, but delete the bundled
    # third-party binary and its notice by name first: leaving a stray rclone
    # copy behind after an uninstall is exactly the kind of orphaned tool a
    # user would never think to look for.
    Delete "$INSTDIR\rclone.exe"
    Delete "$INSTDIR\rclone-COPYING.txt"

    RMDir /r $INSTDIR

    Delete "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk"
    Delete "$DESKTOP\${INFO_PRODUCTNAME}.lnk"

    !insertmacro wails.unassociateFiles
    !insertmacro wails.unassociateCustomProtocols

    !insertmacro wails.deleteUninstaller
SectionEnd
