# Sandboxie compat matrix — run 40859166519 (4)

| app | slot | detected | class | A1 | A2 | A3 | verdict | failClass | bytes | exploratory |
|---|---|---|---|---|---|---|---|---|---|---|
| chrome-msi | msi | msi | msi-refused | FAIL | FAIL | FAIL | FAIL | installer-type-msi | 170467328 | PASS/FAIL/FAIL (msiexec /i <pkg> /qn ALLUSERS=2 MSIINSTALLPERUSER=1 (inside box)) |
| dingtalk | custom-cn | unknown | interactive-unsupported | FAIL | FAIL | FAIL | FAIL | installer-type-unsupported | 460186048 | PASS/PASS/PASS (family-conventional silent args: /S) |
| discord | squirrel | squirrel | interactive-unsupported | FAIL | FAIL | FAIL | FAIL | installer-type-unsupported | 145745848 | PASS/PASS/FAIL (family-conventional silent args: --silent) |
| notepadpp | nsis | nsis | nsis-silent | PASS | PASS | PASS | PASS |  | 6963432 | - |
| telegram | inno | inno | inno-silent | PASS | PASS | PASS | PASS |  | 63102944 | - |
| vlc | other | nsis | nsis-silent | PASS | PASS | PASS | PASS |  | 44943296 | - |
| vscode | per-user | inno | inno-silent | PASS | PASS | FAIL | FAIL | launch-exits | 251590472 | - |
| wechat | nsis-baseline | nsis | nsis-silent | PASS | PASS | PASS | PASS |  | 259938920 | - |

## Per-leg detail

### chrome-msi
```json
{
  "app": "chrome-msi",
  "familySlot": "msi",
  "installerUrl": "https://dl.google.com/dl/chrome/install/googlechromestandaloneenterprise64.msi",
  "installerFile": "GoogleChromeStandaloneEnterprise64.msi",
  "measuredBytes": 170467328,
  "measuredSha256": "cf1757b01c3af403de494a1b702e2a96701fd10026f5b89be53741689ed25895",
  "pinNote": "",
  "detectedType": "msi",
  "productClass": "msi-refused",
  "A1": "FAIL",
  "A1symptom": "product refuses this class today: msi-refused (detectedType=msi)",
  "A2": "FAIL",
  "A2symptom": "not reached (install refused)",
  "A3": "FAIL",
  "A3symptom": "not reached (install refused)",
  "verdict": "FAIL",
  "failClass": "installer-type-msi",
  "candidateTop": "",
  "candidateList": "",
  "hostDirsClean": "PASS",
  "hostUninstallClean": "PASS",
  "installExitCode": "",
  "installElapsedSec": "",
  "exploratory": {
    "attempted": true,
    "method": "msiexec /i <pkg> /qn ALLUSERS=2 MSIINSTALLPERUSER=1 (inside box)",
    "E1": "PASS",
    "E1symptom": "exesInBox=3",
    "E2": "FAIL",
    "E2symptom": "top=C:\\Program Files (x86)\\Google\\GoogleUpdater\\157.0.8089.0\\updater.exe score=30 expectedOneOf=chrome.exe",
    "E3": "FAIL",
    "E3symptom": "none of [chrome.exe] present in box to launch",
    "hostUninstallAfter": "clean"
  }
}
```

### dingtalk
```json
{
  "app": "dingtalk",
  "familySlot": "custom-cn",
  "installerUrl": "https://dtapp-cast.dingtalk.com/202908242023/4090273bee39319286ee77436ca0b845/dingtalk-desktop/win_installer/Release/DingTalk_v8.2.0.260119001.exe",
  "installerFile": "DingTalk_v8.2.0.260119001.exe",
  "measuredBytes": 460186048,
  "measuredSha256": "c4664b74f7b688fcbe30b951632eb6b0abb820703908835695b5f82f8431cf56",
  "pinNote": "",
  "detectedType": "unknown",
  "productClass": "interactive-unsupported",
  "A1": "FAIL",
  "A1symptom": "product refuses this class today: interactive-unsupported (detectedType=unknown)",
  "A2": "FAIL",
  "A2symptom": "not reached (install refused)",
  "A3": "FAIL",
  "A3symptom": "not reached (install refused)",
  "verdict": "FAIL",
  "failClass": "installer-type-unsupported",
  "candidateTop": "",
  "candidateList": "",
  "hostDirsClean": "PASS",
  "hostUninstallClean": "PASS",
  "installExitCode": "",
  "installElapsedSec": "",
  "exploratory": {
    "attempted": true,
    "method": "family-conventional silent args: /S",
    "E1": "PASS",
    "E1symptom": "exesInBox=32",
    "E2": "PASS",
    "E2symptom": "top=C:\\Program Files (x86)\\DingDing\\main\\current\\DingTalk.exe score=80 expectedOneOf=DingTalk.exe",
    "E3": "PASS",
    "E3symptom": "pid=1328 aliveAfter20s=True pidInBoxList=True launchStarterTimeout=False listRaw=[7,7120,8320,620,1328,2876,6128,5680] listParsed=[620,1328,2876,5680,6128,7120,8320]",
    "hostUninstallAfter": "clean"
  }
}
```

