# Poe API Error Reference

Complete guide to Poe API error codes, troubleshooting, and debugging strategies.

---

## HTTP Status Codes

### Client Errors (4xx)

| Code | Status | Meaning | Common Causes |
|------|--------|---------|---------------|
| 400 | Bad Request | Invalid request format | Malformed JSON, missing fields |
| 401 | Unauthorized | Authentication failed | Invalid API key, expired token |
| 403 | Forbidden | Access denied | Subscription tier, permissions |
| 404 | Not Found | Resource doesn't exist | Wrong endpoint, model name |
| 409 | Conflict | Request conflict | Concurrent modification |
| 422 | Unprocessable | Valid format but invalid content | Invalid parameter values |
| 429 | Too Many Requests | Rate limit exceeded | Too many API calls |

### Server Errors (5xx)

| Code | Status | Meaning | Common Causes |
|------|--------|---------|---------------|
| 500 | Internal Server Error | Poe server issue | Poe infrastructure |
| 502 | Bad Gateway | Upstream service down | Provider temporarily unavailable |
| 503 | Service Unavailable | Poe is down | Planned maintenance, overload |
| 504 | Gateway Timeout | Upstream timeout | Slow response from model provider |

---

## Error Response Format

### Standard Error

```json
{
  "error": {
    "type": "invalid_request_error",
    "message": "The 'model' field is required",
    "code": "MISSING_MODEL",
    "status": 400
  }
}
```

### Error Fields

| Field | Type | Description |
|-------|------|-------------|
| `type` | string | Error category |
| `message` | string | Human-readable description |
| `code` | string | Machine-readable error code |
| `status` | number | HTTP status code |
| `details` | object | Additional context (varies) |

---

## Common Errors and Solutions

### 401 Unauthorized

**Problem**: API key is invalid, expired, or missing.

**Symptoms**:
```json
{
  "error": {
    "type": "authentication_error",
    "message": "Invalid API key provided",
    "code": "INVALID_KEY"
  }
}
```

**Solutions**:

1. **Check environment variable**:
```bash
echo $POE_API_KEY  # Should show your key
```

2. **Verify key format**:
```
Valid: poe-xxxxxAbCdEfGhIjKlMnOpQrStUvWxYz
Invalid: your-key-here (missing poe- prefix)
```

