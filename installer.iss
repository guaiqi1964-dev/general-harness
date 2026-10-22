; General Harness v0.3.0 Windows installer script (Inno Setup 6)
; Build: ISCC.exe installer.iss

#define MyAppName "General Harness"
#define MyAppVersion "0.3.6"
#define MyAppFileVersion "0.3.6.0"
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
UninstallDisplayIcon={app}\general-harness.ico
Compression=lzma2/max
SolidCompression=yes
OutputDir=release_assets
OutputBaseFilename=General-Harness-Setup-{#MyAppVersion}
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
; 安装程序自身的 exe 图标（用户双击安装包时看到的第一印象）
SetupIconFile=assets\general-harness.ico
SetupLogging=yes

[Languages]
Name: "chinesesimplified"; MessagesFile: "compiler:Languages\ChineseSimplified.isl"
Name: "english"; MessagesFile: "compiler:Default.isl"

[Tasks]
Name: "desktopicon"; Description: "Create desktop shortcut"; GroupDescription: "Additional tasks:"

[Files]
; 发行件只带未压缩的 gh.exe：UPX 压缩版会被 Windows Defender 误判为
; Trojan:Win32/Wacatac.B!ml（机器学习启发式对 UPX+Go 的典型误报），
; 用户解压即被隔离，得不偿失。
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
Source: "assets\general-harness.ico"; DestDir: "{app}"; Flags: ignoreversion
Source: "plugins\deepseek\config.yaml"; DestDir: "{app}\plugins\deepseek"; Flags: ignoreversion
Source: "plugins\kimi\config.yaml"; DestDir: "{app}\plugins\kimi"; Flags: ignoreversion
Source: "plugins\openai\config.yaml"; DestDir: "{app}\plugins\openai"; Flags: ignoreversion
Source: "plugins\qwen\config.yaml"; DestDir: "{app}\plugins\qwen"; Flags: ignoreversion


[Icons]
Name: "{group}\General Harness (GUI)"; Filename: "{app}\start.bat"; WorkingDir: "{app}"; IconFilename: "{app}\general-harness.ico"
Name: "{group}\General Harness (CLI)"; Filename: "{app}\start.bat"; Parameters: "cli"; WorkingDir: "{app}"; IconFilename: "{app}\general-harness.ico"
Name: "{group}\Restart Engine"; Filename: "{app}\restart.bat"; WorkingDir: "{app}"; IconFilename: "{app}\general-harness.ico"
Name: "{group}\Stop Engine"; Filename: "{app}\stop.bat"; WorkingDir: "{app}"; IconFilename: "{app}\general-harness.ico"
Name: "{group}\Uninstall General Harness"; Filename: "{uninstallexe}"; IconFilename: "{app}\general-harness.ico"
Name: "{autodesktop}\General Harness"; Filename: "{app}\start.bat"; WorkingDir: "{app}"; IconFilename: "{app}\general-harness.ico"; Tasks: desktopicon

[Run]
; Auto-detect Python and install GUI deps (optional; silent failure does not block install)
Filename: "{cmd}"; Parameters: "/c ""python --version >nul 2>&1 && python -m pip install -r ""{app}\gui\requirements.txt"" -q || echo [General Harness] Python not found - the built-in browser UI (/ui) will be used"""; Flags: runhidden; StatusMsg: "Configuring GUI dependency (pywebview)..."
Filename: "{app}\start.bat"; Description: "Launch General Harness now"; Flags: nowait postinstall skipifsilent

[UninstallDelete]
Type: filesandordirs; Name: "{app}\gateway.pid"
Type: filesandordirs; Name: "{app}\usage_stats.json"
Type: files; Name: "{app}\keys.yaml"
