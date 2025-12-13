# OAuth Token Import

This guide explains how to import existing OAuth access and refresh tokens into PhishingClub.

## Overview

You may want to import existing OAuth tokens if you:
- Already have valid tokens from a previous OAuth authorization
- Want to migrate tokens from another system
- Have tokens generated through a different OAuth flow
- Need to use tokens that were authorized outside of PhishingClub

## Requirements

Before importing tokens, you need:
1. Valid access and refresh tokens from an OAuth provider
2. The token expiration timestamp (in milliseconds)
3. The client ID associated with the tokens
4. The token URL for refreshing tokens

## Import Format

Tokens must be provided as a JSON array. Each token object should have the following fields:

```json
[
  {
    "access_token": "eyJ0eXAiOiJKV1QiLCJub25jZSI6...",
    "refresh_token": "1.AXkAwC9YcwqenkWrp4TriW...",
    "client_id": "1fec8e78-bce4-4aaf-ab1b-5451cc387264",
    "expires_at": 1765657989704,
    "name": "user@example.com (Microsoft Teams)",
    "user": "user@example.com",
    "scope": "https://graph.microsoft.com/.default offline_access",
    "token_url": "https://login.microsoftonline.com/73582fc0-9e0a-459e-aba7-84eb896f9a3f/oauth2/v2.0/token",
    "created_at": 1765634409156
  }
]
```

### Field Descriptions

- **access_token** (required): The OAuth access token (typically a JWT)
- **refresh_token** (required): The OAuth refresh token used to get new access tokens
- **client_id** (required): The OAuth application's client ID
- **expires_at** (required): Unix timestamp in milliseconds when the access token expires
- **name** (required): A display name for this token (e.g., "user@example.com (Microsoft Teams)")
- **user** (required): The email or username of the authorized account
- **scope** (required): Space-separated list of OAuth scopes
- **token_url** (optional): The token endpoint URL. Defaults to Microsoft's token endpoint if not provided
- **created_at** (optional): Unix timestamp in milliseconds when the token was created

## Importing Tokens via UI

1. Navigate to the **OAuth** page in PhishingClub
2. Click the **Import Authorized Token** button
3. Paste your JSON array into the text field
4. Click **Submit**

The system will validate and import all tokens in the array. Each token becomes a separate OAuth provider entry.

## Importing Tokens via API

You can also import tokens programmatically using the API:

```bash
POST /api/v1/oauth-provider/import-tokens
Content-Type: application/json

[
  {
    "access_token": "eyJ0eXAiOiJKV1QiLCJub25jZSI6...",
    "refresh_token": "1.AXkAwC9YcwqenkWrp4TriW...",
    "client_id": "1fec8e78-bce4-4aaf-ab1b-5451cc387264",
    "expires_at": 1765657989704,
    "name": "user@example.com (Microsoft Teams)",
    "user": "user@example.com",
    "scope": "https://graph.microsoft.com/.default offline_access"
  }
]
```

Response:
```json
{
  "success": true,
  "data": {
    "ids": ["550e8400-e29b-41d4-a716-446655440000"],
    "count": 1
  }
}
```

## Exporting Tokens

You can export tokens from PhishingClub to the same format used for importing:

1. Navigate to the **OAuth** page
2. Find the provider you want to export
3. Click the **ellipsis menu (⋮)** on the provider row
4. Select **Export Token**

The token will be downloaded as a JSON file that can be imported elsewhere.

### Export via API

```bash
GET /api/v1/oauth-provider/{id}/export-tokens
```

## Imported Provider Restrictions

OAuth providers created via import have some restrictions compared to regular providers:

1. **Cannot be authorized or re-authorized** - They use pre-authorized tokens and cannot go through the OAuth flow
2. **Limited editing** - Only the name field can be edited. Client credentials and URLs cannot be changed
3. **No copying** - Imported providers cannot be copied
4. **Token refresh only** - They can only refresh their tokens using the refresh token, not obtain new ones via authorization

## Microsoft 365 / Teams Example

If you have Microsoft Teams or Office 365 OAuth tokens, they typically look like this:

