#Requires -Version 5.1
param(
    [ValidateSet("build", "install", "test", "unit-test", "vet", "lint", "clean")]
    [string]$Task = "build"
)

$ErrorActionPreference = "Stop"

$Binary        = "instill.exe"
$InstallDir    = "$env:LOCALAPPDATA\Programs\instill"
$LintVersion   = "v2.6.2"

switch ($Task) {
    "build" {
        go build -o $Binary .
    }

    "install" {
        New-Item -ItemType Directory -Force $InstallDir | Out-Null
        go build -o "$InstallDir\$Binary" .
        Write-Host "Installed to $InstallDir\$Binary"

        $userPath = [Environment]::GetEnvironmentVariable("PATH", "User")
        $dirs = $userPath -split ";"
        if ($dirs -notcontains $InstallDir) {
            [Environment]::SetEnvironmentVariable("PATH", "$userPath;$InstallDir", "User")
            Write-Host "Added $InstallDir to your user PATH."
            Write-Host "Restart your terminal for the change to take effect."
        } else {
            Write-Host "$InstallDir is already in your PATH."
        }
    }

    "unit-test" {
        go test ./...
    }

    "test" {
        go test ./...
        Write-Host ""
        Write-Host "Note: BATS integration tests require bash and are not supported natively on Windows."
        Write-Host "      Run them under WSL: wsl bats test"
    }

    "vet" {
        go vet ./...
    }

    "lint" {
        go run "github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$LintVersion" run ./...
    }

    "clean" {
        Remove-Item -Force -ErrorAction SilentlyContinue $Binary
        Remove-Item -Recurse -Force -ErrorAction SilentlyContinue dist
    }
}
