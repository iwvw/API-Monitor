param(
    [string]$Message = "提交所有更改"
)

Write-Host "Working directory: $(Get-Location)" -ForegroundColor Cyan

git status --porcelain > $null 2>&1
if ($LASTEXITCODE -ne 0) {
    Write-Host "无法运行 git。请确保已安装 git 并在 PATH 中可用。" -ForegroundColor Red
    exit 1
}

Write-Host "添加全部更改..." -ForegroundColor Yellow
git add -A
if ($LASTEXITCODE -ne 0) {
    Write-Host "git add 失败。检查工作区或权限。" -ForegroundColor Red
    exit 1
}

Write-Host "提交: $Message" -ForegroundColor Yellow
git commit -m $Message
if ($LASTEXITCODE -ne 0) {
    Write-Host "git commit 未创建新的提交（可能没有更改或出错）。查看 'git status' 了解详情。" -ForegroundColor Yellow
} else {
    Write-Host "提交成功." -ForegroundColor Green
}

Write-Host "推送到远程..." -ForegroundColor Yellow
git push
if ($LASTEXITCODE -ne 0) {
    Write-Host "git push 失败：请检查远程配置、凭据或网络。" -ForegroundColor Red
    exit 1
}

Write-Host "推送完成." -ForegroundColor Green