```json
[
  {
    "access_token": "eyJ0eXAiOiJKV1QiLCJub25jZSI6ImVCUV9XLUV5d2gtSlpqbXFuWDlUTGF2ZkNIR09kV2U4RFBJM1k5a3FibGciLCJhbGciOiJSUzI1NiIsIng1dCI6InJ0c0ZULWItN0x1WTdEVlllU05LY0lKN1ZuYyIsImtpZCI6InJ0c0ZULWItN0x1WTdEVlllU05LY0lKN1ZuYyJ9...",
    "refresh_token": "1.AXkAwC9YcwqenkWrp4TriW-aP3iO7B_kvK9KqxtUUcw4cmR5AKd5AA.BQABAwEAAAADAOz_BQD0_zaTKAHXsCeupQqQPHKcMtLP8K45KxFiwFgAFG5-NCiSMh30e3jgmHbuuFBYk8qWOaMLqmrh_jfA5biRz6pm7w6zmcD4HbpDCUeQ2eZ0UHAl4aeZbp5FYlSrBgozbdLkgDnSXjKOSeHTVprpbOpe94rzqapKwLvUNjHPQvSiRYyOlh94chh-DTEWSYGUK1EA0bShHa51ZfLZOIeLkeDzqieuSt7b4eqBVvkTLArtAHceN0V9rbLTfMAg18usGY6vdZEwOWwAjayuT-xZPSKdTuqeN6CZ1BHKBj7fhmy48jiXyQolgAL6eXMUnjPC_FsUplJn3guYYXmzTh3B6PHJn6EQjuKWGR9Iaaw4TGB0qmwfJJBfsFNv25EKUR8ragrHE-tTvp9fDuRzTMgW9-_kmngdz95W_ob1RaxN6gsLpZ1O1y75dJkFnoQGLy05YdTAQzo9fdddwHR2NMbe7ovXw440hPqb_ValzBY1ovsMSrr2QSFM1VZDdkWKDyj9JBgzd3XVTWLgcUXpxnou_bM2ZHs2EiRQx9FiFCscTAHg7iFjcMIzA1tFUTunKYtcHN3m6-XGiPUQp2g3Zwu8fEnYo_dG10Ci3uw2PkxmsrVHAT3btlY9QnyGzgUQzfR_Cg9mEmXr476NQE6_NqlBPjZRho2klvolBgAthXtyZPKM1vhL-ei7AcMico9_06DBh-g1uavfQ9LtBLG_RCXfKqU2bTsN0KFp4AhTd49jvGLVPw_bFcQ1DzZHLYiQLv5lZqO6ATZiKJHY9FO3Twpnj7dKxqaxytXlBQ3lmxB5MxjRJumK8lvtlF21-MXn5HYymliU1okLsgHIs-lN-NOVvEMQ2tpt-pJ_XEp8l6baMzjcdILaJ9HkiNPhGoWZ3qkv7k_DKpybkZLX6On9KamfaBP-CKcopBbqvHomuZUItm_6MAdMJBuMFypszop4v_rXq1PC3XYlbDcfwiyHoQen79CgX6xap_31UqeYCaTvazQwFlho-Y3CheTfbDRjRJqfxSPmQDZkZugwRlIICDRlId8GogPqH4a_DCv5N3pbmu-lTNJ0YibCYbEanQxyot_UTQGWCRulUIQ28o8Y5wMn9UoBAzD9opT4g1XW3WnAogU4mdOwO40hhXMraW4Cjd5JQOvj-HUmxlwCHP-PiyhHn9w_WDlqdkigJerw3t12L8wZwC77yipee8JGbZFiIoNYW462wtpkUBkqSfsI2D5O-MgeriY5kYHbCz5e-I5g4pwRj54_mwQUkc61OHkf0rXyiVhH8zsmrCvkWov6gPmf9259_QEZ",
    "client_id": "1fec8e78-bce4-4aaf-ab1b-5451cc387264",
    "expires_at": 1765657989704,
    "name": "ronni@365.skansing.dk (Microsoft Teams)",
    "user": "ronni@365.skansing.dk",
    "scope": "https://graph.microsoft.com/.default offline_access"
  }
]
```

### Microsoft OAuth Endpoints

For Microsoft 365/Teams OAuth:

**Authorization URL:**
```
https://login.microsoftonline.com/{tenant-id}/oauth2/v2.0/authorize
```

**Token URL:**
```
https://login.microsoftonline.com/{tenant-id}/oauth2/v2.0/token
```

Replace `{tenant-id}` with your Azure AD tenant ID.

## Common Issues

### Invalid Token Format
Ensure your JSON is properly formatted and all required fields are present.

### Expired Tokens
The refresh token is invalid or has expired. You need to perform a new OAuth authorization flow.

### Token Refresh Failures
If token refresh fails, check that:
- The token URL is correct
- The client ID matches the one used to obtain the tokens
- The refresh token hasn't been revoked

## Security Best Practices

1. **Secure Storage**: Imported tokens are stored encrypted in the database, just like regular OAuth tokens.
2. **Access Control**: Only users with global permissions can import and export tokens.
3. **Token Rotation**: Tokens will be automatically refreshed when they expire using the refresh token.
4. **Audit Trail**: All import and export operations are logged in the audit log.

## Troubleshooting

If you encounter issues importing tokens:

1. Verify the JSON format is correct (use a JSON validator)
2. Check that all required fields are present
3. Ensure the expires_at timestamp is in milliseconds, not seconds
4. Verify the token_url is accessible and correct
5. Check the application logs for detailed error messages