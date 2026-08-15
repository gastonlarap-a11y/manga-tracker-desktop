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
## Installed for one user, without elevation. Wails defaults this to "admin", and what that
## elevation actually covered is three files in Program Files and two shortcuts: `wails.files`
## copies the app binary and nothing else. The payload is extracted, and the service registered,
## by the app itself on first launch — a separate process, started later from a shortcut, which
## runs asInvoker because build/windows/wails.exe.manifest declares no requestedExecutionLevel.
## This installer never hands it a token, so nothing about that path changes here.
##
## What asking for administrator did cost is two things. Someone saw a yellow UAC prompt naming
## an "Unknown publisher" before the app had drawn a single pixel. And elevation, writing
## executables and registering something that starts at login are together the shape a heuristic
## scanner reads as a dropper — an installer with this little to install has no reason to
## imitate one.
##
## Defined before the include on purpose: every value in wails_tools.nsh sits behind !ifndef,
## and that file regenerates on every `wails build`, so this is the only place the choice
## survives.
!define REQUEST_EXECUTION_LEVEL "user"
####
## Include the wails tools
####
!include "wails_tools.nsh"

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
!define MUI_ABORTWARNING # This will warn the user if they exit from the installer.

!insertmacro MUI_PAGE_WELCOME # Welcome to the installer page.
# !insertmacro MUI_PAGE_LICENSE "resources\eula.txt" # Adds a EULA page to the installer
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
# Not Program Files: writing there is what would need the elevation this installer no longer
# asks for. $LOCALAPPDATA\Programs is where Windows expects a per-user install to land.
InstallDir "$LOCALAPPDATA\Programs\${INFO_PRODUCTNAME}"
ShowInstDetails show # This will always show the installation details.

Function .onInit
   # Releases up to v0.1.9 installed into Program Files and registered themselves under HKLM,
   # from an installer that asked for elevation. Nothing removes that on its own, and installing
   # beside it would leave two entries in "Add or remove programs", two shortcuts, and two 96 MB
   # copies of the same app — both of them driving the same backend, since the library and the
   # payload live in the user's data directory and neither installer touches them.
   #
   # So it is found and handed back to its own uninstaller, which still has the elevation needed
   # to delete what it wrote. Launched rather than awaited: a NSIS uninstaller copies itself to
   # a temporary directory and returns immediately, so ExecWait would come back before anything
   # was removed and any check after it would read the old state and lie.
   SetRegView 64
   ReadRegStr $0 HKLM "${UNINST_KEY}" "UninstallString"
   ${If} $0 != ""
       MessageBox MB_YESNO|MB_ICONQUESTION "Hay una versión anterior instalada para todos los usuarios de esta computadora.$\n$\nHay que quitarla antes de instalar esta, y Windows va a pedir permiso de administrador una última vez para hacerlo. Tu biblioteca no se toca: vive aparte.$\n$\n¿Abrir el desinstalador ahora?" IDNO abortInstall
       Exec $0
       MessageBox MB_OK|MB_ICONINFORMATION "Cuando termine de desinstalarse, volvé a ejecutar este instalador."
       abortInstall:
       Abort
   ${EndIf}

   !insertmacro wails.checkArchitecture
FunctionEnd

Section
    !insertmacro wails.setShellContext

    !insertmacro wails.webview2runtime

    SetOutPath $INSTDIR

    !insertmacro wails.files

    CreateShortcut "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"
    CreateShortCut "$DESKTOP\${INFO_PRODUCTNAME}.lnk" "$INSTDIR\${PRODUCT_EXECUTABLE}"

    !insertmacro wails.associateFiles
    !insertmacro wails.associateCustomProtocols

    # Spelled out instead of `!insertmacro wails.writeUninstaller`, and this is the one place
    # where that matters. That macro writes every one of these values to HKLM literally
    # (wails_tools.nsh:118-127), without ever consulting REQUEST_EXECUTION_LEVEL — so without
    # elevation it fails, and NSIS does not abort on a failed WriteRegStr. The install would
    # look like it worked and leave nothing in "Add or remove programs": a silent failure, which
    # is the kind this project refuses to ship.
    #
    # SHELL_CONTEXT is the same pseudo-root the file association macros use, and it already
    # follows wails.setShellContext above — HKCU here, and HKLM again if this installer ever
    # goes back to asking for elevation.
    #
    # If Wails is upgraded, compare this block against the macro it replaces: a field added
    # there would be missed here, and nothing would complain.
    WriteUninstaller "$INSTDIR\uninstall.exe"

    SetRegView 64
    WriteRegStr SHELL_CONTEXT "${UNINST_KEY}" "Publisher" "${INFO_COMPANYNAME}"
    WriteRegStr SHELL_CONTEXT "${UNINST_KEY}" "DisplayName" "${INFO_PRODUCTNAME}"
    WriteRegStr SHELL_CONTEXT "${UNINST_KEY}" "DisplayVersion" "${INFO_PRODUCTVERSION}"
    WriteRegStr SHELL_CONTEXT "${UNINST_KEY}" "DisplayIcon" "$INSTDIR\${PRODUCT_EXECUTABLE}"
    WriteRegStr SHELL_CONTEXT "${UNINST_KEY}" "UninstallString" "$\"$INSTDIR\uninstall.exe$\""
    WriteRegStr SHELL_CONTEXT "${UNINST_KEY}" "QuietUninstallString" "$\"$INSTDIR\uninstall.exe$\" /S"

    ${GetSize} "$INSTDIR" "/S=0K" $0 $1 $2
    IntFmt $0 "0x%08X" $0
    WriteRegDWORD SHELL_CONTEXT "${UNINST_KEY}" "EstimatedSize" "$0"
SectionEnd

Section "uninstall"
    !insertmacro wails.setShellContext

    RMDir /r "$AppData\${PRODUCT_EXECUTABLE}" # Remove the WebView2 DataPath

    RMDir /r $INSTDIR

    Delete "$SMPROGRAMS\${INFO_PRODUCTNAME}.lnk"
    Delete "$DESKTOP\${INFO_PRODUCTNAME}.lnk"

    !insertmacro wails.unassociateFiles
    !insertmacro wails.unassociateCustomProtocols

    # The other half of the block above: wails.deleteUninstaller removes the key from HKLM,
    # which is not where this installer wrote it.
    Delete "$INSTDIR\uninstall.exe"

    SetRegView 64
    DeleteRegKey SHELL_CONTEXT "${UNINST_KEY}"
SectionEnd
