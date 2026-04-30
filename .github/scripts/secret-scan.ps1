$ErrorActionPreference = "Stop"

$repoRoot = Resolve-Path (Join-Path $PSScriptRoot "..\..")
Set-Location $repoRoot

$excludedPathPatterns = @(
    '(^|/)go/\.env\.example$',
    '(^|/)\.gitmodules$'
)

$secretPatterns = @(
    @{
        Name = "Private key block"
        Regex = '-----BEGIN (RSA |EC |OPENSSH |DSA |)?PRIVATE KEY-----'
    },
    @{
        Name = "OpenAI-style API key"
        Regex = 'sk-(proj-)?[A-Za-z0-9_-]{32,}'
    },
    @{
        Name = "GitHub token"
        Regex = 'gh[pousr]_[A-Za-z0-9_]{36,}'
    },
    @{
        Name = "AWS access key"
        Regex = 'AKIA[0-9A-Z]{16}'
    },
    @{
        Name = "Google API key"
        Regex = 'AIza[0-9A-Za-z_-]{35}'
    },
    @{
        Name = "Slack token"
        Regex = 'xox[baprs]-[0-9A-Za-z-]{20,}'
    },
    @{
        Name = "Exchange credential assignment"
        Regex = '(?i)(hyperliquid|binance|okx|bybit|exchange).{0,40}(private[_-]?key|secret|api[_-]?key)\s*[:=]\s*[''"]?[0-9a-f]{40,}'
    }
)

$trackedFiles = git ls-files
$findings = New-Object System.Collections.Generic.List[string]

foreach ($file in $trackedFiles) {
    $normalized = $file -replace '\\', '/'
    $excluded = $false
    foreach ($exclude in $excludedPathPatterns) {
        if ($normalized -match $exclude) {
            $excluded = $true
            break
        }
    }
    if ($excluded) {
        continue
    }

    $fullPath = Join-Path $repoRoot $file
    if (-not (Test-Path -LiteralPath $fullPath -PathType Leaf)) {
        continue
    }

    try {
        $lines = Get-Content -LiteralPath $fullPath -ErrorAction Stop
    } catch {
        continue
    }

    for ($i = 0; $i -lt $lines.Count; $i++) {
        foreach ($pattern in $secretPatterns) {
            if ($lines[$i] -match $pattern.Regex) {
                $findings.Add(("{0}:{1}: {2}" -f $normalized, ($i + 1), $pattern.Name))
            }
        }
    }
}

if ($findings.Count -gt 0) {
    Write-Error ("Potential secrets found:`n" + ($findings -join "`n"))
}

Write-Host "Secret scan completed without findings."