### discord
```json
{
  "app": "discord",
  "familySlot": "squirrel",
  "installerUrl": "https://discord.com/api/downloads/distributions/app/installers/latest?channel=stable&platform=win&arch=x64",
  "installerFile": "DiscordSetup.exe",
  "measuredBytes": 145745848,
  "measuredSha256": "eda6c85b1b6e56507eb5a4cfb38310c20903b7893c435f6fbd869406bda663ab",
  "pinNote": "",
  "detectedType": "squirrel",
  "productClass": "interactive-unsupported",
  "A1": "FAIL",
  "A1symptom": "product refuses this class today: interactive-unsupported (detectedType=squirrel)",
  "A2": "FAIL",
  "A2symptom": "not reached (install refused)",
  "A3": "FAIL",
  "A3symptom": "not reached (install refused)",
  "verdict": "FAIL",
  "failClass": "installer-type-unsupported",
  "candidateTop": "",
  "candidateList": "",
  "hostDirsClean": "PASS",
  "hostUninstallClean": "PASS",
  "installExitCode": "",
  "installElapsedSec": "",
  "exploratory": {
    "attempted": true,
    "method": "family-conventional silent args: --silent",
    "E1": "PASS",
    "E1symptom": "exesInBox=1",
    "E2": "PASS",
    "E2symptom": "top=C:\\Users\\all\\SquirrelMachineInstalls\\Discord.exe score=63 expectedOneOf=Discord.exe",
    "E3": "FAIL",
    "E3symptom": "no process appeared within appearance budget (120s wall clock); launch starter TIMEOUT after 20s",
    "hostUninstallAfter": "clean"
  }
}
```

### notepadpp
```json
{
  "app": "notepadpp",
  "familySlot": "nsis",
  "installerUrl": "https://github.com/notepad-plus-plus/notepad-plus-plus/releases/download/v8.9.8.1/npp.8.9.8.1.Installer.x64.exe",
  "installerFile": "npp.8.9.8.1.Installer.x64.exe",
  "measuredBytes": 6963432,
  "measuredSha256": "26f3bcced788fadbac4e7e577b430ea2ce6060cf3bcad993ef4612ecd7d743da",
  "pinNote": "",
  "detectedType": "nsis",
  "productClass": "nsis-silent",
  "A1": "PASS",
  "A1symptom": "exesInBox=3; installerExit=0 elapsedSec=23.1",
  "A2": "PASS",
  "A2symptom": "top=C:\\Program Files\\Notepad++\\notepad++.exe score=120 expectedOneOf=notepad++.exe",
  "A3": "PASS",
  "A3symptom": "pid=4172 aliveAfter20s=True pidInBoxList=True launchStarterTimeout=False listRaw=[4,5660,3296,7276,4172] listParsed=[3296,4172,5660,7276]",
  "verdict": "PASS",
  "failClass": "",
  "candidateTop": "C:\\Program Files\\Notepad++\\notepad++.exe",
  "candidateList": "120 C:\\Program Files\\Notepad++\\notepad++.exe\n30 C:\\Program Files\\Notepad++\\updater\\GUP.exe",
  "hostDirsClean": "PASS",
  "hostUninstallClean": "PASS",
  "installExitCode": "0",
  "installElapsedSec": 23.1,
  "exploratory": null
}
```

