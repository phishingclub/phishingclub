package service

import (
	"context"
	"regexp"
	"strconv"
	"strings"

	"github.com/go-errors/errors"
	"github.com/google/uuid"
	"github.com/phishingclub/phishingclub/data"
	"github.com/phishingclub/phishingclub/errs"
	"github.com/phishingclub/phishingclub/model"
	"github.com/phishingclub/phishingclub/repository"
	"github.com/phishingclub/phishingclub/validate"
	"github.com/phishingclub/phishingclub/vo"
	"gorm.io/gorm"
)

// AllowListingSyncPathPattern is the form of the seeded runner path
var AllowListingSyncPathPattern = regexp.MustCompile(`^[a-z0-9]{32}$`)

var allowListingHostPattern = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z0-9-]{2,63}$`)

// allowListingExchangeModuleVersion is the pinned version of the Exchange
// Online module the runbook requires. Versions 3.5.0 to 3.9.2 need
// PowerShell 7.4, which Azure Automation offers as a runtime environment.
const allowListingExchangeModuleVersion = "3.9.2"

// AllowListingRunbook is the runner script with the values the setup
// steps name
type AllowListingRunbook struct {
	FileName string `json:"fileName"`
	Runbook  string `json:"runbook"`
	// RunnerURL is the base URL the runbook calls
	RunnerURL string `json:"runnerURL"`
	// ModuleName and ModuleVersion is the Exchange Online module the
	// runbook requires
	ModuleName    string `json:"moduleName"`
	ModuleVersion string `json:"moduleVersion"`
	// Schedule is how often the runbook schedule must run, such as every hour
	Schedule string `json:"schedule"`
	// PermissionScript gives the managed identity the rights it needs
	PermissionScript string `json:"permissionScript"`
}

var errAllowListingNoPath = errors.New("the runner path is not set up, restart Phishing Club")

// runnerPath returns the seeded random path the runner endpoints are served on
func (s *AllowListingSync) runnerPath(ctx context.Context) (string, error) {
	opt, err := s.OptionRepository.GetByKey(ctx, data.OptionKeyAllowListingSyncPath)
	if err != nil {
		return "", errAllowListingNoPath
	}
	path := opt.Value.String()
	if !AllowListingSyncPathPattern.MatchString(path) {
		return "", errAllowListingNoPath
	}
	return path, nil
}

// Runbook returns the runner script of the target of a scope. The runner
// calls Phishing Club on the domain given, which must be a domain the scope
// may use. Every value put in the script is checked against a strict form,
// so nothing in it can break out of its quotes.
func (s *AllowListingSync) Runbook(
	ctx context.Context,
	session *model.Session,
	companyID *uuid.UUID,
	domainName string,
) (_ *AllowListingRunbook, err error) {
	ae := allowListingAudit("AllowListingSync.Runbook", session, companyID)
	defer s.auditDone(ae, &err)
	if err := s.authorize(session, ae); err != nil {
		return nil, err
	}
	target, err := s.target(ctx, companyID)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	host, err := s.checkRunnerDomain(ctx, companyID, domainName)
	if err != nil {
		return nil, err
	}
	path, err := s.runnerPath(ctx)
	if err != nil {
		return nil, err
	}
	base := "https://" + host + "/" + path + "/" + target.ID.String()
	runbook := strings.NewReplacer(
		"__BASE_URL__", base,
		"__MODULE_VERSION__", allowListingExchangeModuleVersion,
	).Replace(allowListingRunbookTemplate)
	// the runbook holds the runner path, so who fetched it is recorded
	ae.Details["targetId"] = target.ID.String()
	ae.Details["domain"] = host
	s.AuditLogAuthorized(ae)
	return &AllowListingRunbook{
		FileName:         "PhishingClubAllowListingSync.ps1",
		Runbook:          runbook,
		RunnerURL:        base,
		ModuleName:       "ExchangeOnlineManagement",
		ModuleVersion:    allowListingExchangeModuleVersion,
		Schedule:         allowListingScheduleText(target.IntervalMinutes),
		PermissionScript: allowListingPermissionScript,
	}, nil
}

var (
	errAllowListingNoDomain    = errors.New("no such domain")
	errAllowListingProxyDomain = errors.New("a proxy domain can not be used")
)

// checkRunnerDomain returns a domain in its normal form when the runner of a
// scope may call it: a domain the scope may use that is not a proxy domain.
// A proxy domain hands unknown paths to the proxied site, a runner call that
// fails would go there.
func (s *AllowListingSync) checkRunnerDomain(ctx context.Context, companyID *uuid.UUID, domainName string) (string, error) {
	host := strings.TrimSuffix(strings.ToLower(strings.TrimSpace(domainName)), ".")
	if !allowListingHostPattern.MatchString(host) {
		return "", validate.WrapErrorWithField(errors.New("not a domain"), "domain")
	}
	name, err := vo.NewString255(host)
	if err != nil {
		return "", validate.WrapErrorWithField(err, "domain")
	}
	domain, err := s.DomainRepository.GetByName(ctx, name, &repository.DomainOption{})
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", validate.WrapErrorWithField(errAllowListingNoDomain, "domain")
		}
		return "", errs.Wrap(err)
	}
	if owner, err := domain.CompanyID.Get(); err == nil && (companyID == nil || owner != *companyID) {
		return "", validate.WrapErrorWithField(errAllowListingNoDomain, "domain")
	}
	if domain.ProxyID.IsSpecified() && !domain.ProxyID.IsNull() {
		return "", validate.WrapErrorWithField(errAllowListingProxyDomain, "domain")
	}
	return host, nil
}

// runner domain issues
const (
	AllowListingRunnerDomainDeleted = "deleted"
	AllowListingRunnerDomainProxy   = "proxy"
)

// withRunnerDomainIssue sets the issue of the runner domain of a target, if
// the domain was deleted, moved out of the scope or made a proxy domain
func (s *AllowListingSync) withRunnerDomainIssue(ctx context.Context, target *model.AllowListingTarget) (*model.AllowListingTarget, error) {
	if target == nil || target.RunnerDomain == "" {
		return target, nil
	}
	_, err := s.checkRunnerDomain(ctx, target.CompanyID, target.RunnerDomain)
	if err == nil {
		return target, nil
	}
	var verr errs.ValidationError
	if !errors.As(err, &verr) {
		return nil, err
	}
	// a stored domain that is no longer usable is deleted, moved out of the
	// scope or a proxy domain now
	target.RunnerDomainIssue = AllowListingRunnerDomainDeleted
	if errors.Is(verr.Err, errAllowListingProxyDomain) {
		target.RunnerDomainIssue = AllowListingRunnerDomainProxy
	}
	return target, nil
}

// SetRunnerDomain stores the domain the runner of a scope calls. The runbook
// has to be downloaded again for a change to reach the tenant.
func (s *AllowListingSync) SetRunnerDomain(
	ctx context.Context,
	session *model.Session,
	companyID *uuid.UUID,
	domainName string,
) (_ *model.AllowListingTarget, err error) {
	ae := allowListingAudit("AllowListingSync.SetRunnerDomain", session, companyID)
	defer s.auditDone(ae, &err)
	if err := s.authorize(session, ae); err != nil {
		return nil, err
	}
	target, err := s.target(ctx, companyID)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	host, err := s.checkRunnerDomain(ctx, companyID, domainName)
	if err != nil {
		return nil, err
	}
	defer s.lockTarget(target.ID)()
	if err := s.Repository.SetTargetRunnerDomain(ctx, &target.ID, host); err != nil {
		return nil, errs.Wrap(err)
	}
	ae.Details["targetId"] = target.ID.String()
	ae.Details["previousDomain"] = target.RunnerDomain
	ae.Details["domain"] = host
	s.AuditLogAuthorized(ae)
	updated, err := s.target(ctx, companyID)
	if err != nil {
		return nil, errs.Wrap(err)
	}
	return s.withRunnerDomainIssue(ctx, updated)
}

// allowListingPermissionScript gives the managed identity of the Automation
// account the Exchange.ManageAsApp permission and the Security Administrator
// role. It runs in Microsoft Graph PowerShell as a Global Administrator or
// Privileged Role Administrator.
const allowListingPermissionScript = `Connect-MgGraph -Scopes 'AppRoleAssignment.ReadWrite.All','Application.Read.All','RoleManagement.ReadWrite.Directory'
$mi = @(Get-MgServicePrincipal -Filter "displayName eq 'NAME OF THE AUTOMATION ACCOUNT'" | Where-Object { $_.ServicePrincipalType -eq 'ManagedIdentity' })
if ($mi.Count -ne 1) { throw "Expected one managed identity with that name, found $($mi.Count)" }
$mi = $mi[0]
$exo = Get-MgServicePrincipal -Filter "appId eq '00000002-0000-0ff1-ce00-000000000000'"
$appRole = $exo.AppRoles | Where-Object { $_.Value -eq 'Exchange.ManageAsApp' }
if (-not $appRole) { throw 'The Office 365 Exchange Online app was not found in the tenant' }
New-MgServicePrincipalAppRoleAssignment -ServicePrincipalId $mi.Id -PrincipalId $mi.Id -ResourceId $exo.Id -AppRoleId $appRole.Id
New-MgRoleManagementDirectoryRoleAssignment -PrincipalId $mi.Id -RoleDefinitionId '194ae4cb-b126-40b2-bd5b-6091b380977d' -DirectoryScopeId '/'
`

// allowListingScheduleText names an interval for the setup steps
func allowListingScheduleText(intervalMinutes int) string {
	switch {
	case intervalMinutes <= 60:
		return "every hour"
	case intervalMinutes == 1440:
		return "every day"
	default:
		return "every " + strconv.Itoa(intervalMinutes/60) + " hours"
	}
}

// allowListingRunbookTemplate is the runner. Every decision is made by
// Phishing Club, the runner only checks each value on its own as well: it
// adds only exact hosts and narrow IP ranges, and removes only values it
// added itself.
const allowListingRunbookTemplate = `<#
Phishing Club allow listing sync

