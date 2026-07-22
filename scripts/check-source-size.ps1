param([string] $Root = (Split-Path -Parent $PSScriptRoot))

$ErrorActionPreference = 'Stop'
$Root = [IO.Path]::GetFullPath($Root).TrimEnd([char]92, [char]47, [char]58)
$sourceExtensions = @('.go', '.ts', '.tsx', '.js', '.jsx', '.sql', '.sh', '.ps1')
$ignoredSegments = @('node_modules', 'vendor', 'dist', 'build', 'coverage', 'tmp', 'generated')
$violations = @()
Get-ChildItem -Path (Join-Path $Root 'backend'), (Join-Path $Root 'frontend'), (Join-Path $Root 'scripts') -Recurse -File | Where-Object {
    $_.Extension -in $sourceExtensions -and $_.FullName -notmatch '[\\/]frontend[\\/]src[\\/]shared[\\/]api[\\/]generated[\\/]' -and -not ($_.FullName -split '[\\/]' | Where-Object { $_ -in $ignoredSegments })
} | ForEach-Object {
    if (-not (Select-String -LiteralPath $_.FullName -Pattern 'Code generated .* DO NOT EDIT\.' -Quiet)) {
        $lines = (Get-Content -LiteralPath $_.FullName | Measure-Object -Line).Lines
        if ($lines -gt 400) { $violations += [PSCustomObject]@{ Path = $_.FullName.Substring($Root.Length).TrimStart([char]92, [char]47) -replace '\\', '/'; Lines = $lines } }
    }
}
if ($violations.Count -gt 0) {
    [Console]::Error.WriteLine('architecture check: handwritten source exceeds 400 lines:')
    foreach ($violation in $violations | Sort-Object Path) { [Console]::Error.WriteLine(('{0} {1} {2}' -f $violation.Path, [char]0x2014, $violation.Lines)) }
    exit 1
}
