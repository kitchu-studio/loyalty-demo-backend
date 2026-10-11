# Adds demo activity for one customer: earns points from three bills and redeems one reward.
# Usage: .\scripts\seed-demo.ps1 -Email you@example.com -Password yourpassword
param(
  [Parameter(Mandatory = $true)][string]$Email,
  [Parameter(Mandatory = $true)][string]$Password,
  [string]$Api = "http://localhost:8081/api/v1",
  [string]$AdminEmail = "owner@example.com",
  [string]$AdminPassword = "demo12345"
)
$ErrorActionPreference = "Stop"

function Call($method, $path, $token, $body) {
  $headers = @{}
  if ($token) { $headers["Authorization"] = "Bearer $token" }
  $args = @{ Method = $method; Uri = "$Api$path"; Headers = $headers; ContentType = "application/json" }
  if ($body) { $args["Body"] = ($body | ConvertTo-Json) }
  Invoke-RestMethod @args
}

try {
  $admin = Call POST "/auth/register" $null @{ email = $AdminEmail; password = $AdminPassword; name = "Owner" }
} catch {
  $admin = Call POST "/auth/login" $null @{ email = $AdminEmail; password = $AdminPassword }
}
$customer = Call POST "/auth/login" $null @{ email = $Email; password = $Password }

foreach ($baht in 400, 300, 150) {
  $code = Call POST "/admin/earn-codes" $admin.access_token @{ amount_baht = $baht }
  $res = Call POST "/points/claim" $customer.access_token @{ code = $code.code }
  Write-Host "+$($res.points) points, balance $($res.balance)"
}

$rewards = Call GET "/rewards" $null $null
$cheapest = $rewards | Sort-Object cost_points | Select-Object -First 1
$redeemed = Call POST "/rewards/$($cheapest.id)/redeem" $customer.access_token $null
Write-Host "Redeemed $($cheapest.name), voucher $($redeemed.redemption.code)"
