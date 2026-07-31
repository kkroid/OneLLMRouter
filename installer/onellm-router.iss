#ifndef AppVersion
  #define AppVersion "1.4.0"
#endif
#ifndef StageDir
  #define StageDir "..\desktop\stage"
#endif

#define AppName "OneLLMRouter"
#define AppExeName "onellm-router-tray.exe"

[Setup]
AppId={{B4D79E0E-89A7-4F6A-BE3B-9C04E17D6D32}
AppName={#AppName}
AppVersion={#AppVersion}
DefaultDirName={localappdata}\Programs\OneLLMRouter
DefaultGroupName=OneLLMRouter
PrivilegesRequired=lowest
ArchitecturesAllowed=x64compatible
ArchitecturesInstallIn64BitMode=x64compatible
OutputDir=..\dist
OutputBaseFilename=OneLLMRouter-{#AppVersion}-setup
Compression=lzma2
SolidCompression=yes
WizardStyle=modern
CloseApplications=no
RestartApplications=no
UninstallDisplayIcon={app}\{#AppExeName}

[Tasks]
Name: "autostart"; Description: "Start OneLLMRouter when I sign in"; GroupDescription: "Startup:"; Flags: checkedonce

[Files]
Source: "{#StageDir}\*"; DestDir: "{app}"; Flags: ignoreversion recursesubdirs createallsubdirs
Source: "..\onellm-router.example.yaml"; DestDir: "{%USERPROFILE}\.onellm"; DestName: "onellm-router.yaml"; Flags: onlyifdoesntexist uninsneveruninstall

[Dirs]
Name: "{%USERPROFILE}\.onellm"; Flags: uninsneveruninstall
Name: "{%USERPROFILE}\.onellm\logs"; Flags: uninsneveruninstall

[Icons]
Name: "{group}\OneLLMRouter"; Filename: "{app}\{#AppExeName}"; Parameters: "--config ""{%USERPROFILE}\.onellm\onellm-router.yaml"""
Name: "{group}\Uninstall OneLLMRouter"; Filename: "{uninstallexe}"

[Registry]
Root: HKCU; Subkey: "Software\Microsoft\Windows\CurrentVersion\Run"; ValueType: none; ValueName: "OneLLMRouter"; Flags: deletevalue
Root: HKCU; Subkey: "Software\Microsoft\Windows\CurrentVersion\Run"; ValueType: string; ValueName: "OneLLMRouter Desktop"; ValueData: """{app}\{#AppExeName}"" --config ""{%USERPROFILE}\.onellm\onellm-router.yaml"""; Tasks: autostart; Flags: uninsdeletevalue

[Run]
Filename: "{app}\{#AppExeName}"; Parameters: "--config ""{%USERPROFILE}\.onellm\onellm-router.yaml"""; Description: "Launch OneLLMRouter"; Flags: nowait postinstall skipifsilent
