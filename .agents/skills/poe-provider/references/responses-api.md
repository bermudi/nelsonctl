# Poe Responses API Reference

The Responses API is Poe's primary interface for AI text generation with support for tools, streaming, and multi-turn conversations.

---

## Endpoint

```
POST https://api.poe.com/bot/{bot_name}
```

### Headers

| Header | Required | Description |
|--------|----------|-------------|
| `Poe-API-Key` | Yes | Your Poe API key |
| `Content-Type` | Yes | Must be `application/json` |

---

## Request Format

### Minimal Request

```json
{
  "query": "Your question or prompt here"
}
```

### Full Request Options

```json
{
  "query": "Explain machine learning",
  "temperature": 0.7,
  "max_output_tokens": 2048,
  "stop_sequences": ["\n\n", "END"],
  "stream": false,
  "system_instruction": "You are a helpful tutor"
}
```

---

## Parameters

| Parameter | Type | Default | Description |
|-----------|------|---------|-------------|
| `query` | string | Required | User message |
| `temperature` | float | 1.0 | Creativity (0.0-2.0) |
| `max_output_tokens` | int | Model default | Max response length |
| `stop_sequences` | array | null | Stop generation at these |
| `stream` | boolean | false | Enable streaming |
| `system_instruction` | string | null | System prompt override |

### Temperature Guide

| Value | Use Case |
|-------|----------|
| 0.0 - 0.3 | Precise, factual responses |
| 0.4 - 0.7 | Balanced (default) |
| 0.8 - 1.0 | Creative, varied outputs |
| 1.0+ | Highly random (use sparingly) |

---

## Multi-Turn Conversations

### With Messages Array

```json
{
  "messages": [
    {"role": "user", "content": "What is Python?"},
    {"role": "assistant", "content": "Python is a programming language..."},
    {"role": "user", "content": "What can I build with it?"}
  ]
}
```

### Roles

| Role | Description |
|------|-------------|
| `user` | Human input |
| `assistant` | AI response |
| `system` | System-level instructions |

### Message Content Formats

**Text only:**
```json
{"role": "user", "content": "Hello"}
```

**Multimodal (with images):**
```json
{
  "role": "user",
  "content": [
    {"type": "text", "text": "What's in this image?"},
    {"type": "image_url", "image_url": {"url": "https://example.com/image.png"}}
  ]
}
```

---

## Streaming Responses

### Request

Set `stream: true` in your request:

```bash
curl -X POST "https://api.poe.com/bot/claude-3-5-sonnet" \
  -H "Poe-API-Key: $POE_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"query": "Write a poem", "stream": true}'
```

### Response Format

Server-Sent Events (SSE):

```
data: {"text": "Roses"}

data: {"text": " are"}

data: {"text": " red"}

data: [DONE]
```

### Client Implementation

```typescript
async function* streamResponse(prompt: string) {
  const response = await fetch('https://api.poe.com/bot/claude-3-5-sonnet', {
    method: 'POST',
    headers: {
      'Poe-API-Key': process.env.POE_API_KEY!,
      'Content-Type': 'application/json'
    },
    body: JSON.stringify({ query: prompt, stream: true })
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

// Usage
for await (const token of streamResponse('Write a story')) {
  process.stdout.write(token.text);
}
```

---

## Tools (Function Calling)

### Defining Tools

```json
{
  "query": "What's the weather in Paris?",
  "tools": [
    {
      "type": "function",
      "function": {
        "name": "get_weather",
        "description": "Get current weather for a location",
        "parameters": {
          "type": "object",
          "properties": {
            "location": {
              "type": "string",
              "description": "City name"
            },
            "unit": {
              "type": "string",
              "enum": ["celsius", "fahrenheit"],
              "default": "celsius"
            }
          },
          "required": ["location"]
        }
      }
    }
  ]
}
```

### Tool Response Format

When the model requests a tool:

```json
{
  "tool_calls": [
    {
      "id": "call_abc123",
      "name": "get_weather",
      "arguments": {"location": "Paris", "unit": "celsius"}
    }
  ]
}
```

### Continuing After Tools

```json
{
  "tool_calls": [
    {"id": "call_abc123", "name": "get_weather", "arguments": {...}}
  ],
  "tool_results": [
    {
      "tool_call_id": "call_abc123",
      "output": "{\"temperature\": 22, \"conditions\": \"Sunny\"}"
    }
  ]
}
```

---

## Response Format

### Non-Streaming

```json
{
  "text": "Generated response text...",
  "model": "claude-3-5-sonnet",
  "usage": {
    "output_tokens": 150,
    "input_tokens": 50
  }
}
```

### Usage Tracking

| Field | Description |
|-------|-------------|
| `input_tokens` | Tokens in your prompt |
| `output_tokens` | Tokens in generated response |

**Note**: Poe bills in compute points, not raw tokens. Usage shown is for reference.

---

## Examples

### Basic Question

```bash
curl -X POST "https://api.poe.com/bot/claude-3-5-sonnet" \
  -H "Poe-API-Key: $POE_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{"query": "What is the capital of Japan?"}'
```

### Code Generation

```bash
curl -X POST "https://api.poe.com/bot/claude-3-5-sonnet" \
  -H "Poe-API-Key: $POE_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "query": "Write a Python function to calculate fibonacci",
    "temperature": 0.2
  }'
```

### Conversation

```bash
curl -X POST "https://api.poe.com/bot/claude-3-5-sonnet" \
  -H "Poe-API-Key: $POE_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "messages": [
      {"role": "user", "content": "I need to sort a list in Python"},
      {"role": "assistant", "content": "You can use the sorted() function..."},
      {"role": "user", "content": "What about descending order?"}
    ]
  }'
```

---

## Rate Limits

| Tier | Requests/minute |
|------|-----------------|
| Free | 10 |
| Basic | 60 |
| Pro | 240 |

Implement exponential backoff on 429 responses:

```typescript
async function requestWithBackoff(params: RequestParams) {
  for (let attempt = 0; attempt < 5; attempt++) {
    const response = await fetch(url, params);
    
    if (response.ok) return response;
    if (response.status !== 429) throw new Error(`HTTP ${response.status}`);
    
    const delay = Math.pow(2, attempt) * 1000;
    await new Promise(r => setTimeout(r, delay));
  }
  throw new Error('Max retries exceeded');
}
```
