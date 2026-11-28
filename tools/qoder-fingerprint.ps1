# 从本机已安装的 Qoder 客户端提取签到设备指纹（checkinFingerprint）。
#
# 原理：Qoder 桌面端把「设备指纹」交给阿里 umid 组件生成，组件随客户端一起
# 安装在本机（<home>\.qoder\.bin\umid-*\runtime-info.exe），直接运行即可读到
# machineToken / machineType / machineCode。machineId 存在客户端的
# auth.machine-id 里，machineOs 由「架构_平台」拼出，hostname 取本机主机名。
#
# 用法：
#   pwsh -File tools/qoder-fingerprint.ps1                 # 自动探测国际版
#   pwsh -File tools/qoder-fingerprint.ps1 -Region cn      # 国内版
#   pwsh -File tools/qoder-fingerprint.ps1 -Pretty         # 带缩进，便于阅读
#
# 输出就是账号编辑弹窗「签到设备指纹」输入框可以直接粘贴的 JSON。
# 注意：指纹与设备绑定，换机器/重装系统后会变（等于上游眼中的新设备）。

[CmdletBinding()]
param(
    [ValidateSet('intl', 'cn')]
    [string]$Region = 'intl',
    [switch]$Pretty
)

$ErrorActionPreference = 'Stop'

$cliHome = switch ($Region) {
    'intl' { Join-Path $env:USERPROFILE '.qoder' }
    'cn' { Join-Path $env:USERPROFILE '.qoder-cn' }
}

$appData = switch ($Region) {
    'intl' { Join-Path $env:APPDATA 'com.qoder.app.stable' }
    'cn' { Join-Path $env:APPDATA 'com.qodercn.app.stable' }
}

function Get-RuntimeInfoExe {
    param([string]$CliHome)
    $bin = Join-Path $CliHome '.bin'
    if (-not (Test-Path -LiteralPath $bin)) { return $null }
    Get-ChildItem -LiteralPath $bin -Directory -ErrorAction SilentlyContinue |
        ForEach-Object { Join-Path $_.FullName 'runtime-info.exe' } |
        Where-Object { Test-Path -LiteralPath $_ } |
        Select-Object -First 1
}

$exe = Get-RuntimeInfoExe -CliHome $cliHome
if (-not $exe) {
    $exe = Get-RuntimeInfoExe -CliHome (Join-Path $env:USERPROFILE '.qoder')
}
if (-not $exe) {
    $exe = Get-RuntimeInfoExe -CliHome (Join-Path $env:USERPROFILE '.qoder-cn')
}
if (-not $exe) {
    Write-Error "未找到 runtime-info.exe。请先安装并登录 Qoder 客户端（$cliHome\.bin\umid-*\runtime-info.exe）。"
    exit 1
}

$raw = & $exe
if (-not $raw) {
    Write-Error "runtime-info.exe 没有输出：$exe"
    exit 1
}

try {
    $info = ($raw | Out-String) | ConvertFrom-Json
} catch {
    Write-Error "runtime-info.exe 输出无法解析为 JSON：$raw"
    exit 1
}

if (-not $info.machineToken) {
    Write-Error "runtime-info.exe 输出缺少 machineToken：$raw"
    exit 1
}

$machineID = $null
$idPath = Join-Path $appData 'auth.machine-id'
if (Test-Path -LiteralPath $idPath) {
    $machineID = (Get-Content -LiteralPath $idPath -Raw).Trim()
}

# machineOs 形态与客户端一致：${arch}_${platform}（x64 → x86_64，arm64 → aarch64）。
$arch = if ($env:PROCESSOR_ARCHITECTURE -eq 'ARM64') { 'aarch64' } elseif ([Environment]::Is64BitProcess) { 'x86_64' } else { 'x86' }
$machineOS = "${arch}_win32"

$fp = [ordered]@{
    machine_id    = $machineID
    machine_token = [string]$info.machineToken
    machine_type  = [string]$info.machineType
    machine_code  = [string]$info.machineCode
    machine_os    = $machineOS
    hostname      = $env:COMPUTERNAME
    version       = '0.4.8'
}

if ($Pretty) {
    $fp | ConvertTo-Json -Depth 4
} else {
    $fp | ConvertTo-Json -Depth 4 -Compress
}
