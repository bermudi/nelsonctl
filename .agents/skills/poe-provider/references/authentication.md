# Poe Authentication Reference

Detailed guide for authenticating with the Poe API.

---

## API Key Authentication

### Getting Your API Key

1. Log into [poe.com](https://poe.com)
2. Navigate to [poe.com/api](https://poe.com/api)
3. Click "Create API Key"
4. Name your key (e.g., "Development", "Production")
5. Copy and store securely

**Important**: API keys are shown only once. Store immediately.

### Environment Setup

#### Bash/Zsh
```bash
export POE_API_KEY=poe-xxxxx-your-key-here
```

Add to `~/.bashrc` or `~/.zshrc` for persistence:
```bash
echo 'export POE_API_KEY="poe-xxxxx"' >> ~/.bashrc
```

#### Node.js
```typescript
// process.env.POE_API_KEY is automatically read
const apiKey = process.env.POE_API_KEY;
```

#### Python
```python
import os
api_key = os.environ.get('POE_API_KEY')
```

#### Docker
```dockerfile
ENV POE_API_KEY=poe-xxxxx
```

### Verifying Authentication

```bash
# Using poe-code CLI
poe-code auth status

# Direct API check
curl -X GET "https://api.poe.com/bot/YourBot" \
  -H "Poe-API-Key: $POE_API_KEY"
```

---

## OAuth PKCE Flow

For user-facing applications where users authenticate with their own Poe accounts.

### Overview

OAuth PKCE (Proof Key for Code Exchange) allows secure authentication without handling passwords:

1. Generate a code verifier and challenge
2. Redirect user to Poe authorization
3. Exchange authorization code for access token
4. Use token for API calls

### Implementation

#### 1. Create Authorization URL

```typescript
import crypto from 'crypto';

function generateCodeVerifier(): string {
  return crypto.randomBytes(32).toString('base64url');
}

function generateCodeChallenge(verifier: string): string {
  return crypto
    .createHash('sha256')
    .update(verifier)
    .digest('base64url');
}

const codeVerifier = generateCodeVerifier();
const codeChallenge = generateCodeChallenge(codeVerifier);

const authUrl = new URL('https://poe.com/oauth/authorize');
authUrl.searchParams.set('response_type', 'code');
authUrl.searchParams.set('client_id', 'YOUR_CLIENT_ID');
authUrl.searchParams.set('redirect_uri', 'https://yourapp.com/callback');
authUrl.searchParams.set('code_challenge', codeChallenge);
authUrl.searchParams.set('code_challenge_method', 'S256');
authUrl.searchParams.set('scope', 'read');

// Store verifier for step 3
session.codeVerifier = codeVerifier;

// Redirect user
res.redirect(authUrl.toString());
```

#### 2. Exchange Code for Token

```typescript
const response = await fetch('https://api.poe.com/oauth/token', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({
    grant_type: 'authorization_code',
    client_id: 'YOUR_CLIENT_ID',
    code: req.query.code,
    redirect_uri: 'https://yourapp.com/callback',
    code_verifier: session.codeVerifier
  })
});

const { access_token, refresh_token, expires_in } = await response.json();
```

#### 3. Refresh Token

```typescript
const response = await fetch('https://api.poe.com/oauth/token', {
  method: 'POST',
  headers: { 'Content-Type': 'application/json' },
  body: JSON.stringify({
    grant_type: 'refresh_token',
    client_id: 'YOUR_CLIENT_ID',
    refresh_token: storedRefreshToken
  })
});
```

---

## Credential Storage

### poe-code Credentials

The `poe-code` CLI stores credentials in:

```
~/.poe-code/
├── credentials.json    # API keys (encrypted)
├── config.json         # User preferences
└── cache/              # Model list cache
```

### Manual Secure Storage

For custom applications:

#### macOS Keychain
```bash
security add-generic-password -a "poe" -s "POE_API_KEY" -w "your-key"
security find-generic-password -a "poe" -s "POE_API_KEY" -w
```

#### Linux (secret-tool)
```bash
secret-tool store --label="POE_API_KEY" poe api_key "your-key"
secret-tool lookup poe api_key
```

#### 1Password CLI
```bash
op item get "Poe API" --fields key=api_key
```

---

## Multi-Account Management

### Switching Between Accounts

```bash
# Log out current account
poe-code logout

# Log in with new account
poe-code login
```

### Environment-Based Keys

```bash
# Development
export POE_API_KEY=poe-dev-key

# Production (different key)
export POE_API_KEY=poe-prod-key
```

### API Key Rotation

```typescript
async function rotateApiKey(oldKey: string) {
  // 1. Create new key via API
  const newKey = await createPoeApiKey({ name: 'Rotated Key' });
  
  // 2. Update environment
  process.env.POE_API_KEY = newKey;
  
  // 3. Verify new key works
  await verifyKey(newKey);
  
  // 4. Delete old key
  await deletePoeApiKey(oldKey);
}
```

---

## Troubleshooting Auth Issues

### "Invalid API Key" (401)

1. Check key hasn't expired or been deleted
2. Verify no typos in the key
3. Ensure correct environment variable name (`POE_API_KEY`)
4. Try regenerating the key

### "Access Denied" (403)

1. Check subscription status - some tiers have limited access
2. Verify key has required permissions/scopes
3. Contact Poe support if issue persists

### OAuth Flow Fails

1. **Invalid state**: Use cryptographic state parameter to prevent CSRF
2. **Expired code**: Authorization codes expire quickly; exchange immediately
3. **Redirect mismatch**: `redirect_uri` must match exactly

### Token Expiration

Refresh tokens periodically:
```typescript
// Check expiration and refresh if needed
if (Date.now() > tokenExpiresAt) {
  const newToken = await refreshToken(refreshToken);
  storeToken(newToken);
}
```

---

## Security Checklist

- [ ] API key stored in environment variable, not in code
- [ ] Keys committed to `.gitignore` (if stored in files)
- [ ] Different keys for dev/staging/production
- [ ] OAuth PKCE used for user-facing apps
- [ ] Refresh tokens stored securely (encrypted DB, keychain)
- [ ] Regular audit of active API keys
- [ ] Keys rotated when team members leave
- [ ] Rate limiting implemented to prevent abuse
