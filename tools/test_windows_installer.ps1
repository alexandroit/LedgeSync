<#
Native Windows integration test. Uses only synthetic temporary data and refuses
to run when LedgeSync is already installed for this account. Requires PowerShell 7.
Wizard smoke exercises the actual wizard when an interactive desktop exists;
it does not claim a complete application UI test or a SmartScreen acceptance test.
#>
[CmdletBinding()]
param(
    [Parameter(Mandatory = $true)][string] $Installer,
    [Parameter(Mandatory = $true)][string] $PayloadRoot,
    [Parameter(Mandatory = $true)][ValidateSet('amd64', 'arm64')][string] $Arch,
    [string] $Version = '0.1.0-alpha.1',
    [string] $ReportPath,
    [switch] $WizardSmoke
)

$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
if (-not $IsWindows) { throw 'This integration test requires native Windows.' }
$actualArch = [System.Runtime.InteropServices.RuntimeInformation]::OSArchitecture.ToString().ToLowerInvariant()
$expectedArch = if ($Arch -eq 'amd64') { 'x64' } else { 'arm64' }
if ($actualArch -ne $expectedArch) { throw "Native OS architecture $actualArch does not match $Arch." }
$Installer = (Resolve-Path -LiteralPath $Installer).Path
$PayloadRoot = (Resolve-Path -LiteralPath $PayloadRoot).Path
$keyPath = 'Software\Microsoft\Windows\CurrentVersion\Uninstall\{E6DE1B85-8A9D-47D2-9960-5A26E99A0C2B}_is1'
$userRegistry = [Microsoft.Win32.RegistryKey]::OpenBaseKey([Microsoft.Win32.RegistryHive]::CurrentUser, [Microsoft.Win32.RegistryView]::Registry64)
$machineRegistry = [Microsoft.Win32.RegistryKey]::OpenBaseKey([Microsoft.Win32.RegistryHive]::LocalMachine, [Microsoft.Win32.RegistryView]::Registry64)
$existing = $userRegistry.OpenSubKey($keyPath)
if ($existing) { $existing.Dispose(); throw 'Refusing to test over an existing per-user LedgeSync installation.' }
$existing = $machineRegistry.OpenSubKey($keyPath)
if ($existing) { $existing.Dispose(); throw 'Refusing to test alongside an existing machine LedgeSync installation.' }
$desktopShortcut = Join-Path ([Environment]::GetFolderPath('Desktop')) 'LedgeSync.lnk'
if (Test-Path -LiteralPath $desktopShortcut) { throw 'Refusing to replace an existing LedgeSync desktop shortcut.' }
$testRoot = Join-Path ([System.IO.Path]::GetTempPath()) ('ledgesync-installer-test-' + [guid]::NewGuid())
$installDir = Join-Path $testRoot 'LedgeSync'
$group = 'LedgeSync CI ' + [guid]::NewGuid().ToString('N')
$startShortcut = Join-Path ([Environment]::GetFolderPath('Programs')) ($group + '\LedgeSync.lnk')
New-Item -ItemType Directory -Path $testRoot | Out-Null
$report = [ordered]@{
    architecture = $Arch; version = $Version; installerSHA256 = (Get-FileHash -LiteralPath $Installer -Algorithm SHA256).Hash.ToLowerInvariant()
    nativeOS = [Environment]::OSVersion.VersionString; testDirectory = $testRoot
    wizard = [ordered]@{ status = 'not-requested'; detail = 'No GUI smoke requested.' }
    checks = @(); status = 'running'
}

function Invoke-CheckedProcess([string] $File, [string[]] $Arguments) {
    $process = Start-Process -FilePath $File -ArgumentList $Arguments -PassThru
    if (-not $process.WaitForExit(120000)) {
        & taskkill.exe /PID $process.Id /T /F | Out-Null
        throw "Timed out waiting for $File"
    }
    # Refresh after waiting: Start-Process cached handles otherwise occasionally
    # return a null ExitCode when the child terminates before the first poll.
    $process.Refresh()
    if ($process.ExitCode -ne 0) { throw "$File exited with $($process.ExitCode). Logs: $testRoot" }
}

function Assert-Payload {
    $files = @('LedgeSync.exe', 'LICENSE', 'NOTICE', 'THIRD_PARTY_NOTICES.md')
    $files += @(Get-ChildItem -LiteralPath (Join-Path $PayloadRoot 'third_party') -File -Recurse | ForEach-Object {
        [System.IO.Path]::GetRelativePath($PayloadRoot, $_.FullName)
    })
    foreach ($relative in $files) {
        $source = Join-Path $PayloadRoot $relative
        $installed = Join-Path $installDir $relative
        if (-not (Test-Path -LiteralPath $installed -PathType Leaf)) { throw "Missing installed payload: $relative" }
        if ((Get-FileHash -LiteralPath $source -Algorithm SHA256).Hash -ne (Get-FileHash -LiteralPath $installed -Algorithm SHA256).Hash) {
            throw "Installed bytes differ from released payload: $relative"
        }
    }
    foreach ($relative in @('INSTALL.txt', 'INNO_SETUP_LICENSE.txt', 'unins000.exe')) {
        if (-not (Test-Path -LiteralPath (Join-Path $installDir $relative) -PathType Leaf)) { throw "Missing installation support file: $relative" }
    }
    return $files
}

