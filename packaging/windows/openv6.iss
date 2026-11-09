; Inno Setup script for the Windows installer (built in CI).
; Expects openv6.exe (GUI build) and openv6.ico next to this file and
; the version in the OPENV6_VERSION environment variable.
#define AppVersion GetEnv("OPENV6_VERSION")

[Setup]
AppId={{6F70656E-7636-4E2D-9000-000000000001}
AppName=openv6
AppVersion={#AppVersion}
AppVerName=openv6 {#AppVersion}
AppPublisher=openv6
AppPublisherURL=https://github.com/synthcoz/openv6
DefaultDirName={autopf}\openv6
DisableDirPage=yes
DisableProgramGroupPage=yes
PrivilegesRequired=admin
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
OutputDir=.
OutputBaseFilename=openv6-setup-windows-amd64
SetupIconFile=openv6.ico
UninstallDisplayIcon={app}\openv6.exe
UninstallDisplayName=openv6
WizardStyle=modern
Compression=lzma2
SolidCompression=yes
CloseApplications=no

[Files]
Source: "openv6.exe"; DestDir: "{app}"; Flags: ignoreversion

[Icons]
Name: "{autoprograms}\openv6"; Filename: "{app}\openv6.exe"; Comment: "openv6 IPv6 proxy"
Name: "{autodesktop}\openv6"; Filename: "{app}\openv6.exe"; Comment: "openv6 IPv6 proxy"; Tasks: desktopicon

[Tasks]
Name: "desktopicon"; Description: "Create a desktop shortcut"

[Run]
Filename: "{app}\openv6.exe"; Parameters: "install server"; Flags: runhidden waituntilterminated; StatusMsg: "Installing the openv6 service..."
Filename: "{app}\openv6.exe"; Parameters: "tray"; Flags: nowait runasoriginaluser; Description: "Start openv6"
Filename: "{app}\openv6.exe"; Parameters: "open"; Flags: postinstall runasoriginaluser nowait skipifsilent; Description: "Open the openv6 dashboard"

[UninstallRun]
Filename: "{app}\openv6.exe"; Parameters: "uninstall"; Flags: runhidden waituntilterminated; RunOnceId: "openv6-uninstall"

[Code]
// Stop the running service and tray so their files can be replaced on upgrade.
function PrepareToInstall(var NeedsRestart: Boolean): String;
var
  Code: Integer;
begin
  Exec(ExpandConstant('{sys}\sc.exe'), 'stop openv6', '', SW_HIDE, ewWaitUntilTerminated, Code);
  Exec(ExpandConstant('{sys}\taskkill.exe'), '/F /IM openv6-tray.exe', '', SW_HIDE, ewWaitUntilTerminated, Code);
  Exec(ExpandConstant('{sys}\taskkill.exe'), '/F /IM openv6.exe', '', SW_HIDE, ewWaitUntilTerminated, Code);
  Sleep(1500);
  Result := '';
end;
