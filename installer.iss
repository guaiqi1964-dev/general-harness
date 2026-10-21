; General Harness v0.3.3 Windows installer script (Inno Setup 6) — CLI-only branch
; Build: ISCC.exe installer.iss
; 本分支不含 GUI（无 pywebview、无 /ui 页面、无 gui/ 目录），安装后即用终端。

#define MyAppName "General Harness (CLI)"
#define MyAppVersion "0.3.4"
#define MyAppFileVersion "0.3.4.0"
#define MyAppPublisher "GQ's Lab"
#define MyAppExeName "start.bat"
#define MyAppId "{{8F3B1A2C-5D4E-4A6B-9C7D-1E2F3A4B5C6D}"

[Setup]
AppId={#MyAppId}
AppName=General Harness
AppVersion={#MyAppVersion}
AppVerName={#MyAppName} {#MyAppVersion}
AppPublisher={#MyAppPublisher}
DefaultDirName={autopf}\General Harness CLI
DefaultGroupName=General Harness CLI
DisableProgramGroupPage=no
UninstallDisplayIcon={app}\general-harness.ico
Compression=lzma2/max
SolidCompression=yes
OutputDir=release_assets
OutputBaseFilename=General-Harness-CLI-Setup-{#MyAppVersion}
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
PrivilegesRequired=lowest
PrivilegesRequiredOverridesAllowed=dialog
WizardStyle=modern
; 显式指定含中文字形的对话框字体。默认 Segoe UI 在精简系统/英文系统上可能
; 没有配置 GDI FontLink，导致中文向导文字全部渲染成方块（tofu）。
DefaultDialogFontName=Microsoft YaHei UI
VersionInfoVersion={#MyAppFileVersion}
VersionInfoProductName={#MyAppName}
; 安装程序自身的 exe 图标
SetupIconFile=assets\general-harness.ico
SetupLogging=yes

[Languages]
Name: "chinesesimplified"; MessagesFile: "compiler:Languages\ChineseSimplified.isl"
Name: "english"; MessagesFile: "compiler:Default.isl"

[Tasks]
Name: "desktopicon"; Description: "Create desktop shortcut"; GroupDescription: "Additional tasks:"

[Files]
; 只发布 UPX 压缩版：未压缩的 gh.exe 有 6.4 MB，对纯 CLI 发行版没有价值。
; 只带未压缩的 gh.exe：UPX 压缩版会被 Windows Defender 误判为 Trojan:Win32/Wacatac.B!ml
Source: "bin\gh.exe"; DestDir: "{app}\bin"; Flags: ignoreversion
Source: "config.yaml"; DestDir: "{app}"; Flags: ignoreversion
Source: "start.bat"; DestDir: "{app}"; Flags: ignoreversion
Source: "restart.bat"; DestDir: "{app}"; Flags: ignoreversion
Source: "stop.bat"; DestDir: "{app}"; Flags: ignoreversion
Source: "start.py"; DestDir: "{app}"; Flags: ignoreversion
Source: "README.md"; DestDir: "{app}"; Flags: ignoreversion
Source: "README.en.md"; DestDir: "{app}"; Flags: ignoreversion
Source: "cli\README.md"; DestDir: "{app}\cli"; Flags: ignoreversion
Source: "models\*.gguf"; DestDir: "{app}\models"; Flags: ignoreversion
Source: "assets\general-harness.ico"; DestDir: "{app}"; Flags: ignoreversion
Source: "plugins\deepseek\config.yaml"; DestDir: "{app}\plugins\deepseek"; Flags: ignoreversion
Source: "plugins\kimi\config.yaml"; DestDir: "{app}\plugins\kimi"; Flags: ignoreversion
Source: "plugins\openai\config.yaml"; DestDir: "{app}\plugins\openai"; Flags: ignoreversion
Source: "plugins\qwen\config.yaml"; DestDir: "{app}\plugins\qwen"; Flags: ignoreversion

[Icons]
Name: "{group}\General Harness CLI"; Filename: "{app}\start.bat"; WorkingDir: "{app}"; IconFilename: "{app}\general-harness.ico"
Name: "{group}\Restart Engine"; Filename: "{app}\restart.bat"; WorkingDir: "{app}"; IconFilename: "{app}\general-harness.ico"
Name: "{group}\Stop Engine"; Filename: "{app}\stop.bat"; WorkingDir: "{app}"; IconFilename: "{app}\general-harness.ico"
Name: "{group}\Uninstall General Harness CLI"; Filename: "{uninstallexe}"; IconFilename: "{app}\general-harness.ico"
Name: "{autodesktop}\General Harness CLI"; Filename: "{app}\start.bat"; WorkingDir: "{app}"; IconFilename: "{app}\general-harness.ico"; Tasks: desktopicon

[Run]
; 不再安装任何 Python 依赖：CLI 是本分支唯一前端，go 引擎自带终端界面。
Filename: "{app}\start.bat"; Description: "Launch General Harness CLI now"; Flags: nowait postinstall skipifsilent

[UninstallDelete]
Type: filesandordirs; Name: "{app}\gateway.pid"
Type: filesandordirs; Name: "{app}\usage_stats.json"
Type: files; Name: "{app}\keys.yaml"