Runs in Azure Automation with a system assigned managed identity. Each run
reads the Microsoft Defender Advanced Delivery phishing simulation lists,
fetches a plan from Phishing Club, applies it, reads the lists again and
reports both reads. Phishing Club decides what is added and removed, the
runbook keeps no state between runs.

Needs the encrypted Automation variable PhishingClubSyncToken. The tenant
domain is set in $Organization below.
#>
$ErrorActionPreference = 'Stop'

$BaseUrl = '__BASE_URL__'

# the .onmicrosoft.com domain of the tenant, filled in by Phishing Club when
# the runbook is downloaded
$Organization = '__TENANT_DOMAIN__'

# Optional limits set by the tenant. When a list is not empty, any value
# outside it is refused even when Phishing Club asks to add it.
$AllowedSenderIPs = @()
$AllowedDomainSuffixes = @()

$MaxChanges = 60
$DomainPattern = '^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z0-9-]{2,63}$'
$UrlPattern = '^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z0-9-]{2,63}/\*$'

$Token = Get-AutomationVariable -Name 'PhishingClubSyncToken'
$Headers = @{ Authorization = "Bearer $($Token)" }
$RunErrors = New-Object System.Collections.Generic.List[string]

function Test-Suffix([string]$Name) {
    if ($AllowedDomainSuffixes.Count -eq 0) { return $true }
    foreach ($item in $AllowedDomainSuffixes) {
        $suffix = "$($item)".ToLower()
        if ($Name -eq $suffix -or $Name.EndsWith('.' + $suffix)) { return $true }
    }
    return $false
}

