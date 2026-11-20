param(
    [switch]$NoProxmox,
    [switch]$NoBrowser,
    [switch]$SelfTest
)

$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = [System.Text.UTF8Encoding]::new()
$root = Split-Path -Parent $PSScriptRoot
$pgBin = Join-Path $root '.tools\postgresql\postgresql-17.12.0-x86_64-pc-windows-msvc\bin'
$pgData = Join-Path $root '.tools\pgdata'
$goExe = Join-Path $root '.tools\go\bin\go.exe'
$webRoot = Join-Path $root 'web'
$logRoot = Join-Path $root 'tmp\logs'
$ownedProcesses = @()

function Write-Step([string]$Message) {
    Write-Host "`n==> $Message" -ForegroundColor Cyan
}

function Assert-File([string]$Path, [string]$Description) {
    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) {
        throw "$Description не найден: $Path"
    }
}

function Wait-Http([string]$Name, [string]$Url, [int]$Attempts = 60) {
    for ($attempt = 1; $attempt -le $Attempts; $attempt++) {
        try {
            $response = Invoke-WebRequest -Uri $Url -UseBasicParsing -TimeoutSec 2
            if ($response.StatusCode -eq 200) {
                Write-Host "$Name доступен: $Url" -ForegroundColor Green
                return
            }
        }
        catch {
            if ($attempt % 10 -eq 0) {
                Write-Host "Ожидание $Name... $attempt/$Attempts"
            }
        }
        Start-Sleep -Seconds 1
    }
    throw "$Name не запустился за $Attempts секунд. Проверьте логи в $logRoot"
}

