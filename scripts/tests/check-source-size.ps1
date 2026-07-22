$root = Join-Path ([IO.Path]::GetTempPath()) ('pharmacycrm-size-' + [guid]::NewGuid())
try {
    New-Item -ItemType Directory -Force -Path "$root/backend/internal", "$root/frontend/src", "$root/scripts" | Out-Null
    1..400 | Set-Content "$root/backend/internal/at_limit.go"
    1..401 | Set-Content "$root/backend/internal/a.go"
    1..450 | Set-Content "$root/frontend/src/b.tsx"
    $errorOutput = Join-Path $root 'size-errors.txt'
    $process = Start-Process -FilePath (Join-Path $PSHOME 'powershell.exe') -ArgumentList '-NoProfile', '-File', "$PSScriptRoot/../check-source-size.ps1", '-Root', $root -Wait -PassThru -RedirectStandardError $errorOutput
    if ($process.ExitCode -ne 1) { throw 'expected size checker failure' }
    $text = Get-Content -Raw $errorOutput
    if ($text -notmatch 'backend/internal/a.go.*401' -or $text -notmatch 'frontend/src/b.tsx.*450') { throw "unexpected output: $text" }
} finally { Remove-Item -Recurse -Force $root -ErrorAction SilentlyContinue }
