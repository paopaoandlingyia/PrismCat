param(
    [switch]$SkipBuild
)

$ErrorActionPreference = "Stop"

$repoRoot = git rev-parse --show-toplevel
if (-not $repoRoot) {
    throw "This script must be run inside a Git checkout."
}

$webDir = Join-Path $repoRoot "web"
$distDir = Join-Path $webDir "dist"
$embedDir = Join-Path $repoRoot "internal/server/ui"
$placeholder = Join-Path $embedDir "README.md"

if (-not (Test-Path -LiteralPath $placeholder)) {
    throw "Missing tracked placeholder: internal/server/ui/README.md"
}

if (-not $SkipBuild) {
    Push-Location $webDir
    try {
        npm run build
    } finally {
        Pop-Location
    }
}

if (-not (Test-Path -LiteralPath (Join-Path $distDir "index.html"))) {
    throw "Missing web/dist/index.html. Run without -SkipBuild or build the frontend first."
}

Push-Location $repoRoot
try {
    git clean -fdX -- internal/server/ui
} finally {
    Pop-Location
}

Copy-Item -Path (Join-Path $distDir "*") -Destination $embedDir -Recurse -Force
