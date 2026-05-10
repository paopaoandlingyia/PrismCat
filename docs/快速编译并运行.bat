@echo off
setlocal

pushd "%~dp0.."
if errorlevel 1 (
    echo Failed to enter project root.
    pause
    exit /b 1
)

echo [1/4] Stopping old PrismCat process...
taskkill /f /im prismcat.exe >nul 2>nul

echo.
echo [2/4] Building web UI...
pushd web
if errorlevel 1 (
    echo Failed to enter web directory.
    pause
    popd
    exit /b 1
)
call npm install
if errorlevel 1 (
    echo npm install failed.
    pause
    popd
    popd
    exit /b 1
)
call npm run build
if errorlevel 1 (
    echo Web UI build failed.
    pause
    popd
    popd
    exit /b 1
)
popd

echo.
echo [3/4] Syncing embedded UI files...
if not exist "internal\server\ui\README.md" (
    echo Missing embedded UI placeholder: internal\server\ui\README.md
    pause
    popd
    exit /b 1
)
git clean -fdX -- internal/server/ui
if errorlevel 1 (
    echo Failed to clean generated embedded UI files.
    pause
    popd
    exit /b 1
)
if not exist "internal\server\ui" mkdir "internal\server\ui"
if errorlevel 1 (
    echo Failed to create embedded UI directory.
    pause
    popd
    exit /b 1
)
xcopy /s /e /y "web\dist\*" "internal\server\ui\" >nul
if errorlevel 1 (
    echo Failed to sync embedded UI files.
    pause
    popd
    exit /b 1
)

echo.
echo [4/4] Building PrismCat executable...
set CGO_ENABLED=0
go build -ldflags="-H windowsgui -s -w" -o prismcat_new.exe ./cmd/prismcat/
if errorlevel 1 (
    echo Go build failed. Please check Go installation and make sure prismcat.exe is not locked.
    pause
    popd
    exit /b 1
)

if exist prismcat.exe del /f prismcat.exe
if errorlevel 1 (
    echo Failed to replace old prismcat.exe. Please close PrismCat and retry.
    pause
    popd
    exit /b 1
)
move /y prismcat_new.exe prismcat.exe >nul
if errorlevel 1 (
    echo Failed to move new executable into place.
    pause
    popd
    exit /b 1
)

echo.
echo PrismCat built successfully. Starting...
start "" prismcat.exe
popd
exit /b 0