function Start-LoggedProcess(
    [string]$Name,
    [string]$FilePath,
    [string[]]$Arguments,
    [string]$WorkingDirectory
) {
    $stdout = Join-Path $logRoot ($Name + '.out.log')
    $stderr = Join-Path $logRoot ($Name + '.err.log')
    $process = Start-Process `
        -FilePath $FilePath `
        -ArgumentList $Arguments `
        -WorkingDirectory $WorkingDirectory `
        -RedirectStandardOutput $stdout `
        -RedirectStandardError $stderr `
        -WindowStyle Hidden `
        -PassThru
    $script:ownedProcesses += [PSCustomObject]@{ Name = $Name; Process = $process }
    Write-Host "$Name запущен (PID $($process.Id))" -ForegroundColor Green
    return $process
}

function Stop-OwnedProcesses {
    for ($index = $script:ownedProcesses.Count - 1; $index -ge 0; $index--) {
        $entry = $script:ownedProcesses[$index]
        if ($null -ne $entry.Process -and -not $entry.Process.HasExited) {
            Write-Host "Останавливаю $($entry.Name)..." -ForegroundColor Yellow
            & taskkill.exe /PID $entry.Process.Id /T /F 2>$null | Out-Null
        }
    }
}

try {
    Write-Step 'Проверка файлов проекта'
    Assert-File (Join-Path $pgBin 'postgres.exe') 'PostgreSQL'
    Assert-File (Join-Path $pgBin 'pg_isready.exe') 'pg_isready'
    Assert-File $goExe 'Go'
    Assert-File (Join-Path $webRoot 'package.json') 'Frontend package.json'
    Assert-File (Join-Path $webRoot 'node_modules\.bin\vite.cmd') 'Vite (выполните npm ci в каталоге web)'
    if (-not (Test-Path -LiteralPath $pgData -PathType Container)) {
        throw "Каталог PostgreSQL не найден: $pgData"
    }
    $npm = Get-Command npm.cmd -ErrorAction Stop
    New-Item -ItemType Directory -Force -Path $logRoot | Out-Null
    Write-Host "Корень проекта: $root"
    Write-Host "Логи: $logRoot"

    Write-Step 'PostgreSQL на порту 55432'
    & (Join-Path $pgBin 'pg_isready.exe') -h 127.0.0.1 -p 55432 *> $null
    if ($LASTEXITCODE -ne 0) {
        Start-LoggedProcess 'postgresql' (Join-Path $pgBin 'postgres.exe') @('-D', $pgData, '-p', '55432') $root | Out-Null
    }
    else {
        Write-Host 'PostgreSQL уже запущен; существующий процесс не останавливается этим скриптом.' -ForegroundColor Yellow
    }
    $pgReady = $false
    for ($attempt = 1; $attempt -le 30; $attempt++) {
        & (Join-Path $pgBin 'pg_isready.exe') -h 127.0.0.1 -p 55432 *> $null
        if ($LASTEXITCODE -eq 0) { $pgReady = $true; break }
        Start-Sleep -Seconds 1
    }
    if (-not $pgReady) { throw 'PostgreSQL не отвечает на порту 55432' }
    Write-Host 'PostgreSQL готов.' -ForegroundColor Green

    $databaseExists = & (Join-Path $pgBin 'psql.exe') -h 127.0.0.1 -p 55432 -U postgres -d postgres -tAc "SELECT 1 FROM pg_database WHERE datname='orchestrator'"
    if ($databaseExists.Trim() -ne '1') {
        Write-Host 'Создаю базу orchestrator...'
        & (Join-Path $pgBin 'createdb.exe') -h 127.0.0.1 -p 55432 -U postgres orchestrator
        if ($LASTEXITCODE -ne 0) { throw 'Не удалось создать базу orchestrator' }
    }

    Write-Step 'Management API на порту 8080'
    $env:GOTOOLCHAIN = 'local'
    $env:MANAGEMENT_ENVIRONMENT = 'development'
    $env:MANAGEMENT_ADDRESS = ':8080'
    $env:MANAGEMENT_DATABASE_URL = 'postgres://postgres@127.0.0.1:55432/orchestrator?sslmode=disable'
    $env:MANAGEMENT_AUTH_KEY = 'AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA='
    $env:MANAGEMENT_ENCRYPTION_KEY = 'BBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBBB='
    $env:MANAGEMENT_RUNNER_TOKEN = 'development-only-runner-token-change-in-production'
    $configuredProxmoxUrl = $env:MANAGEMENT_PROXMOX_API_URL
    Remove-Item Env:MANAGEMENT_PROXMOX_API_URL -ErrorAction SilentlyContinue
    Remove-Item Env:MANAGEMENT_PROXMOX_API_TOKEN -ErrorAction SilentlyContinue
    Remove-Item Env:MANAGEMENT_PROXMOX_INSECURE_SKIP_VERIFY -ErrorAction SilentlyContinue

    if (-not $NoProxmox) {
        $defaultProxmoxUrl = if (-not [string]::IsNullOrWhiteSpace($configuredProxmoxUrl)) { $configuredProxmoxUrl } else { 'https://pve.example.local:8006' }
        $proxmoxUrl = (Read-Host "Proxmox API URL [$defaultProxmoxUrl]").Trim()
        if ([string]::IsNullOrWhiteSpace($proxmoxUrl)) { $proxmoxUrl = $defaultProxmoxUrl }
        try {
            $parsedProxmoxUrl = [System.Uri]$proxmoxUrl
            if (-not $parsedProxmoxUrl.IsAbsoluteUri -or $parsedProxmoxUrl.Scheme -ne 'https') {
                throw 'Invalid Proxmox API URL'
            }
        }
        catch {
            throw 'Укажите полный HTTPS URL, например https://pve.example.local:8006'
        }
        $env:MANAGEMENT_PROXMOX_API_URL = $proxmoxUrl.TrimEnd('/')

        $tokenUser = (Read-Host 'Пользователь Proxmox [foundry-ro@pve]').Trim()
        if ([string]::IsNullOrWhiteSpace($tokenUser)) { $tokenUser = 'foundry-ro@pve' }
        if (-not $tokenUser.Contains('@') -or $tokenUser.Contains('!') -or $tokenUser.Contains('=')) {
            throw 'Пользователь должен иметь формат name@pve, например foundry-rw@pve'
        }

        $defaultTokenName = if ($tokenUser -eq 'foundry-rw@pve') { 'OpenTofu' } else { 'Audit' }
        $tokenName = (Read-Host "Имя токена [$defaultTokenName]").Trim()
        if ([string]::IsNullOrWhiteSpace($tokenName)) { $tokenName = $defaultTokenName }
        if ($tokenName.Contains('!') -or $tokenName.Contains('=') -or $tokenName.Contains(' ')) {
            throw 'Введите только имя токена, например OpenTofu или Audit'
        }

        $secureSecret = Read-Host 'Секретный код токена (только код после =; пустой ввод — без Proxmox)' -AsSecureString
        $secret = [System.Net.NetworkCredential]::new('', $secureSecret).Password.Trim()
        if (-not [string]::IsNullOrWhiteSpace($secret)) {
            $env:MANAGEMENT_PROXMOX_API_TOKEN = "$tokenUser!$tokenName=$secret"
            if ([string]::IsNullOrWhiteSpace($env:MANAGEMENT_PROXMOX_CA_FILE)) {
                $env:MANAGEMENT_PROXMOX_INSECURE_SKIP_VERIFY = 'true'
            }
            Write-Host "Proxmox включён для $tokenUser!$tokenName" -ForegroundColor Green
        }
        else {
            Write-Host 'Proxmox не настроен: панель покажет соответствующее состояние.' -ForegroundColor Yellow
        }
        Remove-Variable secureSecret, secret, tokenUser, tokenName, defaultTokenName, configuredProxmoxUrl, defaultProxmoxUrl, proxmoxUrl, parsedProxmoxUrl -ErrorAction SilentlyContinue
    }
    else {
        Write-Host 'Запуск без Proxmox (-NoProxmox).' -ForegroundColor Yellow
    }

    Start-LoggedProcess 'management' $goExe @('run', './cmd/server') $root | Out-Null
    Remove-Item Env:MANAGEMENT_PROXMOX_API_TOKEN -ErrorAction SilentlyContinue
    Wait-Http 'Management API' 'http://127.0.0.1:8080/api/v1/readyz'

    Write-Step 'Vite UI на порту 7100'
    Start-LoggedProcess 'vite' $npm.Source @('run', 'dev', '--', '--host', '127.0.0.1', '--port', '7100') $webRoot | Out-Null
    Wait-Http 'Vite UI' 'http://127.0.0.1:7100/'

    Write-Host "`nГотово:" -ForegroundColor Cyan
    Write-Host '  UI:        http://127.0.0.1:7100/'
    Write-Host '  Health:    http://127.0.0.1:8080/api/v1/healthz'
    Write-Host '  Readiness: http://127.0.0.1:8080/api/v1/readyz'
    Write-Host "  Логи:      $logRoot"

    if ($SelfTest) {
        Write-Host 'Самопроверка запуска завершена успешно.' -ForegroundColor Green
        return
    }

    if (-not $NoBrowser) {
        Start-Process 'http://127.0.0.1:7100/'
    }
    Write-Host "`nНажмите Enter, чтобы остановить запущенные скриптом процессы." -ForegroundColor Cyan
    [void](Read-Host)
}
catch {
    Write-Host "`nОШИБКА: $($_.Exception.Message)" -ForegroundColor Red
    Write-Host "Проверьте логи: $logRoot" -ForegroundColor Yellow
    if (-not $SelfTest) { [void](Read-Host 'Нажмите Enter для выхода') }
    exit 1
}
finally {
    Stop-OwnedProcesses
}