3. **Regenerate key** if compromised:
   - Visit [poe.com/api](https://poe.com/api)
   - Delete old key
   - Create new key
   - Update environment

4. **Check key permissions**:
   - Some keys may be read-only
   - Subscription required for certain models

---

### 403 Forbidden

**Problem**: Valid key but insufficient permissions.

**Symptoms**:
```json
{
  "error": {
    "type": "permission_error",
    "message": "Your subscription does not include access to this model",
    "code": "SUBSCRIPTION_REQUIRED"
  }
}
```

**Solutions**:

1. **Upgrade subscription**:
   - Poe Pro subscription for full access
   - Check [poe.com/settings](https://poe.com/settings)

2. **Use eligible models**:
   - Not all models available on all tiers
   - Check model availability

3. **Check OAuth scopes**:
   - If using OAuth, ensure requested scopes include access

---

### 404 Not Found

**Problem**: Endpoint or model doesn't exist.

**Symptoms**:
```json
{
  "error": {
    "type": "not_found_error",
    "message": "Model 'gpt-5' not found",
    "code": "MODEL_NOT_FOUND"
  }
}
```

**Solutions**:

1. **Use correct model name**:
```bash
poe-code models --search <your-model>
```

2. **Check endpoint URL**:
```bash
# Wrong
https://api.poe.com/bot/gpt-5

# Correct
https://api.poe.com/bot/gpt-4o
```

3. **Refresh model cache**:
```bash
poe-code models --refresh
```

---

### 422 Unprocessable Entity

**Problem**: Request format is valid but content is invalid.

**Symptoms**:
```json
{
  "error": {
    "type": "invalid_request_error",
    "message": "temperature must be between 0 and 2",
    "code": "INVALID_PARAMETER"
  }
}
```

**Solutions**:

1. **Validate parameters**:
```typescript
const temperature = Math.max(0, Math.min(2, userTemperature));
```

2. **Check required fields**:
```typescript
if (!messages || messages.length === 0) {
  throw new Error('messages array is required');
}
```

3. **Sanitize input**:
```typescript
const sanitized = input.replace(/[^\x00-\x7F]/g, ''); // ASCII only
```

---

### 429 Rate Limited

**Problem**: Too many requests in short time.

**Symptoms**:
```json
{
  "error": {
    "type": "rate_limit_error",
    "message": "Rate limit exceeded. Please wait before retrying.",
    "code": "RATE_LIMITED",
    "retry_after": 30
  }
}
```

**Solutions**:

1. **Implement exponential backoff**:
```typescript
async function requestWithBackoff(fn: () => Promise<Response>) {
  for (let attempt = 0; attempt < 5; attempt++) {
    const response = await fn();
    
    if (response.status !== 429) return response;
    
    const retryAfter = response.headers.get('Retry-After') || Math.pow(2, attempt);
    await sleep(retryAfter * 1000);
  }
  throw new Error('Max retries exceeded');
}
```

2. **Add request queuing**:
```typescript
class RequestQueue {
  private queue: (() => Promise<any>)[] = [];
  private processing = 0;
  private readonly maxConcurrent = 5;
  private readonly requestsPerMinute = 60;

  async add<T>(fn: () => Promise<T>): Promise<T> {
    return new Promise((resolve, reject) => {
      this.queue.push(async () => {
        try {
          resolve(await fn());
        } catch (e) {
          reject(e);
        }
      });
      this.processQueue();
    });
  }

  private async processQueue() {
    while (this.queue.length > 0 && this.processing < this.maxConcurrent) {
      this.processing++;
      const fn = this.queue.shift()!;
      await fn();
      this.processing--;
      await sleep(1000 / this.requestsPerMinute);
    }
  }
}
```

3. **Use streaming instead of multiple requests** - One streaming request costs less than 5 separate requests

---

### 500 Internal Server Error

**Problem**: Poe server-side issue.

**Symptoms**:
```json
{
  "error": {
    "type": "server_error",
    "message": "An unexpected error occurred",
    "code": "INTERNAL_ERROR"
  }
}
```

**Solutions**:

1. **Retry with backoff** - Server errors are usually temporary
2. **Check Poe status** - [status.poe.com](https://status.poe.com)
3. **Try alternative model** - Switch to a different provider
4. **Contact support** if persistent - Include request IDs

---

## Error Handling Patterns

### TypeScript Implementation

```typescript
interface PoeError {
  type: string;
  message: string;
  code: string;
  status: number;
}

class PoeAPIError extends Error {
  constructor(public apiError: PoeError) {
    super(apiError.message);
    this.name = 'PoeAPIError';
  }

  isAuthError() {
    return this.apiError.status === 401;
  }

  isRateLimited() {
    return this.apiError.status === 429;
  }

  isRetryable() {
    return [429, 500, 502, 503, 504].includes(this.apiError.status);
  }
}

async function handlePoeError(error: unknown): Promise<void> {
  if (error instanceof PoeAPIError) {
    switch (error.apiError.status) {
      case 401:
        console.error('Invalid API key');
        // Prompt for new key or redirect to login
        break;
      case 403:
        console.error('Subscription required');
        // Prompt to upgrade
        break;
      case 429:
        console.error('Rate limited, retrying...');
        // Already handled by backoff
        break;
      default:
        console.error(`API Error: ${error.message}`);
    }
  } else {
    console.error('Unexpected error:', error);
  }
}
```

### Python Implementation

```python
import time
from typing import Optional

class PoeAPIError(Exception):
    def __init__(self, error_dict: dict):
        self.type = error_dict.get('type')
        self.message = error_dict.get('message')
        self.code = error_dict.get('code')
        self.status = error_dict.get('status')
        super().__init__(self.message)

    def is_retryable(self) -> bool:
        return self.status in [429, 500, 502, 503, 504]

def make_request_with_retry(session, url, data, max_retries=5):
    for attempt in range(max_retries):
        response = session.post(url, json=data)
        
        if response.ok:
            return response.json()
        
        error = response.json().get('error', {})
        
        if response.status_code == 429:
            retry_after = int(response.headers.get('Retry-After', 2 ** attempt))
            time.sleep(retry_after)
            continue
        
        if response.status_code >= 500:
            time.sleep(2 ** attempt)
            continue
        
        raise PoeAPIError(error)
    
    raise PoeAPIError({'message': 'Max retries exceeded', 'status': 500})
```

---

## Debugging Tips

### Enable Request Logging

```typescript
const DEBUG = process.env.DEBUG === 'true';

async function apiCall(endpoint: string, body: any) {
  if (DEBUG) {
    console.log('→ Request:', { endpoint, body });
  }
  
  const response = await fetch(endpoint, {
    method: 'POST',
    headers: {
      'Poe-API-Key': process.env.POE_API_KEY!,
      'Content-Type': 'application/json'
    },
    body: JSON.stringify(body)
  });
  
  const data = await response.json();
  
  if (DEBUG) {
    console.log('← Response:', { status: response.status, data });
  }
  
  return data;
}
```

### Check Request IDs

Every response includes a request ID for support:

```json
{
  "id": "req_abc123xyz",
  "...": "other fields"
}
```

### Common Mistakes

| Mistake | Fix |
|---------|-----|
| Using `OPENAI_API_KEY` | Use `POE_API_KEY` |
| Wrong base URL | Use `https://api.poe.com` |
| Missing `Content-Type` | Always set to `application/json` |
| Invalid model name | Run `poe-code models` to verify |
| No streaming on long outputs | Use streaming for responses > 500 tokens |

---

## Support Resources

- **Status Page**: [status.poe.com](https://status.poe.com)
- **Documentation**: [poe.com/api](https://poe.com/api)
- **CLI Help**: `poe-code --help`
- **Discord**: [discord.gg/joinpoe](https://discord.gg/joinpoe)

When contacting support, include:
1. Request ID from the error response
2. Timestamp of the request
3. Model you were trying to use
4. Full request and response (with API key redacted)
