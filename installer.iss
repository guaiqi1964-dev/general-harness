; General Harness v0.2.0 Windows installer script (Inno Setup 6)
; Build: ISCC.exe installer.iss

#define MyAppName "General Harness"
#define MyAppVersion "0.2.0"
#define MyAppFileVersion "0.2.0.0"
#define MyAppPublisher "GQ's Lab"
#define MyAppExeName "start.bat"
#define MyAppId "{{8F3B1A2C-5D4E-4A6B-9C7D-1E2F3A4B5C6D}"

[Setup]
AppId={#MyAppId}
AppName={#MyAppName}
AppVersion={#MyAppVersion}
AppPublisher={#MyAppPublisher}
DefaultDirName={autopf}\General Harness
DefaultGroupName=General Harness
DisableProgramGroupPage=no
UninstallDisplayIcon={app}\bin\gh_upx.exe
Compression=lzma2/max
SolidCompression=yes
OutputDir=release_assets
OutputBaseFilename=General-Harness-Setup-{#MyAppVersion}
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
PrivilegesRequired=lowest
PrivilegesRequiredOverridesAllowed=dialog
WizardStyle=modern
VersionInfoVersion={#MyAppFileVersion}
VersionInfoProductName={#MyAppName}
SetupLogging=yes

[Languages]
Name: "chinesesimplified"; MessagesFile: "compiler:Languages\ChineseSimplified.isl"
Name: "english"; MessagesFile: "compiler:Default.isl"

[Tasks]
Name: "desktopicon"; Description: "Create desktop shortcut"; GroupDescription: "Additional tasks:"; Flags: unchecked

[Files]
Source: "bin\gh_upx.exe"; DestDir: "{app}\bin"; Flags: ignoreversion
Source: "bin\gh.exe"; DestDir: "{app}\bin"; Flags: ignoreversion
Source: "config.yaml"; DestDir: "{app}"; Flags: ignoreversion
Source: "start.py"; DestDir: "{app}"; Flags: ignoreversion
Source: "start.bat"; DestDir: "{app}"; Flags: ignoreversion
Source: "restart.bat"; DestDir: "{app}"; Flags: ignoreversion
Source: "stop.bat"; DestDir: "{app}"; Flags: ignoreversion
Source: "README.md"; DestDir: "{app}"; Flags: ignoreversion
Source: "README.en.md"; DestDir: "{app}"; Flags: ignoreversion
Source: "cli\README.md"; DestDir: "{app}\cli"; Flags: ignoreversion
Source: "gui\gui.py"; DestDir: "{app}\gui"; Flags: ignoreversion
Source: "gui\requirements.txt"; DestDir: "{app}\gui"; Flags: ignoreversion
Source: "models\*.gguf"; DestDir: "{app}\models"; Flags: ignoreversion
Source: "plugins\deepseek\config.yaml"; DestDir: "{app}\plugins\deepseek"; Flags: ignoreversion
Source: "plugins\kimi\config.yaml"; DestDir: "{app}\plugins\kimi"; Flags: ignoreversion
Source: "plugins\openai\config.yaml"; DestDir: "{app}\plugins\openai"; Flags: ignoreversion
Source: "plugins\qwen\config.yaml"; DestDir: "{app}\plugins\qwen"; Flags: ignoreversion


[Icons]
Name: "{group}\General Harness (GUI)"; Filename: "{app}\start.bat"; WorkingDir: "{app}"
Name: "{group}\General Harness (CLI)"; Filename: "{app}\start.bat"; Parameters: "cli"; WorkingDir: "{app}"
Name: "{group}\Restart Engine"; Filename: "{app}\restart.bat"; WorkingDir: "{app}"
Name: "{group}\Stop Engine"; Filename: "{app}\stop.bat"; WorkingDir: "{app}"
Name: "{group}\Uninstall General Harness"; Filename: "{uninstallexe}"
Name: "{autodesktop}\General Harness"; Filename: "{app}\start.bat"; WorkingDir: "{app}"; Tasks: desktopicon

[Run]
; Auto-detect Python and install GUI deps (optional; silent failure does not block install)
Filename: "{cmd}"; Parameters: "/c ""python --version >nul 2>&1 && python -m pip install -r ""{app}\gui\requirements.txt"" -q || echo [General Harness] Python not found, GUI will use browser mode"""; Flags: runhidden; StatusMsg: "Configuring GUI dependency (pywebview)..."
Filename: "{app}\start.bat"; Description: "Launch General Harness now"; Flags: nowait postinstall skipifsilent

[UninstallDelete]
Type: filesandordirs; Name: "{app}\gateway.pid"
Type: filesandordirs; Name: "{app}\usage_stats.json"
