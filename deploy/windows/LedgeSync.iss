; Built only through tools/package_windows.py with a verified existing desktop app.
; Inno Setup 7.1.0. Keep AppId stable across versions and architectures.
#ifndef PayloadDir
  #error PayloadDir must be supplied by the packaging script
#endif
#ifndef PackageVersion
  #error PackageVersion is required
#endif

[Setup]
AppId={{E6DE1B85-8A9D-47D2-9960-5A26E99A0C2B}
AppName=LedgeSync
AppVersion={#PackageVersion}
AppVerName=LedgeSync {#PackageVersion}
AppPublisher=LedgeSync contributors
AppPublisherURL=https://ledgesync.com
AppSupportURL=https://github.com/alexandroit/LedgeSync/issues
AppUpdatesURL=https://github.com/alexandroit/LedgeSync/releases
VersionInfoVersion={#NumericVersion}
VersionInfoDescription=LedgeSync graphical installer
DefaultDirName={localappdata}\Programs\LedgeSync
DefaultGroupName=LedgeSync
DisableProgramGroupPage=yes
DisableDirPage=no
DisableWelcomePage=no
PrivilegesRequired=lowest
SetupArchitecture=x64
ArchitecturesAllowed={#AllowedArchitecture}
ArchitecturesInstallIn64BitMode={#AllowedArchitecture}
MinVersion=10.0.20348
OutputBaseFilename={#OutputName}
OutputDir={#PackageOutput}
SetupIconFile={#InstallerIcon}
UninstallDisplayIcon={app}\LedgeSync.exe
UninstallDisplayName=LedgeSync
Uninstallable=yes
CreateUninstallRegKey=yes
LicenseFile={#PayloadDir}\LICENSE
InfoBeforeFile={#PayloadDir}\INSTALL.txt
Compression=lzma2
SolidCompression=yes
WizardStyle=modern
CloseApplications=yes
RestartApplications=no
ChangesEnvironment=no

[Languages]
Name: "english"; MessagesFile: "compiler:Default.isl"

[Tasks]
Name: "desktopicon"; Description: "Create a &desktop shortcut"; GroupDescription: "Shortcuts:"; Flags: unchecked

[Files]
Source: "{#PayloadDir}\LedgeSync.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#PayloadDir}\LICENSE"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#PayloadDir}\NOTICE"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#PayloadDir}\THIRD_PARTY_NOTICES.md"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#PayloadDir}\third_party\*"; DestDir: "{app}\third_party"; Flags: ignoreversion recursesubdirs createallsubdirs
Source: "{#PayloadDir}\INSTALL.txt"; DestDir: "{app}"; Flags: ignoreversion
Source: "{#PayloadDir}\INNO_SETUP_LICENSE.txt"; DestDir: "{app}"; Flags: ignoreversion

[Icons]
Name: "{userprograms}\{groupname}\LedgeSync"; Filename: "{app}\LedgeSync.exe"; WorkingDir: "{app}"; AppUserModelID: "com.ledgesync.app"
Name: "{userdesktop}\LedgeSync"; Filename: "{app}\LedgeSync.exe"; WorkingDir: "{app}"; Tasks: desktopicon; AppUserModelID: "com.ledgesync.app"

[Run]
Filename: "{app}\LedgeSync.exe"; Description: "Open LedgeSync"; Flags: nowait postinstall skipifsilent unchecked

[Code]
const
  RuntimeKey = 'Software\Microsoft\EdgeUpdate\Clients\{F3017226-FE2A-4295-8BDF-00C3A9A7E4C5}';

function InitializeSetup: Boolean;
var
  Version: TWindowsVersion;
begin
  GetWindowsVersionEx(Version);
  Result := (Version.Major >= 10) and
    (((Version.ProductType = VER_NT_WORKSTATION) and (Version.Build >= 22000)) or
     (((Version.ProductType = VER_NT_SERVER) or (Version.ProductType = VER_NT_DOMAIN_CONTROLLER)) and (Version.Build >= 20348)));
  if not Result then
    SuppressibleMsgBox('LedgeSync requires Windows 11 or Windows Server 2022 or later. A graphical desktop and Microsoft Edge WebView2 Runtime are required.', mbError, MB_OK, IDOK);
end;

function RuntimeVersionPresent(Root: Integer; Key: String): Boolean;
var
  Version: String;
  ParsedVersion: Int64;
begin
  Result := False;
  if RegQueryStringValue(Root, Key, 'pv', Version) then
    if StrToVersion(Version, ParsedVersion) then
      Result := ParsedVersion > 0;
end;

function PrepareToInstall(var NeedsRestart: Boolean): String;
begin
  Result := '';
  { Microsoft's documented per-machine 32-bit registry view and per-user key. }
  if not (RuntimeVersionPresent(HKLM32, RuntimeKey) or RuntimeVersionPresent(HKCU, RuntimeKey)) then
    Result := 'Microsoft Edge WebView2 Runtime is required. Install the Evergreen Runtime from https://developer.microsoft.com/microsoft-edge/webview2/ and run this installer again. LedgeSync does not download or install this prerequisite automatically.';
end;

{ Intentionally no UninstallDelete, services, startup entries, scheduled tasks,
  prerequisite downloads, or writes outside the per-user installation/shortcuts. }
