param([string] $Root = (Split-Path -Parent $PSScriptRoot))
$ErrorActionPreference = 'Stop'
$Root = [IO.Path]::GetFullPath($Root).TrimEnd([char]92, [char]47, [char]58)
$sourceExtensions = @('.go', '.ts', '.tsx', '.js', '.jsx', '.sql', '.sh', '.ps1')
$ignoredSegments = @('node_modules', 'vendor', 'dist', 'build', 'coverage', 'tmp', 'generated')
$violations = @()
$roots = @('backend', 'frontend', 'deploy', 'scripts') | ForEach-Object { Join-Path $Root $_ }
Get-ChildItem -Path $roots -Recurse -File | ForEach-Object {
    $relativePath = [IO.Path]::GetRelativePath($Root, $_.FullName) -replace '\\', '/'
    $relativeSegments = $relativePath -split '/'
    $isIgnored = $relativeSegments | Where-Object { $_ -in $ignoredSegments }
    $isGeneratedAPI = $relativePath -like 'frontend/src/shared/api/generated/*'
    if ($_.Extension -in $sourceExtensions -and -not $isIgnored -and -not $isGeneratedAPI) {
        if (-not (Select-String -LiteralPath $_.FullName -Pattern 'Code generated .* DO NOT EDIT\.' -Quiet)) {
            $lines = (Get-Content -LiteralPath $_.FullName | Measure-Object -Line).Lines
            if ($lines -gt 400) { $violations += [PSCustomObject]@{ Path = $relativePath; Lines = $lines } }
        }
    }
}
if ($violations.Count -gt 0) {
    [Console]::Error.WriteLine('architecture check: handwritten source exceeds 400 lines:')
    foreach ($violation in $violations | Sort-Object Path) {
        [Console]::Error.WriteLine(('{0} {1} {2}' -f $violation.Path, [char]0x2014, $violation.Lines))
    }
    exit 1
}