# Test-IP accepts one address in its normal form, or a network of at least
# /24 for IPv4 and /48 for IPv6
function Test-IP([string]$Value) {
    $parts = $Value.Split('/')
    if ($parts.Count -gt 2) { return $false }
    $parsed = $null
    if (-not [System.Net.IPAddress]::TryParse($parts[0], [ref]$parsed)) { return $false }
    if ($parsed.ToString() -ne $parts[0]) { return $false }
    # an IPv4 address written as IPv6 would be checked against the wrong width
    if ($parsed.IsIPv4MappedToIPv6) { return $false }
    if ($parts.Count -eq 2) {
        if ($parts[1] -notmatch '^[0-9]{1,3}$') { return $false }
        $prefix = [int]$parts[1]
        if ($parsed.AddressFamily -eq 'InterNetwork') { $min = 24; $max = 32 } else { $min = 48; $max = 128 }
        if ($prefix -lt $min -or $prefix -gt $max) { return $false }
    }
    return $true
}

function Test-Add([string]$Kind, [string]$Value) {
    switch ($Kind) {
        'domain' { return ($Value -cmatch $DomainPattern -and (Test-Suffix $Value)) }
        'ip' {
            if (-not (Test-IP $Value)) { return $false }
            if ($AllowedSenderIPs.Count -gt 0) { return ($AllowedSenderIPs -contains $Value) }
            return $true
        }
        'url' {
            if ($Value -cnotmatch $UrlPattern) { return $false }
            return (Test-Suffix $Value.Substring(0, $Value.Length - 2))
        }
    }
    return $false
}

