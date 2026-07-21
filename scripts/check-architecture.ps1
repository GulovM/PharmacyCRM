Set-StrictMode -Version Latest
$ErrorActionPreference = 'Stop'

$rootDirectory = Split-Path -Parent $PSScriptRoot
Set-Location $rootDirectory

function Fail([string] $Message) {
    [Console]::Error.WriteLine("architecture check: $Message")
    exit 1
}

function Require-Directory([string] $Path) {
    if (-not (Test-Path -LiteralPath $Path -PathType Container)) {
        Fail "required directory is missing: $Path"
    }
}

function Test-RipgrepMatch([string[]] $Arguments) {
    & rg @Arguments | Out-Null
    if ($LASTEXITCODE -eq 0) {
        return $true
    }
    if ($LASTEXITCODE -eq 1) {
        return $false
    }
    Fail 'ripgrep failed while scanning the repository'
}

$requiredDirectories = @(
    'backend',
    'backend/cmd/api', 'backend/cmd/worker', 'backend/cmd/migrate',
    'backend/internal/bootstrap', 'backend/internal/platform', 'backend/internal/shared',
    'backend/internal/orchestration', 'backend/internal/modules', 'backend/migrations', 'backend/test',
    'frontend', 'frontend/src/app', 'frontend/src/pages', 'frontend/src/widgets', 'frontend/src/features',
    'frontend/src/entities', 'frontend/src/shared', 'frontend/src/test', 'frontend/e2e',
    'deploy', 'docs'
)
foreach ($directory in $requiredDirectories) {
    Require-Directory $directory
}

$forbiddenPaths = @(
    'backend/internal/handlers', 'backend/internal/services', 'backend/internal/repositories',
    'backend/internal/models', 'backend/internal/utils', 'frontend/src/api.ts'
)
foreach ($forbidden in $forbiddenPaths) {
    if (Test-Path -LiteralPath $forbidden) {
        Fail "forbidden path exists: $forbidden"
    }
}

if (Get-ChildItem -Path backend -Recurse -File | Where-Object { $_.Extension -in '.ts', '.tsx', '.jsx', '.vue' } | Select-Object -First 1) {
    Fail 'frontend source must not be placed in backend/'
}

if (Get-ChildItem -Path frontend -Recurse -File -Filter '*.go' | Select-Object -First 1) {
    Fail 'backend Go source must not be placed in frontend/'
}

if (Test-RipgrepMatch @('-n', '--glob', '*.go', '"[^"\n]*frontend/', 'backend')) {
    Fail 'backend Go source must not import frontend source'
}

if (Test-RipgrepMatch @('-n', '--glob', '*.{ts,tsx,js,jsx}', 'from [''"][^''"]*backend/|import\([''"][^''"]*backend/', 'frontend')) {
    Fail 'frontend source must not import backend source'
}

if (-not (Test-Path -LiteralPath 'backend/go.mod' -PathType Leaf)) {
    Fail 'backend must remain an independent Go module'
}

if (-not (Test-Path -LiteralPath 'frontend/package.json' -PathType Leaf)) {
    Fail 'frontend must remain an independent JavaScript application root'
}

if ((Test-Path -LiteralPath 'frontend/package-lock.json') -or (Test-Path -LiteralPath 'frontend/yarn.lock')) {
    Fail 'frontend must use pnpm only; npm and Yarn lockfiles are forbidden'
}

Write-Output 'architecture check: passed'