### telegram
```json
{
  "app": "telegram",
  "familySlot": "inno",
  "installerUrl": "https://github.com/telegramdesktop/tdesktop/releases/download/v7.3.3/td-setup-win-x64-7.3.3.exe",
  "installerFile": "td-setup-win-x64-7.3.3.exe",
  "measuredBytes": 63102944,
  "measuredSha256": "865cd80080ef1948bd818000c05fe81cabbbd0688a91c2cf8978f9cc73fd5a06",
  "pinNote": "",
  "detectedType": "inno",
  "productClass": "inno-silent",
  "A1": "PASS",
  "A1symptom": "exesInBox=3; installerExit=0 elapsedSec=20.3",
  "A2": "PASS",
  "A2symptom": "top=C:\\Users\\runneradmin\\AppData\\Roaming\\Telegram Desktop\\Telegram.exe score=200 expectedOneOf=Telegram.exe",
  "A3": "PASS",
  "A3symptom": "pid=5184 aliveAfter20s=True pidInBoxList=True launchStarterTimeout=False listRaw=[4,7060,5184,5324,7540] listParsed=[5184,5324,7060,7540]",
  "verdict": "PASS",
  "failClass": "",
  "candidateTop": "C:\\Users\\runneradmin\\AppData\\Roaming\\Telegram Desktop\\Telegram.exe",
  "candidateList": "200 C:\\Users\\runneradmin\\AppData\\Roaming\\Telegram Desktop\\Telegram.exe\n130 C:\\Users\\runneradmin\\AppData\\Roaming\\Telegram Desktop\\Updater.exe",
  "hostDirsClean": "PASS",
  "hostUninstallClean": "PASS",
  "installExitCode": "0",
  "installElapsedSec": 20.3,
  "exploratory": null
}
```

### vlc
```json
{
  "app": "vlc",
  "familySlot": "other",
  "installerUrl": "https://download.videolan.org/pub/videolan/vlc/3.0.21/win64/vlc-3.0.21-win64.exe",
  "installerFile": "vlc-3.0.21-win64.exe",
  "measuredBytes": 44943296,
  "measuredSha256": "9742689a50e96ddc04d80ceff046b28da2beefd617be18166f8c5e715ec60c59",
  "pinNote": "",
  "detectedType": "nsis",
  "productClass": "nsis-silent",
  "A1": "PASS",
  "A1symptom": "exesInBox=3; installerExit=0 elapsedSec=20.5",
  "A2": "PASS",
  "A2symptom": "top=C:\\Program Files\\VideoLAN\\VLC\\vlc.exe score=120 expectedOneOf=vlc.exe",
  "A3": "PASS",
  "A3symptom": "pid=8608 aliveAfter20s=True pidInBoxList=True launchStarterTimeout=False listRaw=[3,8608,7676,3772] listParsed=[3772,7676,8608]",
  "verdict": "PASS",
  "failClass": "",
  "candidateTop": "C:\\Program Files\\VideoLAN\\VLC\\vlc.exe",
  "candidateList": "120 C:\\Program Files\\VideoLAN\\VLC\\vlc.exe\n70 C:\\Program Files\\VideoLAN\\VLC\\vlc-cache-gen.exe",
  "hostDirsClean": "PASS",
  "hostUninstallClean": "PASS",
  "installExitCode": "0",
  "installElapsedSec": 20.5,
  "exploratory": null
}
```

