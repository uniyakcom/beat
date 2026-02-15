<#
.SYNOPSIS
    运行完整基准测试并生成 benchmarks_*.txt 基线文件 (Windows)

.DESCRIPTION
    自动检测系统 CPU 拓扑，运行 Beat 基准 + 竞品对比，
    输出文件格式与 benchmarks_linux_*.txt 对齐。

.PARAMETER BenchTime
    每项基准运行时长，默认 3s

.PARAMETER Count
    重复次数，默认 1

.PARAMETER SkipCompare
    跳过 _benchmarks 竞品对比

.EXAMPLE
    .\scripts\bench.ps1
    .\scripts\bench.ps1 -BenchTime 5s -Count 3
    .\scripts\bench.ps1 -SkipCompare
#>
[CmdletBinding()]
param(
    [string]$BenchTime = "5s",
    [int]$Count = 3,
    [switch]$SkipCompare
)

Set-StrictMode -Version Latest
$ErrorActionPreference = "Stop"

Push-Location (Split-Path $PSScriptRoot -Parent)
try {

# ── 检测系统信息 ────────────────────────────────────────────────

$cpu = Get-CimInstance Win32_Processor | Select-Object -First 1
$cs  = Get-CimInstance Win32_ComputerSystem | Select-Object -First 1
$os  = Get-CimInstance Win32_OperatingSystem | Select-Object -First 1

$cores   = [int]$cpu.NumberOfCores
$threads = [int]$cs.NumberOfLogicalProcessors
$arch    = $env:PROCESSOR_ARCHITECTURE.ToLower()

$outFile = "benchmarks_windows_${cores}c${threads}t.txt"

Write-Host "=== Beat Benchmark ===" -ForegroundColor Cyan
Write-Host "  OS:       windows/$arch"
Write-Host "  Cores:    $cores  Threads: $threads"
Write-Host "  Output:   $outFile"
Write-Host "  Options:  benchtime=$BenchTime  count=$Count"
Write-Host ""

# ── 辅助函数 ────────────────────────────────────────────────────

function Tee-Line([string]$line) {
    Write-Host $line
    $line
}

# ── 生成基线文件 ────────────────────────────────────────────────

$output = @()

$output += Tee-Line "# $(Get-Date -Format 'yyyy-MM-dd HH:mm:ss K')"
$output += Tee-Line "# $($os.Caption) [$($os.Version)] $arch"
$output += Tee-Line ""

# 系统信息
$output += Tee-Line 'PS> Get-CimInstance Win32_Processor | Format-List Caption, MaxClockSpeed, NumberOfCores, NumberOfLogicalProcessors'
$cpuInfo = $cpu | Format-List Caption, MaxClockSpeed, NumberOfCores, NumberOfLogicalProcessors | Out-String
$cpuInfo.Trim().Split("`n") | ForEach-Object { $output += Tee-Line $_.TrimEnd() }
$output += Tee-Line ""

# 缓存信息
$output += Tee-Line 'PS> Get-CimInstance Win32_CacheMemory | Format-Table Purpose, InstalledSize -AutoSize'
$cacheInfo = Get-CimInstance Win32_CacheMemory | Format-Table Purpose, InstalledSize -AutoSize | Out-String
$cacheInfo.Trim().Split("`n") | ForEach-Object { $output += Tee-Line $_.TrimEnd() }
$output += Tee-Line ""

$output += Tee-Line 'PS> go version'
$goVer = & go version 2>&1
$output += Tee-Line $goVer
$output += Tee-Line ""

# 主基准测试
Write-Host "Running benchmarks (this may take several minutes)..." -ForegroundColor Yellow
$output += Tee-Line "PS> go test -bench=. -benchmem -benchtime=$BenchTime -count=$Count -timeout=600s"
& go 'test' '-bench=.' '-benchmem' "-benchtime=$BenchTime" "-count=$Count" '-timeout=600s' 2>&1 | 
    ForEach-Object { $output += Tee-Line "$_" }

# 竞品对比
if (-not $SkipCompare -and (Test-Path "_benchmarks")) {
    $output += Tee-Line ""
    $output += Tee-Line "# _benchmarks (vs)"
    Write-Host "Running comparison benchmarks (this may take several minutes)..." -ForegroundColor Yellow
    $output += Tee-Line "PS> go test -bench=. -benchmem -benchtime=$BenchTime -count=$Count -timeout=600s .\_benchmarks\"
    Push-Location "_benchmarks"
    try {
        & go 'test' '-bench=.' '-benchmem' "-benchtime=$BenchTime" "-count=$Count" '-timeout=600s' 2>&1 | 
            ForEach-Object { $output += Tee-Line "$_" }
    } finally {
        Pop-Location
    }
}

# 写入文件 (UTF-8 no BOM)
$utf8NoBOM = [System.Text.UTF8Encoding]::new($false)
[System.IO.File]::WriteAllLines(
    (Join-Path $PWD $outFile),
    $output,
    $utf8NoBOM
)

Write-Host ""
Write-Host "=== Done: $outFile ===" -ForegroundColor Green

} finally {
    Pop-Location
}
