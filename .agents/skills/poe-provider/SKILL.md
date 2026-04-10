---
name: poe-provider
description: "Complete reference for integrating with Poe as an AI model provider. Use when: (1) Configuring Poe as a provider for coding agents (Claude Code, Codex, OpenCode, Kimi), (2) Setting up Poe API authentication with API keys or OAuth, (3) Querying AI models via the Responses API or Chat Completions API, (4) Generating images, videos, or audio through Poe, (5) Managing API usage and compute points billing, (6) Configuring Poe's MCP server for model access, (7) Users mention Poe subscriptions, model access, or wanting to use Poe with their favorite AI tools. Triggers especially when users say 'use Poe', 'Poe API', 'poe-code', 'configure AI provider', or need to access models through Poe."
---

# Poe Provider Integration

Poe provides unified access to hundreds of AI models from multiple providers through a single API. This skill covers authentication, API usage, content generation, and best practices for integrating with Poe.

---

## ⚠️ Critical Poe Gotchas (Read First!)

**These are the mistakes agents make repeatedly without explicit guidance:**

| Gotcha | Wrong | Correct |
|--------|-------|---------|
| **Env var name** | `OPENAI_API_KEY` | `POE_API_KEY` |
| **API key prefix** | `sk-...` or `your-key` | `poe-xxxxx-...` |
| **Base URL** | `api.openai.com` | `api.poe.com/v1` |
| **Model names** | `gpt-4`, `claude-3` | `claude-3-5-sonnet`, `gpt-4o` |
| **Billing** | Tokens | Compute points |
| **Auth header** | Varies by endpoint | See below |

---

## Quick Setup (3 Steps)

### 1. Get Your API Key

```bash
# Visit https://poe.com/api while logged in
# Click "Create API Key"
# Copy the key (it starts with poe-)
```

### 2. Set Environment Variable

```bash
export POE_API_KEY=poe-xxxxx-your-key-here
```

### 3. Verify It Works

```bash
poe-code auth status
```

---

## Switching from OpenAI SDK

**This is the fastest way to use Poe with existing code:**

```typescript
// ❌ Wrong - OpenAI
import OpenAI from 'openai';
const client = new OpenAI({ apiKey: process.env.OPENAI_API_KEY });

// ✅ Correct - Poe (just add baseURL!)
import OpenAI from 'openai';
const client = new OpenAI({ 
  baseURL: 'https://api.poe.com/v1',
  apiKey: process.env.POE_API_KEY 
});
```

**Python:**
```python
# Same pattern - just add base_url
from openai import OpenAI
client = OpenAI(
    base_url="https://api.poe.com/v1",
    api_key=os.environ.get("POE_API_KEY")
)
```

### Model Name Mapping

| OpenAI | Poe Equivalent |
|--------|---------------|
| `gpt-4o` | `claude-3-5-sonnet` or `gpt-4o` |
| `gpt-4-turbo` | `claude-3-5-sonnet` |
| `gpt-3.5-turbo` | `claude-3-haiku` |
| `o1-preview` | `o1-preview` |

---

## Poe Code CLI (Essential Tool)

Poe provides a powerful CLI for managing Poe integration:

### Core Commands

```bash
# Install
npm install -g poe-code

# Check balance
poe-code usage

# View usage history
poe-code usage list --pages 5

# List available models
poe-code models
poe-code models --search claude

# Wrap Claude Code with Poe
poe-code wrap claude

# One-off prompt
poe-code spawn claude "Fix the bug in auth.ts"

# Spawn against GitHub repo
poe-code spawn codex "Review auth module" --cwd github://owner/repo
```

### MCP Server Setup

```bash
# Configure Claude Code to use Poe MCP
npx poe-code@latest mcp configure claude-code

# Available MCP tools:
# - generate_text  (query any Poe bot)
# - generate_image
# - generate_video
# - generate_audio
```

---

## API Endpoints

### Responses API (Recommended)

```bash
curl -X POST "https://api.poe.com/bot/claude-3-5-sonnet" \
  -H "Poe-API-Key: $POE_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"query": "Your question"}'
```

### Chat Completions API (OpenAI-compatible)

