# 配置模拟器的中文语言与 120Hz 刷新率。
# 用法：
#   .\tools\configure-avd.ps1                  # 配置默认 AVD apimonitor_api36
#   .\tools\configure-avd.ps1 -AvdName 其他名
# 需要模拟器处于运行状态（脚本会通过 adb 下发设置）。

param(
    [string]$AvdName = "apimonitor_api36",
    [int]$RefreshRate = 120,
    [string]$Locale = "zh-CN"
)

$ErrorActionPreference = "Stop"

$sdkRoot = $env:ANDROID_HOME
if (-not $sdkRoot) { $sdkRoot = "E:\Android\Sdk" }
$adb = Join-Path $sdkRoot "platform-tools\adb.exe"
if (-not (Test-Path $adb)) { throw "找不到 adb：$adb" }

$configPath = Join-Path $env:USERPROFILE ".android\avd\$AvdName.avd\config.ini"
if (-not (Test-Path $configPath)) { throw "找不到 AVD 配置：$configPath" }

$devices = & $adb devices | Select-String -Pattern "emulator-\d+\s+device"
if (-not $devices) { throw "没有已连接的模拟器，请先启动 $AvdName" }

Write-Host "配置 AVD: $AvdName (locale=$Locale, ${RefreshRate}Hz)"

# 1. 持久化到 AVD 配置，重启后依然生效
$lines = Get-Content $configPath
$lines = $lines -replace '^hw\.lcd\.vsync=.*$', "hw.lcd.vsync=$RefreshRate"
$lines = $lines -replace '^hw\.gpu\.enabled=.*$', 'hw.gpu.enabled=yes'
if ($lines -notmatch '^hw\.lcd\.vsync=') { $lines += "hw.lcd.vsync=$RefreshRate" }
Set-Content -Path $configPath -Value $lines
Write-Host "  已写入 config.ini"

# 2. 运行时下发语言与刷新率
$currentProp = (& $adb shell getprop persist.sys.locale).Trim()
if ($currentProp -ne $Locale) {
    & $adb shell "setprop persist.sys.locale $Locale" 2>&1 | Out-Null
    & $adb shell "setprop ctl.restart zygote" 2>&1 | Out-Null
    Write-Host "  语言已改为 $Locale，等待系统重启…"
    & $adb wait-for-device
}
& $adb shell settings put system peak_refresh_rate $RefreshRate
& $adb shell settings put system min_refresh_rate $RefreshRate
& $adb shell settings put system adaptive_refresh_rate 0

Start-Sleep -Seconds 2

# 3. 校验
$currentLocale = (& $adb shell getprop persist.sys.locale).Trim()
$displayDump = (& $adb shell dumpsys display) -join "`n"
$vsyncMatch = [regex]::Match($displayDump, 'mSupportedRefreshRates=\[([0-9.]+)\]')
$activeMatch = [regex]::Match($displayDump, 'mActiveRenderFrameRate=([0-9.]+)')

Write-Host ""
Write-Host "结果："
Write-Host "  locale         = $currentLocale"
if ($vsyncMatch.Success) { Write-Host "  支持刷新率     = $($vsyncMatch.Groups[1].Value) Hz" }
if ($activeMatch.Success) { Write-Host "  当前渲染帧率   = $($activeMatch.Groups[1].Value) Hz" }

if ($currentLocale -ne $Locale) {
    Write-Host "  提示：语言需要重启模拟器（adb shell setprop ctl.restart zygote）后完全生效"
}