function Assert-Shortcut([string] $Path) {
    if (-not (Test-Path -LiteralPath $Path -PathType Leaf)) { throw "Missing shortcut: $Path" }
    $shell = New-Object -ComObject WScript.Shell
    try {
        $shortcut = $shell.CreateShortcut($Path)
        if ($shortcut.TargetPath -ne (Join-Path $installDir 'LedgeSync.exe') -or $shortcut.WorkingDirectory -ne $installDir) {
            throw "Wrong shortcut destination: $Path"
        }
    } finally { [void][System.Runtime.InteropServices.Marshal]::FinalReleaseComObject($shell) }
}

function Assert-Registration {
    $key = $userRegistry.OpenSubKey($keyPath)
    if (-not $key) { throw 'Current-user Add/Remove Programs registration is missing.' }
    try {
        if ($key.GetValue('DisplayName') -ne 'LedgeSync' -or $key.GetValue('DisplayVersion') -ne $Version) {
            throw 'Wrong installed name/version in Add/Remove Programs.'
        }
        if ($key.GetValue('InstallLocation').TrimEnd('\') -ne $installDir) { throw 'Wrong registered installation folder.' }
        if ($key.GetValue('UninstallString') -notlike ('*' + $installDir + '\unins000.exe*')) { throw 'Wrong uninstall registration.' }
    } finally { $key.Dispose() }
    $machineKey = $machineRegistry.OpenSubKey($keyPath)
    if ($machineKey) { $machineKey.Dispose(); throw 'Installer unexpectedly registered an all-users installation.' }
}

function Find-Wizard {
    $windows = [System.Windows.Automation.AutomationElement]::RootElement.FindAll(
        [System.Windows.Automation.TreeScope]::Children,
        [System.Windows.Automation.Condition]::TrueCondition)
    foreach ($window in $windows) {
        if ($window.Current.Name -match '^Setup - LedgeSync') { return $window }
    }
    return $null
}

function Find-WizardButton($Window, [string] $NamePattern) {
    $condition = [System.Windows.Automation.PropertyCondition]::new(
        [System.Windows.Automation.AutomationElement]::ControlTypeProperty,
        [System.Windows.Automation.ControlType]::Button)
    $buttons = $Window.FindAll([System.Windows.Automation.TreeScope]::Descendants, $condition)
    foreach ($button in $buttons) {
        if (($button.Current.Name -replace '&', '') -match $NamePattern -and $button.Current.IsEnabled) { return $button }
    }
    return $null
}

function Test-Wizard {
    if (-not [Environment]::UserInteractive -or (Get-Process -Id $PID).SessionId -eq 0) {
        return [ordered]@{ status = 'skipped'; detail = 'No interactive desktop in this runner session; silent installation remains mandatory.' }
    }
    Add-Type -AssemblyName UIAutomationClient
    Add-Type -AssemblyName UIAutomationTypes
    if (Find-Wizard) { throw 'Another LedgeSync installer wizard is already open.' }
    $process = Start-Process -FilePath $Installer -ArgumentList @('/SP-', ('/DIR="' + $installDir + '"'), ('/LOG="' + (Join-Path $testRoot 'wizard.log') + '"')) -PassThru
    try {
        $deadline = [DateTime]::UtcNow.AddSeconds(30)
        do {
            Start-Sleep -Milliseconds 250
            $window = Find-Wizard
        } until ($window -or [DateTime]::UtcNow -gt $deadline)
        if (-not $window) { throw 'Interactive session did not show the LedgeSync setup wizard.' }
        $next = Find-WizardButton $window '^Next\s*>'
        if (-not $next) { throw 'Setup wizard does not have an enabled Next button.' }
        $next.GetCurrentPattern([System.Windows.Automation.InvokePattern]::Pattern).Invoke()
        Start-Sleep -Milliseconds 500
        $window = Find-Wizard
        $back = Find-WizardButton $window '^<\s*Back'
        if (-not $back) { throw 'Next did not navigate to the next wizard page.' }
        $cancel = Find-WizardButton $window '^Cancel$'
        if (-not $cancel) { throw 'Setup wizard is missing Cancel.' }
        $wizardProcessId = $window.Current.ProcessId
        $cancel.GetCurrentPattern([System.Windows.Automation.InvokePattern]::Pattern).Invoke()
        # UIA providers may expose the owned modal dialog either beneath its
        # owner or as another top-level window. Match the exact wizard process.
        $deadline = [DateTime]::UtcNow.AddSeconds(5)
        do {
            Start-Sleep -Milliseconds 250
            $windows = [System.Windows.Automation.AutomationElement]::RootElement.FindAll(
                [System.Windows.Automation.TreeScope]::Children,
                [System.Windows.Automation.Condition]::TrueCondition)
            $yes = $null
            foreach ($candidate in $windows) {
                if ($candidate.Current.ProcessId -eq $wizardProcessId) {
                    $yes = Find-WizardButton $candidate '^Yes$'
                    if ($yes) { break }
                }
            }
            if ($yes) { $yes.GetCurrentPattern([System.Windows.Automation.InvokePattern]::Pattern).Invoke(); break }
            $process.Refresh()
        } until ($process.HasExited -or [DateTime]::UtcNow -gt $deadline)
        if (-not $process.WaitForExit(10000)) { throw 'Wizard did not exit after cancellation.' }
        if (Test-Path -LiteralPath (Join-Path $installDir 'LedgeSync.exe')) { throw 'Cancelling the wizard unexpectedly installed the app.' }
        return [ordered]@{ status = 'passed'; detail = 'Actual welcome window, enabled Next, next page, and cancellation exercised through Windows UI Automation.' }
    } finally {
        $process.Refresh()
        if (-not $process.HasExited) { & taskkill.exe /PID $process.Id /T /F | Out-Null }
    }
}

$trackedFiles = @()
try {
    if ($WizardSmoke) { $report.wizard = Test-Wizard }
    $common = @('/VERYSILENT', '/SUPPRESSMSGBOXES', '/NORESTART', '/SP-', ('/DIR="' + $installDir + '"'), ('/GROUP="' + $group + '"'))
    Invoke-CheckedProcess $Installer ($common + @('/TASKS=""', ('/LOG="' + (Join-Path $testRoot 'install.log') + '"')))
    $trackedFiles = @(Assert-Payload)
    Assert-Shortcut $startShortcut
    Assert-Registration
    if (Test-Path -LiteralPath $desktopShortcut) { throw 'Desktop shortcut was not optional.' }
    $report.checks += 'silent current-user install; all released payload bytes; notices; Start menu; Add/Remove Programs; desktop shortcut absent by default'

    $fixture = Join-Path $installDir 'user-data\keep.txt'
    New-Item -ItemType Directory -Path (Split-Path -Parent $fixture) | Out-Null
    [System.IO.File]::WriteAllText($fixture, 'Synthetic user data must survive upgrade and uninstall.')
    $fixtureHash = (Get-FileHash -LiteralPath $fixture -Algorithm SHA256).Hash
    Invoke-CheckedProcess $Installer ($common + @('/TASKS=desktopicon', ('/LOG="' + (Join-Path $testRoot 'reinstall.log') + '"')))
    $null = Assert-Payload
    Assert-Shortcut $startShortcut
    Assert-Shortcut $desktopShortcut
    Assert-Registration
    if ((Get-FileHash -LiteralPath $fixture -Algorithm SHA256).Hash -ne $fixtureHash) { throw 'Reinstallation changed user data.' }
    $report.checks += 'same-version reinstall; stable registration; optional desktop shortcut; preserved synthetic user data'

    Invoke-CheckedProcess (Join-Path $installDir 'unins000.exe') @('/VERYSILENT', '/SUPPRESSMSGBOXES', '/NORESTART', ('/LOG="' + (Join-Path $testRoot 'uninstall.log') + '"'))
    foreach ($relative in ($trackedFiles + @('INSTALL.txt', 'INNO_SETUP_LICENSE.txt'))) {
        if (Test-Path -LiteralPath (Join-Path $installDir $relative)) { throw "Uninstall left installed payload: $relative" }
    }
    foreach ($shortcut in @($startShortcut, $desktopShortcut)) {
        if (Test-Path -LiteralPath $shortcut) { throw "Uninstall left shortcut: $shortcut" }
    }
    $key = $userRegistry.OpenSubKey($keyPath)
    if ($key) { $key.Dispose(); throw 'Uninstall left the Add/Remove Programs entry.' }
    if (-not (Test-Path -LiteralPath $fixture) -or (Get-FileHash -LiteralPath $fixture -Algorithm SHA256).Hash -ne $fixtureHash) { throw 'Uninstall removed or changed user data.' }
    $report.checks += 'silent uninstall; tracked files and shortcuts removed; registration removed; synthetic user data preserved'
    $report.status = 'passed'
} catch {
    $report.status = 'failed'
    $report.error = $_.Exception.Message
    throw
} finally {
    $userRegistry.Dispose()
    $machineRegistry.Dispose()
    if ($ReportPath) {
        $ReportPath = [System.IO.Path]::GetFullPath($ReportPath)
        [void][System.IO.Directory]::CreateDirectory((Split-Path -Parent $ReportPath))
        [System.IO.File]::WriteAllText($ReportPath, ($report | ConvertTo-Json -Depth 8))
    }
    $report | ConvertTo-Json -Depth 8 | Write-Output
}
