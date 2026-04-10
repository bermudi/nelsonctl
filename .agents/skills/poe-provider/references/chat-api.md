# Poe Chat Completions API Reference

OpenAI-compatible Chat Completions endpoint for drop-in replacement with existing code.

---

## Endpoint

```
POST https://api.poe.com/v1/chat/completions
```

**Note**: This is similar to OpenAI's Chat Completions API but routes through Poe's infrastructure.

---

## Authentication

Use Bearer token authentication:

```bash
curl -X POST "https://api.poe.com/v1/chat/completions" \
  -H "Authorization: Bearer $POE_API_KEY" \
  -H "Content-Type: application/json"
```

---

## Request Format

### Minimal Request

```json
{
  "model": "claude-3-5-sonnet",
  "messages": [
    {"role": "user", "content": "Hello"}
  ]
}
```

### Full Request

```json
{
  "model": "claude-3-5-sonnet",
  "messages": [
    {"role": "system", "content": "You are a helpful assistant"},
    {"role": "user", "content": "Explain quantum computing"}
  ],
  "temperature": 0.7,
  "max_tokens": 1000,
  "top_p": 1.0,
  "frequency_penalty": 0.0,
  "presence_penalty": 0.0,
  "stream": false,
  "stop": null
}
```

---

## Parameters

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `model` | string | Required | Model identifier |
| `messages` | array | Required | Conversation messages |
| `temperature` | float | 1.0 | Sampling temperature |
| `max_tokens` | int | Model default | Max response tokens |
| `top_p` | float | 1.0 | Nucleus sampling |
| `frequency_penalty` | float | 0.0 | Penalize repeated tokens |
| `presence_penalty` | float | 0.0 | Penalize repeated topics |
| `stream` | boolean | false | Enable streaming |
| `stop` | array | null | Stop sequences |
| `tools` | array | null | Available functions |

### Temperature vs Top-P

Use **either** `temperature` **or** `top_p`, not both. Using both can lead to unexpected behavior.

- `temperature`: Direct sampling control (0.0 = deterministic, 2.0 = very random)
- `top_p`: Nucleus sampling (0.1 = top 10% probability mass)

---

## Message Format

### Standard Roles

```json
[
  {"role": "system", "content": "You are a helpful assistant"},
  {"role": "user", "content": "What is 2+2?"},
  {"role": "assistant", "content": "2+2 equals 4."},
  {"role": "user", "content": "Thanks!"}
]
```

### Roles Reference

| Role | Description |
|------|-------------|
| `system` | System instructions (precedes user) |
| `user` | Human messages |
| `assistant` | AI responses |
| `developer` | Developer messages (model-specific) |

### Multimodal Content

```json
{
  "role": "user",
  "content": [
    {"type": "text", "text": "What is in this image?"},
    {
      "type": "image_url",
      "image_url": {
        "url": "https://example.com/photo.jpg",
        "detail": "auto"
      }
    }
  ]
}
```

**Image detail options:**
- `auto`: Model decides
- `low`: Lower resolution (faster, cheaper)
- `high`: High resolution (slower, more expensive)

---

## Response Format

### Non-Streaming

```json
{
  "id": "chatcmpl-123",
  "object": "chat.completion",
  "created": 1677652288,
  "model": "claude-3-5-sonnet",
  "choices": [
    {
      "index": 0,
      "message": {
        "role": "assistant",
        "content": "Response text..."
      },
      "finish_reason": "stop"
    }
  ],
  "usage": {
    "prompt_tokens": 50,
    "completion_tokens": 150,
    "total_tokens": 200
  }
}
```

### Finish Reasons

| Reason | Meaning |
|--------|---------|
| `stop` | Natural stopping point |
| `length` | Hit `max_tokens` limit |
| `content_filter` | Content filtered |
| `function_call` | Model requested function |
| `tool_calls` | Tool execution needed |

---

## Streaming

### Request

```bash
curl -X POST "https://api.poe.com/v1/chat/completions" \
  -H "Authorization: Bearer $POE_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "model": "claude-3-5-sonnet",
    "messages": [{"role": "user", "content": "Count to 5"}],
    "stream": true
  }'
```

### Streaming Response

Server-Sent Events format:

```
data: {"id":"chatcmpl-123","object":"chat.completion.chunk","created":123,"model":"claude-3-5-sonnet","choices":[{"index":0,"delta":{"content":"Hello"},"finish_reason":null}]}

data: {"id":"chatcmpl-123","object":"chat.completion.chunk","created":123,"model":"claude-3-5-sonnet","choices":[{"index":0,"delta":{"content":" world"},"finish_reason":null}]}

data: [DONE]
```

### Client Implementation

