#define MyAppName "Rillan"
#define MyAppPublisher "RillanAI"
#define MyAppURL "https://github.com/rillanai/rillan"
#define MyAppExeName "rillan.exe"

[Setup]
AppId={{C8B76222-C498-4A7D-A8D7-6F8AF764CC00}
AppName={#MyAppName}
AppVersion={#AppVersion}
AppPublisher={#MyAppPublisher}
AppPublisherURL={#MyAppURL}
DefaultDirName={autopf}\Rillan
DisableProgramGroupPage=yes
OutputDir=dist
OutputBaseFilename=rillan_{#AppVersion}_windows_amd64_setup
Compression=lzma
SolidCompression=yes

[Files]
Source: "dist\rillan_{#AppVersion}_windows_amd64\rillan.exe"; DestDir: "{app}"; Flags: ignoreversion
Source: "packaging\windows\rillan-service.xml"; DestDir: "{app}"; Flags: ignoreversion
Source: "packaging\install\install-ollama.ps1"; DestDir: "{app}"; Flags: ignoreversion

[Run]
Filename: "powershell.exe"; Parameters: "-ExecutionPolicy Bypass -File \"{app}\install-ollama.ps1\""; Flags: postinstall shellexec
