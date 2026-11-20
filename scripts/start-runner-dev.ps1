param(
    [switch]$SelfTest,
    [switch]$NoProxmox
)

$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = [System.Text.UTF8Encoding]::new()
$root = Split-Path -Parent $PSScriptRoot
$goExe = Join-Path $root '.tools\go\bin\go.exe'
$openTofuExe = Join-Path $root '.tools\opentofu\tofu.exe'
$logRoot = Join-Path $root 'tmp\logs'

function Write-Step([string]$Message) {
    Write-Host "`n==> $Message" -ForegroundColor Cyan
}

function Assert-File([string]$Path, [string]$Description) {
    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) {
        throw "$Description не найден: $Path"
    }
}

function Ensure-OpenTofu {
    if (Test-Path -LiteralPath $openTofuExe -PathType Leaf) { return }
    if ($env:PROCESSOR_ARCHITECTURE -ne 'AMD64') {
        throw 'Автоматическая установка portable OpenTofu поддерживает только Windows AMD64.'
    }

    $openTofuDir = Split-Path -Parent $openTofuExe
    $archive = Join-Path $openTofuDir 'tofu.zip'
    $url = 'https://github.com/opentofu/opentofu/releases/download/v1.13.9/tofu_1.13.9_windows_amd64.zip'
    $expectedHash = 'B23A811741911BAA829AA7D3D3718626C549E8934546822B0E9B99B9FBA75E4E'
    New-Item -ItemType Directory -Force -Path $openTofuDir | Out-Null
    Write-Host 'Скачиваю portable OpenTofu 1.13.9...'
    Invoke-WebRequest -Uri $url -OutFile $archive -UseBasicParsing -TimeoutSec 120
    $actualHash = (Get-FileHash -Algorithm SHA256 -LiteralPath $archive).Hash
    if ($actualHash -ne $expectedHash) {
        Remove-Item -LiteralPath $archive -Force -ErrorAction SilentlyContinue
        throw "SHA-256 OpenTofu не совпал: $actualHash"
    }
    Expand-Archive -LiteralPath $archive -DestinationPath $openTofuDir -Force
    Remove-Item -LiteralPath $archive -Force
    Assert-File $openTofuExe 'OpenTofu после установки'
    Write-Host "OpenTofu установлен: $openTofuExe" -ForegroundColor Green
}

function Wait-Management {
    for ($attempt = 1; $attempt -le 30; $attempt++) {
        try {
            $response = Invoke-WebRequest -Uri 'http://127.0.0.1:8080/api/v1/readyz' -UseBasicParsing -TimeoutSec 2
            if ($response.StatusCode -eq 200) { return }
        }
        catch {}
        Start-Sleep -Seconds 1
    }
    throw 'Management API недоступен по адресу http://127.0.0.1:8080. Сначала запустите start-dev.bat.'
}

function Get-RunningDevRunner {
    return Get-CimInstance Win32_Process -ErrorAction SilentlyContinue | Where-Object {
        $_.Name -eq 'go.exe' -and $_.CommandLine -like '*run*./cmd/runner*'
    } | Select-Object -First 1
}