function Get-Values($Items) {
    return @($Items | Where-Object { $_ } | ForEach-Object { "$($_)" } | Where-Object { $_ })
}

# Read-Lists reads the lists, a failed read stops the run before anything is
# changed or reported
function Read-Lists {
    # Microsoft may list a rule pending deletion next to the valid one, and
    # the two can not be told apart safely here. The run stops without a
    # change until only one rule is left.
    $rules = @(Get-ExoPhishSimOverrideRule)
    if ($rules.Count -gt 1) { throw 'More than one phishing simulation rule was found, one may be pending deletion. Nothing was changed, the next run tries again.' }
    $rule = $rules | Select-Object -First 1
    $domains = @(); $ips = @()
    if ($rule) {
        # @() keeps an empty list a list, PowerShell turns it into null
        $domains = @(Get-Values $rule.Domains)
        $ips = @(Get-Values $rule.SenderIpRanges)
        # a rule always holds a domain and a sending IP, a rule read without
        # them is a failed read and must never be reported as an empty tenant
        if ($domains.Count -eq 0 -or $ips.Count -eq 0) { throw 'The phishing simulation rule was read without domains or sending IPs' }
    }
    $urls = @(Get-Values (Get-TenantAllowBlockListItems -ListType Url -ListSubType AdvancedDelivery | ForEach-Object { $_.Value }))
    return @{ rule = $rule; domains = $domains; ips = $ips; urls = $urls }
}

function Get-Keys($Lists) {
    $keys = New-Object System.Collections.Generic.HashSet[string]
    foreach ($v in $Lists.domains) { [void]$keys.Add("domain:$($v.ToLower())") }
    foreach ($v in $Lists.ips) { [void]$keys.Add("ip:$($v.ToLower())") }
    foreach ($v in $Lists.urls) { [void]$keys.Add("url:$($v.ToLower())") }
    return ,$keys
}

