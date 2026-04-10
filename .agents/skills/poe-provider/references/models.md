# Poe Model Catalog

Complete reference for models available through Poe, sourced live from the API.

---

## Listing Models

### API Endpoint

```
GET https://api.poe.com/v1/models
```

No auth required. Returns all publicly available models.

```bash
# Fetch and filter
curl -s https://api.poe.com/v1/models | jq '.data[].id' -r

# Filter by provider
curl -s https://api.poe.com/v1/models | jq '.data[] | select(.owned_by=="Anthropic") | .id' -r

# Filter by input modality
curl -s https://api.poe.com/v1/models | jq '.data[] | select(.architecture.input_modalities | contains(["image"])) | .id' -r
```

### Response Format

```json
{
  "object": "list",
  "data": [
    {
      "id": "claude-sonnet-4.5",
      "object": "model",
      "created": 1712505600000,
      "owned_by": "Anthropic",
      "architecture": {
        "input_modalities": ["text", "image"],
        "output_modalities": ["text"]
      },
      "pricing": {
        "prompt": "0.0000026",
        "completion": "0.000013",
        "input_cache_read": "0.00000026",
        "input_cache_write": "0.0000032"
      }
    }
  ]
}
```

Pricing is per-token in USD. When `pricing` is `null`, the model is free or request-based (see `request` field for per-request pricing).

---

## Text Models by Provider

### Anthropic

| Model | Input | Price In/Out (per 1M tokens) | Notes |
|-------|-------|------------------------------|-------|
| `claude-opus-4.6` | text, image | $4.30 / $21.00 | Latest flagship |
| `claude-opus-4.5` | text, image | $4.30 / $21.00 | |
| `claude-sonnet-4.6` | text, image | $2.60 / $13.00 | Latest sonnet |
| `claude-sonnet-4.5` | text, image | $2.60 / $13.00 | |
| `claude-haiku-4.5` | text, image | $0.85 / $4.30 | Latest haiku |
| `claude-haiku-3` | text, image | $0.21 / $1.10 | Cheapest |

### OpenAI

| Model | Input | Price In/Out (per 1M) | Notes |
|-------|-------|-----------------------|-------|
| `gpt-5.4` | text, image | $2.20 / $14.00 | Latest |
| `gpt-5.4-mini` | text, image | $0.68 / $4.00 | |
| `gpt-5.4-nano` | text, image | $0.18 / $1.10 | |
| `gpt-5.3-codex` | text, image | $1.60 / $13.00 | Code-optimized |
| `gpt-5.3-codex-spark` | text | Free | |
| `gpt-5.3-instant` | text, image | $1.60 / $13.00 | |
| `gpt-5.2` | text, image | $1.60 / $13.00 | |
| `gpt-5.2-codex` | text, image | $1.60 / $13.00 | Code-optimized |
| `gpt-5.2-instant` | text, image | $1.60 / $13.00 | |
| `gpt-5.1-codex-max` | text, image | $1.10 / $9.00 | Max code |
| `gpt-5.1-codex` | text, image | $1.10 / $9.00 | Code-optimized |
| `gpt-5.1` | text, image | $1.10 / $9.00 | |
| `gpt-5.1-instant` | text, image | $1.10 / $9.00 | |
| `gpt-5.1-codex-mini` | text | $0.22 / $1.80 | Code-optimized |
| `gpt-5` | text, image | $1.10 / $9.00 | |
| `gpt-5-chat` | text, image | $1.10 / $9.00 | |
| `gpt-5-codex` | text, image | $1.10 / $9.00 | Code-optimized |
| `gpt-5-mini` | text, image | $0.22 / $1.80 | |
| `gpt-5-nano` | text, image | $0.045 / $0.36 | |

Anything older that what is listed is not recommended.

### Google

| Model | Input | Price In/Out (per 1M) | Notes |
|-------|-------|-----------------------|-------|
| `gemini-3.1-pro` | text, image, video, audio | $2.00 / $12.00 | Latest pro |
| `gemini-3.1-flash-lite` | text, image, video, audio | $0.25 / $1.50 | |
| `gemini-3-flash` | text, image, video, audio | $0.40 / $2.40 | |
| `gemma-4-31b` | text, image | Free | Open weights |

### Novita AI (35 models)

**DeepSeek (via Novita):**