try {
    Write-Step 'Проверка Runner'
    Assert-File $goExe 'Go'
    Ensure-OpenTofu

    $existingRunner = Get-RunningDevRunner
    if ($null -ne $existingRunner) {
        throw "Dev Runner уже запущен (PID $($existingRunner.ProcessId))."
    }

    New-Item -ItemType Directory -Force -Path $logRoot, (Join-Path $root 'tmp\go'), (Join-Path $root 'tmp\runner-jobs'), (Join-Path $root 'tmp\runner-state') | Out-Null
    Wait-Management

    $env:GOTOOLCHAIN = 'local'
    $env:GOPROXY = 'https://goproxy.cn,direct'
    $env:TMP = Join-Path $root 'tmp\go'
    $env:TEMP = $env:TMP
    $env:GOTMPDIR = $env:TMP
    $env:GOCACHE = Join-Path $root '.tools\go\cache'
    $env:RUNNER_ENVIRONMENT = 'development'
    $env:RUNNER_MANAGEMENT_URL = 'http://127.0.0.1:8080'
    $env:RUNNER_RUNNER_TOKEN = 'development-only-runner-token-change-in-production'
    $env:RUNNER_OPENTOFU_BINARY = $openTofuExe
    $env:RUNNER_WORK_ROOT = Join-Path $root 'tmp\runner-jobs'
    $env:RUNNER_STATE_ROOT = Join-Path $root 'tmp\runner-state'

    if ($SelfTest) {
        Write-Host 'Самопроверка запуска Runner.' -ForegroundColor Green
        $env:PROXMOX_VE_ENDPOINT = 'https://pve.example.local:8006/'
        $env:PROXMOX_VE_API_TOKEN = 'self-test@pve!SelfTest=not-a-real-token'
        $env:PROXMOX_VE_INSECURE = 'true'
    }
    elseif ($NoProxmox) {
        Write-Host 'Запуск Runner без настроек Proxmox: задачи не смогут выполнить provider.' -ForegroundColor Yellow
    }
    else {
        $defaultEndpoint = if (-not [string]::IsNullOrWhiteSpace($env:PROXMOX_VE_ENDPOINT)) { $env:PROXMOX_VE_ENDPOINT } else { 'https://pve.example.local:8006' }
        $endpoint = (Read-Host "Proxmox API URL [$defaultEndpoint]").Trim()
        if ([string]::IsNullOrWhiteSpace($endpoint)) { $endpoint = $defaultEndpoint }
        try {
            $parsedEndpoint = [System.Uri]$endpoint
            if (-not $parsedEndpoint.IsAbsoluteUri -or $parsedEndpoint.Scheme -ne 'https') { throw 'Invalid endpoint' }
        }
        catch {
            throw 'Укажите полный HTTPS URL, например https://pve.example.local:8006'
        }
        $env:PROXMOX_VE_ENDPOINT = $endpoint.TrimEnd('/') + '/'

        # Можно вставить сразу полный токен user@realm!id=secret, а можно ответить на три поля по отдельности.
        $identity = (Read-Host 'Пользователь токена (например orchestrator-rw@pve; можно вставить и полный токен вида user@pve!OpenTofu=secret)').Trim()
        if ($identity -match '^([^!@=]+@[^!@=]+)!(.+?)=(.+)$') {
            $userRealm = $Matches[1]
            $tokenId = $Matches[2]
            $tokenSecret = $Matches[3]
        }
        else {
            $userRealm = $identity
            if ($userRealm -notmatch '^[^!@=]+@[^!@=]+$') {
                throw 'Укажите пользователя в формате user@realm, например orchestrator-rw@pve.'
            }
            $tokenId = (Read-Host 'ID токена (например OpenTofu)').Trim()
            if ($tokenId -notmatch '^[^!=]+$') {
                throw 'ID токена не должно содержать ! и =.'
            }
            $secureSecret = Read-Host 'Секрет токена' -AsSecureString
            $tokenSecret = [System.Net.NetworkCredential]::new('', $secureSecret).Password.Trim()
            if ([string]::IsNullOrWhiteSpace($tokenSecret)) { throw 'Секрет токена не может быть пустым.' }
            Remove-Variable secureSecret -ErrorAction SilentlyContinue
        }
        $token = "$userRealm!$tokenId=$tokenSecret"
        $env:PROXMOX_VE_API_TOKEN = $token
        Write-Host "Токен собран: $userRealm!$tokenId=***" -ForegroundColor Green

        $caFile = (Read-Host 'PEM CA Proxmox (Enter — insecure dev mode)').Trim()
        if (-not [string]::IsNullOrWhiteSpace($caFile)) {
            if (-not (Test-Path -LiteralPath $caFile -PathType Leaf)) { throw "CA-файл не найден: $caFile" }
            $env:PROXMOX_VE_CA_FILE = $caFile
            $env:PROXMOX_VE_INSECURE = 'false'
        }
        else {
            Remove-Item Env:PROXMOX_VE_CA_FILE -ErrorAction SilentlyContinue
            $env:PROXMOX_VE_INSECURE = 'true'
        }
        Remove-Variable token, tokenSecret, tokenId, userRealm, identity, endpoint, parsedEndpoint, caFile -ErrorAction SilentlyContinue
    }

    Write-Host "Management:  $env:RUNNER_MANAGEMENT_URL"
    Write-Host "OpenTofu:    $env:RUNNER_OPENTOFU_BINARY"
    Write-Host "Рабочие каталоги: $env:RUNNER_WORK_ROOT и $env:RUNNER_STATE_ROOT"

    if ($SelfTest) {
        $stdout = Join-Path $logRoot 'runner-selftest.out.log'
        $stderr = Join-Path $logRoot 'runner-selftest.err.log'
        Remove-Item -LiteralPath $stdout, $stderr -ErrorAction SilentlyContinue
        $process = Start-Process `
            -FilePath $goExe `
            -ArgumentList @('run', './cmd/runner') `
            -WorkingDirectory $root `
            -RedirectStandardOutput $stdout `
            -RedirectStandardError $stderr `
            -WindowStyle Hidden `
            -PassThru
        Start-Sleep -Seconds 8
        $started = -not $process.HasExited
        & taskkill.exe /PID $process.Id /T /F 2>$null | Out-Null
        if (-not $started) {
            throw "Runner завершился при запуске. Логи: $stdout, $stderr"
        }
        Write-Host 'Самопроверка Runner завершена успешно.' -ForegroundColor Green
        return
    }

    Write-Host "`nRunner запущен. Оставь это окно открытым; Ctrl+C остановит Runner." -ForegroundColor Cyan
    & $goExe run ./cmd/runner
    exit $LASTEXITCODE
}
catch {
    Write-Host "`nОШИБКА: $($_.Exception.Message)" -ForegroundColor Red
    if (-not $SelfTest) { [void](Read-Host 'Нажмите Enter для выхода') }
    exit 1
}
finally {
    Remove-Item Env:PROXMOX_VE_API_TOKEN -ErrorAction SilentlyContinue
}