### vscode
```json
{
  "app": "vscode",
  "familySlot": "per-user",
  "installerUrl": "https://update.code.visualstudio.com/latest/win32-x64-user/stable",
  "installerFile": "VSCodeUserSetup.exe",
  "measuredBytes": 251590472,
  "measuredSha256": "d4be210abf9c9739d65445bd7993210d6163f32069297adc2314d9bb04ad4afb",
  "pinNote": "",
  "detectedType": "inno",
  "productClass": "inno-silent",
  "A1": "PASS",
  "A1symptom": "exesInBox=14; installerExit=0 elapsedSec=73.9",
  "A2": "PASS",
  "A2symptom": "top=C:\\Users\\runneradmin\\AppData\\Local\\Programs\\Microsoft VS Code\\Code.exe score=170 expectedOneOf=Code.exe",
  "A3": "FAIL",
  "A3symptom": "no process appeared within appearance budget (120s wall clock)",
  "verdict": "FAIL",
  "failClass": "launch-exits",
  "candidateTop": "C:\\Users\\runneradmin\\AppData\\Local\\Programs\\Microsoft VS Code\\Code.exe",
  "candidateList": "170 C:\\Users\\runneradmin\\AppData\\Local\\Programs\\Microsoft VS Code\\Code.exe\n62 C:\\Users\\runneradmin\\AppData\\Local\\Programs\\Microsoft VS Code\\bin\\code-tunnel.exe\n60 C:\\Users\\runneradmin\\AppData\\Local\\Programs\\Microsoft VS Code\\2c33b50b16\\resources\\app\\extensions\\copilot\\node_modules\\@github\\copilot\\sdk\\ripgrep\\bin\\win32-x64\\rg.exe\n60 C:\\Users\\runneradmin\\AppData\\Local\\Programs\\Microsoft VS Code\\2c33b50b16\\resources\\app\\extensions\\copilot\\node_modules\\@github\\copilot\\sdk\\tgrep\\bin\\win32-x64\\tgrep.exe\n60 C:\\Users\\runneradmin\\AppData\\Local\\Programs\\Microsoft VS Code\\2c33b50b16\\resources\\app\\extensions\\copilot\\node_modules\\@github\\copilot\\tgrep\\bin\\win32-x64\\tgrep.exe\n60 C:\\Users\\runneradmin\\AppData\\Local\\Programs\\Microsoft VS Code\\2c33b50b16\\resources\\app\\node_modules.asar.unpacked\\@github\\copilot-sdk-win32-x64\\prebuilds\\win32-x64\\copilot-runtime.exe\n60 C:\\Users\\runneradmin\\AppData\\Local\\Programs\\Microsoft VS Code\\2c33b50b16\\resources\\app\\node_modules.asar.unpacked\\@github\\copilot-sdk-win32-x64\\ripgrep\\bin\\win32-x64\\rg.exe\n60 C:\\Users\\runneradmin\\AppData\\Local\\Programs\\Microsoft VS Code\\2c33b50b16\\resources\\app\\node_modules.asar.unpacked\\@github\\copilot-sdk-win32-x64\\tgrep\\bin\\win32-x64\\tgrep.exe",
  "hostDirsClean": "PASS",
  "hostUninstallClean": "PASS",
  "installExitCode": "0",
  "installElapsedSec": 73.9,
  "exploratory": null
}
```

### wechat
```json
{
  "app": "wechat",
  "familySlot": "nsis-baseline",
  "installerUrl": "https://dldir1v6.qq.com/weixin/Universal/Windows/WeChatWin_4.2.0.exe",
  "installerFile": "WeChatWin_4.2.0.exe",
  "measuredBytes": 259938920,
  "measuredSha256": "fc948730e854ae0e03e64ac57ffde0f61b530d7a571d16df411914bad30a1526",
  "pinNote": "",
  "detectedType": "nsis",
  "productClass": "nsis-silent",
  "A1": "PASS",
  "A1symptom": "exesInBox=6; installerExit=0 elapsedSec=25.3",
  "A2": "PASS",
  "A2symptom": "top=C:\\Program Files\\Tencent\\Weixin\\Weixin.exe score=120 expectedOneOf=Weixin.exe,WeChat.exe",
  "A3": "PASS",
  "A3symptom": "pid=1888 aliveAfter20s=True pidInBoxList=True launchStarterTimeout=False listRaw=[8,8708,1320,2272,1836,1148,1888,7020,1716] listParsed=[1148,1320,1716,1836,1888,2272,7020,8708]",
  "verdict": "PASS",
  "failClass": "",
  "candidateTop": "C:\\Program Files\\Tencent\\Weixin\\Weixin.exe",
  "candidateList": "120 C:\\Program Files\\Tencent\\Weixin\\Weixin.exe\n30 C:\\Program Files\\Tencent\\Weixin\\4.2.0.9\\crashpad_handler.exe\n30 C:\\Program Files\\Tencent\\Weixin\\4.2.0.9\\WeixinExt.exe\n30 C:\\Program Files\\Tencent\\Weixin\\4.2.0.9\\WeixinUpdate.exe\n30 C:\\Program Files\\Tencent\\Weixin\\4.2.0.9\\WetypeInstaller.exe",
  "hostDirsClean": "PASS",
  "hostUninstallClean": "PASS",
  "installExitCode": "0",
  "installElapsedSec": 25.3,
  "exploratory": null
}
```