| Model | Input | Price | Notes |
|-------|-------|-------|-------|
| `deepseek-v3.2` | text | $0.27 / $0.40 per 1M | Latest |

**Kimi (via Novita):**

| Model | Input | Price In/Out (per 1M) | Notes |
|-------|-------|-----------------------|-------|
| `kimi-k2.5` | text, image, video | $0.60 / $3.00 | Latest, multimodal |

**MiniMax (via Novita):**

| Model | Input | Price In/Out (per 1M) | Notes |
|-------|-------|-----------------------|-------|
| `minimax-m2.7` | text | $0.30 / $1.20 | Latest |
| `minimax-m2.5` | text | $0.30 / $1.20 | |

**GLM (via Novita):**

| Model | Input | Price In/Out (per 1M) | Notes |
|-------|-------|-----------------------|-------|
| `glm-5` | text | $1.00 / $3.20 | Latest |
| `glm-4.7-n` | text | $0.60 / $2.20 | |
| `glm-4.7-flash` | text | $0.07 / $0.40 | Cheapest |
| `glm-4.6` | text | — | |
| `glm-4.6v` | text, image | — | Vision |
| `glm-4.5` | text | — | |

### Fireworks AI (12 models)

| Model | Input | Price | Notes |
|-------|-------|-------|-------|
| `kimi-k2.5-fw` | text, image | Free | |
| `glm-5.1-fw` | text | $1.40 / $4.40 per 1M | |
| `deepseek-v3.2-fw` | text, image | $18.00/req | |
| `openai-gpt-oss-120b` | text, image | — | |
| `openai-gpt-oss-20b` | text, image | — | |

### Together AI (16 models)

| Model | Input | Price | Notes |
|-------|-------|-------|-------|
| `kimi-k2.5-tog` | text | $11.00/req | |
| `glm-5-t` | text | Free | |
| `minimax-m2.5-t` | text | $3.90/req | |

---

## Cross-Provider Model Index

The same model may be available through multiple providers, sometimes at different prices or with different capabilities.

### Kimi K2.5

| Provider | Model ID | Input | Price |
|----------|----------|-------|-------|
| Novita AI | `kimi-k2.5` | text, image, video | $0.60 / $3.00 per 1M |
| Fireworks AI | `kimi-k2.5-fw` | text, image | Free |
| Together AI | `kimi-k2.5-tog` | text | $11.00/req |

### MiniMax M2.5 & M2.7

| Provider | Model ID | Input | Price |
|----------|----------|-------|-------|
| **M2.7** | | | |
| Novita AI | `minimax-m2.7` | text | $0.30 / $1.20 per 1M |
| **M2.5** | | | |
| Novita AI | `minimax-m2.5` | text | $0.30 / $1.20 per 1M |
| Together AI | `minimax-m2.5-t` | text | $3.90/req |

### GLM-5 & GLM-5.1

| Provider | Model ID | Input | Price |
|----------|----------|-------|-------|
| **GLM-5** | | | |
| Novita AI | `glm-5` | text | $1.00 / $3.20 per 1M |
| Together AI | `glm-5-t` | text | Free |
| **GLM-5.1** | | | |
| Fireworks AI | `glm-5.1-fw` | text | $1.40 / $4.40 per 1M |

---

## Model Selection Guide

### By Task

| Task | Recommended Models |
|------|-------------------|
| **Coding** | claude-sonnet-4.6, glm-5.1 |
| **Fast coding** | minimax-2.5, minimax-2.7, kimi-2.5 |
| **Complex reasoning/coding** | claude-opus-4.6, gpt-5.4 |
| **Long documents** | gemini-3.0-flash |
| **Multimodal (vision)** | gemini-3.1-pro, kimi-k2.5 (cheaper) |
| **Cheap/fast** | gpt-5.4-nano, gemini-3.1-flash-lite |

---

## Best Practices

1. **Check pricing type** — some models use per-token pricing, others per-request. Per-request models show `pricing.prompt: null` with `pricing.request` set.

2. **Use specific providers for the same model** — e.g., `glm-5-t` (Together, free) vs `glm-5` (Novita, $1/$3.20 per 1M).

3. **Cache the model list** — call `GET /v1/models` once per session/day. The list changes infrequently.

4. **Filter by modality** — use `architecture.input_modalities` to find vision-capable models, etc.

5. **Pin versions for production** — model availability and pricing can change.