```typescript
async function* streamChat(messages: any[], model: string) {
  const response = await fetch('https://api.poe.com/v1/chat/completions', {
    method: 'POST',
    headers: {
      'Authorization': `Bearer ${process.env.POE_API_KEY}`,
      'Content-Type': 'application/json'
    },
    body: JSON.stringify({ model, messages, stream: true })
  });

  const reader = response.body?.getReader();
  const decoder = new TextDecoder();

  while (reader) {
    const { done, value } = await reader.read();
    if (done) break;

    const chunk = decoder.decode(value);
    const lines = chunk.split('\n');

    for (const line of lines) {
      if (line.startsWith('data: ')) {
        const data = line.slice(6);
        if (data === '[DONE]') return;
        try {
          yield JSON.parse(data);
        } catch {}
      }
    }
  }
}
```

---

## Function/Tool Calling

### With Functions

```json
{
  "model": "claude-3-5-sonnet",
  "messages": [
    {"role": "user", "content": "What's the weather in San Francisco?"}
  ],
  "tools": [
    {
      "type": "function",
      "function": {
        "name": "get_weather",
        "description": "Get weather for a location",
        "parameters": {
          "type": "object",
          "properties": {
            "location": {"type": "string"},
            "unit": {"type": "string", "enum": ["celsius", "fahrenheit"]}
          },
          "required": ["location"]
        }
      }
    }
  ]
}
```

### Response with Function Call

```json
{
  "choices": [{
    "message": {
      "role": "assistant",
      "content": null,
      "tool_calls": [{
        "index": 0,
        "id": "call_abc123",
        "type": "function",
        "function": {
          "name": "get_weather",
          "arguments": "{\"location\": \"San Francisco\"}"
        }
      }]
    },
    "finish_reason": "tool_calls"
  }]
}
```

### Continuing with Tool Results

```json
{
  "model": "claude-3-5-sonnet",
  "messages": [
    {"role": "user", "content": "What's the weather in San Francisco?"},
    {"role": "assistant", "tool_calls": [{"id": "call_abc123", "function": {"name": "get_weather", "arguments": "{\"location\": \"San Francisco\"}"}}]},
    {"role": "tool", "tool_call_id": "call_abc123", "content": "{\"temperature\": 22, \"conditions\": \"Sunny\"}"}
  ]
}
```

---

## OpenAI SDK Compatibility

### Node.js

```typescript
import OpenAI from 'openai';

const client = new OpenAI({
  baseURL: 'https://api.poe.com/v1',
  apiKey: process.env.POE_API_KEY
});

// Use exactly like OpenAI
const chat = await client.chat.completions.create({
  model: 'claude-3-5-sonnet',
  messages: [{ role: 'user', content: 'Hello' }]
});
```

### Python

```python
from openai import OpenAI

client = OpenAI(
    base_url="https://api.poe.com/v1",
    api_key=os.environ.get("POE_API_KEY")
)

response = client.chat.completions.create(
    model="claude-3-5-sonnet",
    messages=[{"role": "user", "content": "Hello"}]
)
```

### LangChain

```typescript
import { ChatOpenAI } from '@langchain/openai';

const llm = new ChatOpenAI({
  model: 'claude-3-5-sonnet',
  openAIApiKey: process.env.POE_API_KEY,
  configuration: {
    basePath: 'https://api.poe.com/v1'
  }
});
```

---

## Model Mapping

| OpenAI Model | Poe Equivalent |
|--------------|---------------|
| `gpt-4` | `claude-3-5-sonnet` or `claude-3-opus` |
| `gpt-3.5-turbo` | `claude-3-haiku` or `claude-3-sonnet` |
| `gpt-4-turbo` | `claude-3-5-sonnet` |
| `gpt-4o` | `claude-3-5-sonnet` |

**Note**: Model names on Poe may differ. Use `poe-code models` to find exact identifiers.

---

## Error Handling

### Common Errors

| Status | Error | Solution |
|--------|-------|----------|
| 400 | Bad request | Check JSON format |
| 401 | Invalid key | Verify `POE_API_KEY` |
| 403 | Forbidden | Check subscription tier |
| 404 | Model not found | Use correct model name |
| 429 | Rate limited | Implement backoff |
| 500 | Server error | Retry later |

### Retry Implementation

```typescript
async function chatWithRetry(messages: any[], model: string, maxRetries = 3) {
  for (let i = 0; i < maxRetries; i++) {
    try {
      const response = await fetch('https://api.poe.com/v1/chat/completions', {
        method: 'POST',
        headers: {
          'Authorization': `Bearer ${process.env.POE_API_KEY}`,
          'Content-Type': 'application/json'
        },
        body: JSON.stringify({ model, messages })
      });

      if (!response.ok) {
        if (response.status === 429 || response.status >= 500) {
          await sleep(Math.pow(2, i) * 1000);
          continue;
        }
        throw new Error(`HTTP ${response.status}`);
      }

      return response.json();
    } catch (error) {
      if (i === maxRetries - 1) throw error;
      await sleep(Math.pow(2, i) * 1000);
    }
  }
}
```