# a run that stops on an error still tells Phishing Club why, the error
# shows there until a run succeeds
try {
    # setup errors from here on are reported to Phishing Club too
    # the runtime environment must hold the pinned module version
    Import-Module ExchangeOnlineManagement -RequiredVersion '__MODULE_VERSION__'
    if ($Organization -cnotmatch '^[a-z0-9][a-z0-9-]{0,62}\.onmicrosoft\.com$') { throw 'Set $Organization at the top of the runbook to the .onmicrosoft.com domain of the tenant' }

    Connect-ExchangeOnline -ManagedIdentity -Organization $Organization -ShowBanner:$false

    $before = Read-Lists
    $beforeKeys = Get-Keys $before

    $plan = Invoke-RestMethod -Method Get -Uri "$($BaseUrl)/plan" -Headers $Headers -ConnectionTimeoutSeconds 30 -OperationTimeoutSeconds 60
    if (-not $plan.planId) { throw 'Phishing Club returned no plan' }
    $adds = @($plan.add | Where-Object { $_ })
    $removes = @($plan.remove | Where-Object { $_ })
    if ($adds.Count -gt $MaxChanges -or $removes.Count -gt $MaxChanges) { throw 'The plan has more changes than allowed' }

    $addDomains = @(); $addIPs = @(); $addUrls = @()
    foreach ($op in $adds) {
        $kind = "$($op.kind)"; $value = "$($op.value)"
        if (-not (Test-Add $kind $value)) { $RunErrors.Add("refused to add $($kind) $($value)"); continue }
        if ($beforeKeys.Contains("$($kind):$($value)")) { continue }
        switch ($kind) {
            'domain' { $addDomains += $value }
            'ip' { $addIPs += $value }
            'url' { $addUrls += $value }
        }
    }
    $removeDomains = @(); $removeIPs = @(); $removeUrls = @()
    foreach ($op in $removes) {
        $kind = "$($op.kind)"; $value = "$($op.value)"
        $key = "$($kind):$($value)"
        # already gone from the tenant, such as after a lost report, nothing to do
        if (-not $beforeKeys.Contains($key)) { continue }
        switch ($kind) {
            'domain' { $removeDomains += $value }
            'ip' { $removeIPs += $value }
            'url' { $removeUrls += $value }
        }
    }

    $rule = $before.rule
    $keptDomains = @($before.domains | Where-Object { $removeDomains -notcontains $_.ToLower() })
    $keptIPs = @($before.ips | Where-Object { $removeIPs -notcontains $_.ToLower() })
    # the whole rule goes only when every domain and sending IP in it was added
    # by this runbook and nothing is added
    $removeWholeRule = $rule -and $plan.removeRule -and $keptDomains.Count -eq 0 -and $keptIPs.Count -eq 0 -and $addDomains.Count -eq 0 -and $addIPs.Count -eq 0
    # otherwise the rule must keep a domain and a sending IP. When an add was
    # refused here or the tenant changed since the plan, one removal is held
    # back, an empty rule is refused by Microsoft along with every other change
    if ($rule -and -not $removeWholeRule) {
        if ($keptDomains.Count -eq 0 -and $addDomains.Count -eq 0 -and $removeDomains.Count -gt 0) {
            $held = $removeDomains[-1]
            $removeDomains = @($removeDomains | Select-Object -SkipLast 1)
            $keptDomains = @($held)
        }
        if ($keptIPs.Count -eq 0 -and $addIPs.Count -eq 0 -and $removeIPs.Count -gt 0) {
            $held = $removeIPs[-1]
            $removeIPs = @($removeIPs | Select-Object -SkipLast 1)
            $keptIPs = @($held)
        }
    }

    # keys of the adds that went through, reported so Phishing Club knows
    # which values it added
    $doneKeys = New-Object System.Collections.Generic.List[string]
    $ruleAddKeys = @($addDomains | ForEach-Object { "domain:$($_)" }) + @($addIPs | ForEach-Object { "ip:$($_)" })
    $ruleRemoved = $false
    if ($removeWholeRule) {
        try {
            Remove-ExoPhishSimOverrideRule -Identity $rule.Identity -Confirm:$false
            $ruleRemoved = $true
        } catch { $RunErrors.Add("could not remove the phishing simulation rule: $($_.Exception.Message)") }
    } elseif (-not $rule -and $addDomains.Count -gt 0 -and $addIPs.Count -gt 0) {
        try {
            $policy = Get-PhishSimOverridePolicy | Select-Object -First 1
            if (-not $policy) { $policy = New-PhishSimOverridePolicy -Name 'PhishSimOverridePolicy' }
            New-ExoPhishSimOverrideRule -Policy $policy.Identity -Domains $addDomains -SenderIpRanges $addIPs | Out-Null
            $doneKeys.AddRange([string[]]$ruleAddKeys)
        } catch {
            $RunErrors.Add("could not create the phishing simulation rule: $($_.Exception.Message)")
        }
    } elseif (-not $rule -and ($addDomains.Count -gt 0 -or $addIPs.Count -gt 0)) {
        $RunErrors.Add('the phishing simulation rule needs at least one domain and one sending IP')
    } elseif ($rule) {
        $params = @{ Identity = $rule.Identity }
        if ($addDomains.Count -gt 0) { $params.AddDomains = $addDomains }
        if ($removeDomains.Count -gt 0) { $params.RemoveDomains = $removeDomains }
        if ($addIPs.Count -gt 0) { $params.AddSenderIpRanges = $addIPs }
        if ($removeIPs.Count -gt 0) { $params.RemoveSenderIpRanges = $removeIPs }
        if ($params.Count -gt 1) {
            try {
                Set-ExoPhishSimOverrideRule @params
                if ($ruleAddKeys.Count -gt 0) { $doneKeys.AddRange([string[]]$ruleAddKeys) }
            } catch { $RunErrors.Add("could not update the phishing simulation rule: $($_.Exception.Message)") }
        }
    }
    # removes first, a full list has no room for an add before
    foreach ($url in $removeUrls) {
        try {
            Remove-TenantAllowBlockListItems -ListType Url -ListSubType AdvancedDelivery -Entries $url | Out-Null
        } catch { $RunErrors.Add("could not remove URL $($url): $($_.Exception.Message)") }
    }
    foreach ($url in $addUrls) {
        try {
            New-TenantAllowBlockListItems -Allow -ListType Url -ListSubType AdvancedDelivery -Entries $url -NoExpiration | Out-Null
            $doneKeys.Add("url:$($url)")
        } catch { $RunErrors.Add("could not add URL $($url): $($_.Exception.Message)") }
    }

    $after = Read-Lists
    # a rule this run removed can still be listed while Microsoft deletes it,
    # it is gone for the report
    if ($ruleRemoved -and $after.rule) {
        $after = @{ rule = $null; domains = @(); ips = @(); urls = $after.urls }
    }
    $afterKeys = Get-Keys $after

    # a turned off policy keeps its values but they have no effect
    try {
        $policyNow = Get-PhishSimOverridePolicy | Select-Object -First 1
        if ($policyNow -and $policyNow.Enabled -eq $false) { $RunErrors.Add('the phishing simulation policy is turned off in the tenant') }
    } catch { $RunErrors.Add("could not read the phishing simulation policy: $($_.Exception.Message)") }

    $addedNow = @($doneKeys | Where-Object { $afterKeys.Contains($_) })

    $body = @{
        planId = "$($plan.planId)"
        before = @{ domains = @($before.domains); ips = @($before.ips); urls = @($before.urls) }
        observed = @{ domains = @($after.domains); ips = @($after.ips); urls = @($after.urls) }
        errors = @($RunErrors)
        addedNow = @($addedNow)
    } | ConvertTo-Json -Depth 5
    Invoke-RestMethod -Method Post -Uri "$($BaseUrl)/report" -Headers $Headers -ContentType 'application/json' -Body $body -ConnectionTimeoutSeconds 30 -OperationTimeoutSeconds 60 | Out-Null
} catch {
    $message = "$($_.Exception.Message)"
    try {
        $failure = @{ failed = $true; errors = @(@($RunErrors) + @($message)) } | ConvertTo-Json -Depth 3
        Invoke-RestMethod -Method Post -Uri "$($BaseUrl)/report" -Headers $Headers -ContentType 'application/json' -Body $failure -ConnectionTimeoutSeconds 30 -OperationTimeoutSeconds 60 | Out-Null
    } catch { }
    throw
} finally {
    try { Disconnect-ExchangeOnline -Confirm:$false } catch { }
}
if ($RunErrors.Count -gt 0) { Write-Warning ($RunErrors -join '; ') }
`