```bash
curl -X POST "https://api.poe.com/v1/chat/completions" \
  -H "Authorization: Bearer $POE_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "claude-3-5-sonnet",
    "messages": [{"role": "user", "content": "Hello"}]
  }'
```

### Auth Header by Endpoint

| Endpoint | Header |
|----------|--------|
| `/bot/*` | `Poe-API-Key: $POE_API_KEY` |
| `/v1/*` | `Authorization: Bearer $POE_API_KEY` |

---

## Authentication

### API Key (Simplest)

```bash
# Environment variable
export POE_API_KEY=poe-xxxxx

# Or use poe-code login (opens browser)
npx poe-code@latest login
```

### OAuth PKCE (For User-Facing Apps)

```typescript
import { poeOAuth } from 'poe-oauth';

// 1. Create auth URL
const { authorizationUrl } = await poeOAuth.createAuthCode({
  callbackUrl: 'https://yourapp.com/callback'
});
res.redirect(authorizationUrl);

// 2. Exchange code for key
const { key } = await poeOAuth.exchangeCode({ code: req.query.code });
```

---

## Usage & Billing

**Poe uses compute points**, not tokens. Monitor usage:

```bash
# Check balance
poe-code usage

# View history
poe-code usage list --pages 5

# Filter by model
poe-code usage list --filter claude
```

### Point Costs (Relative)

| Type | Relative Cost |
|------|---------------|
| Text (100 tokens) | 1x |
| Image (1024x1024) | ~10x |
| Video (5 sec) | ~50x |

---

## Error Handling

### Quick Reference

| Status | Meaning | Fix |
|--------|---------|-----|
| **401** | Invalid API key | Check `POE_API_KEY`, regenerate key |
| **403** | No permission | Upgrade subscription |
| **404** | Model not found | Use `poe-code models` to verify name |
| **429** | Rate limited | Wait, implement backoff |

### 401 vs 403

- **401 Unauthorized**: Your API key is wrong or expired
- **403 Forbidden**: Your subscription doesn't cover this

### Retry Pattern

```typescript
async function retryWithBackoff(fn: () => Promise<Response>, maxRetries = 5) {
  for (let i = 0; i < maxRetries; i++) {
    const response = await fn();
    if (response.ok) return response;
    if (response.status === 429 || response.status >= 500) {
      await sleep(Math.pow(2, i) * 1000);
      continue;
    }
    throw new Error(`HTTP ${response.status}`);
  }
}
```

---

## Content Generation

### Image Generation

```bash
curl -X POST "https://api.poe.com/bot/dalle-3" \
  -H "Poe-API-Key: $POE_API_KEY" \
  -d '{"query": "A sunset over mountains", "image_size": "1024x1024"}'
```

**Sizes:** `1024x1024` (square), `1792x1024` (landscape), `1024x1792` (portrait)  
**Styles:** `vivid` (enhanced), `natural` (realistic)

### Video Generation

```bash
curl -X POST "https://api.poe.com/bot/video-generator" \
  -H "Poe-API-Key: $POE_API_KEY" \
  -d '{"query": "Ocean waves crashing"}'
```

### Audio (TTS)

```bash
curl -X POST "https://api.poe.com/bot/audio-tts" \
  -H "Poe-API-Key: $POE_API_KEY" \
  -d '{"query": "Hello!", "voice": "alloy"}'
```

**Voices:** `alloy`, `echo`, `fable`, `onyx`, `nova`, `shimmer`

---

## Gotchas (Additional)

- **OAuth requires subscription**: Users need Poe Pro subscription for API access
- **Rate limits vary by tier**: Check `poe-code usage` for your limits
- **Streaming errors**: Wrap in try/catch, handle incomplete streams
- **Model availability**: Not all models available via API (some web-only)

---

## Reference Files

| File | When to Read |
|------|--------------|
| `references/authentication.md` | OAuth flow, credential storage |
| `references/responses-api.md` | Full Responses API reference, tools |
| `references/chat-api.md` | Chat Completions details |
| `references/models.md` | Full model catalog (if needed) |
| `references/content-gen.md` | Image/video/audio details |
| `references/errors.md` | Error codes, debugging |
