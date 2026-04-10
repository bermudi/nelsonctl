# Poe Content Generation Reference

Guide for generating images, videos, and audio through Poe's specialized endpoints.

---

## Overview

Poe provides specialized endpoints for content generation beyond text:

| Type | Endpoint | Models |
|------|----------|--------|
| Images | `/bot/{bot}/generate_image` | dalle-3, stable-diffusion |
| Videos | `/bot/{bot}/generate_video` | Runway, Kling |
| Audio | `/bot/{bot}/generate_audio` | TTS models |

---

## Image Generation

### Using the Responses API

```bash
curl -X POST "https://api.poe.com/bot/dalle-3" \
  -H "Poe-API-Key: $POE_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "query": "A serene mountain landscape at sunset with snow-capped peaks"
  }'
```

### With Size Options

```bash
curl -X POST "https://api.poe.com/bot/dalle-3" \
  -H "Poe-API-Key: $POE_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "query": "A futuristic city with flying cars",
    "image_size": "1024x1024",
    "image_style": "vivid"
  }'
```

### Size Options

| Size | Aspect Ratio | Best For |
|------|--------------|----------|
| `1024x1024` | 1:1 | Square images, avatars |
| `1024x1792` | 9:16 | Mobile, stories |
| `1792x1024` | 16:9 | Landscape, banners |

### Style Options

| Style | Description |
|-------|-------------|
| `vivid` | Enhanced colors, higher contrast |
| `natural` | More realistic, less stylized |

### Response Format

```json
{
  "images": [
    {
      "url": "https://example.com/generated-image.png",
      "revised_prompt": "A serene mountain landscape..."
    }
  ]
}
```

### JavaScript Implementation

```typescript
async function generateImage(prompt: string, size: string = '1024x1024') {
  const response = await fetch('https://api.poe.com/bot/dalle-3', {
    method: 'POST',
    headers: {
      'Poe-API-Key': process.env.POE_API_KEY!,
      'Content-Type': 'application/json'
    },
    body: JSON.stringify({
      query: prompt,
      image_size: size,
      image_style: 'vivid'
    })
  });

  const data = await response.json();
  return data.images[0];
}

// Usage
const image = await generateImage('A cute puppy', '1024x1024');
console.log(image.url);
```

### Python Implementation

```python
import requests
import os

def generate_image(prompt, size='1024x1024'):
    response = requests.post(
        'https://api.poe.com/bot/dalle-3',
        headers={'Poe-API-Key': os.environ['POE_API_KEY']},
        json={'query': prompt, 'image_size': size}
    )
    return response.json()['images'][0]

image = generate_image('A sunset over the ocean')
print(image['url'])
```

---

## Video Generation

### Basic Video Request

```bash
curl -X POST "https://api.poe.com/bot/video-generator" \
  -H "Poe-API-Key: $POE_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "query": "Ocean waves gently crashing on a sandy beach at sunset"
  }'
```

### Response Format

```json
{
  "videos": [
    {
      "url": "https://example.com/generated-video.mp4",
      "duration_seconds": 5,
      "status": "ready"
    }
  ]
}
```

### JavaScript Implementation

```typescript
async function generateVideo(prompt: string) {
  const response = await fetch('https://api.poe.com/bot/video-generator', {
    method: 'POST',
    headers: {
      'Poe-API-Key': process.env.POE_API_KEY!,
      'Content-Type': 'application/json'
    },
    body: JSON.stringify({ query: prompt })
  });

  const data = await response.json();
  
  // Videos may be generated async
  if (data.status === 'processing') {
    console.log('Video is being generated...');
    // Poll for completion or use webhook
    return pollForVideo(data.job_id);
  }
  
  return data.videos[0];
}
```

### Polling for Video Completion

```typescript
async function pollForVideo(jobId: string, maxAttempts = 30) {
  for (let i = 0; i < maxAttempts; i++) {
    const response = await fetch(`https://api.poe.com/bot/video-generator/${jobId}`, {
      headers: { 'Poe-API-Key': process.env.POE_API_KEY! }
    });
    
    const data = await response.json();
    
    if (data.status === 'ready') {
      return data.videos[0];
    }
    
    await new Promise(r => setTimeout(r, 2000)); // Wait 2s
  }
  throw new Error('Video generation timed out');
}
```

---

## Audio Generation (Text-to-Speech)

### Basic TTS Request

```bash
curl -X POST "https://api.poe.com/bot/audio-tts" \
  -H "Poe-API-Key: $POE_API_KEY" \
  -H "Content-Type: application/json" \
  -d '{
    "query": "Hello! Welcome to our application.",
    "voice": "alloy"
  }'
```

### Voice Options

| Voice | Style | Gender |
|-------|-------|--------|
| `alloy` | Neutral | Neutral |
| `echo` | Friendly | Male |
| `fable` | British | Male |
| `onyx` | Deep | Male |
| `nova` | Warm | Female |
| `shimmer` | Soft | Female |

### Response Format

```json
{
  "audio": {
    "url": "https://example.com/audio.mp3",
    "duration_seconds": 3.5,
    "format": "mp3"
  }
}
```

### JavaScript Implementation

```typescript
interface TTSOptions {
  text: string;
  voice?: 'alloy' | 'echo' | 'fable' | 'onyx' | 'nova' | 'shimmer';
  speed?: number; // 0.5 - 2.0
}

async function textToSpeech({ text, voice = 'alloy', speed = 1.0 }: TTSOptions) {
  const response = await fetch('https://api.poe.com/bot/audio-tts', {
    method: 'POST',
    headers: {
      'Poe-API-Key': process.env.POE_API_KEY!,
      'Content-Type': 'application/json'
    },
    body: JSON.stringify({
      query: text,
      voice,
      speed
    })
  });

  const data = await response.json();
  return data.audio;
}

// Usage
const audio = await textToSpeech({
  text: 'Your order has been confirmed!',
  voice: 'nova',
  speed: 1.1
});
```

---

## Content Generation via MCP Tools

Poe's MCP server exposes content generation as tools:

### Image Tool

```typescript
// MCP tool call
{
  tool: 'generate_image',
  arguments: {
    prompt: 'A cozy coffee shop interior',
    size: '1024x1024',
    style: 'vivid'
  }
}
```

### Video Tool

```typescript
{
  tool: 'generate_video',
  arguments: {
    prompt: 'Rain falling on a window'
  }
}
```

### Audio Tool

```typescript
{
  tool: 'generate_audio',
  arguments: {
    text: 'Welcome to our store!',
    voice: 'nova'
  }
}
```

---

## Batch Generation

### Multiple Images

```typescript
async function generateMultiple(prompts: string[]) {
  const results = await Promise.all(
    prompts.map(prompt => generateImage(prompt))
  );
  return results;
}

// Generate variations
const variations = await generateMultiple([
  'A red sports car',
  'A blue sports car',
  'A green sports car'
]);
```

### Style Variations

```typescript
async function generateVariations(prompt: string) {
  const styles = ['vivid', 'natural'] as const;
  const sizes = ['1024x1024', '1792x1024'] as const;
  
  const tasks = [];
  
  for (const style of styles) {
    for (const size of sizes) {
      tasks.push(generateImage(prompt, size, style));
    }
  }
  
  return Promise.all(tasks);
}
```

---

## Error Handling

### Common Errors

| Error | Cause | Solution |
|-------|-------|----------|
| `invalid_prompt` | Prompt too long or contains blocked content | Shorten prompt, remove sensitive content |
| `model_unavailable` | Generator model down | Retry or use alternative |
| `rate_limited` | Too many requests | Implement backoff |
| `content_policy` | Violates content guidelines | Modify prompt |

### Retry Logic

```typescript
async function generateWithRetry(
  type: 'image' | 'video' | 'audio',
  params: any,
  maxRetries = 3
) {
  for (let i = 0; i < maxRetries; i++) {
    try {
      switch (type) {
        case 'image':
          return await generateImage(params.prompt, params.size);
        case 'video':
          return await generateVideo(params.prompt);
        case 'audio':
          return await textToSpeech(params);
      }
    } catch (error) {
      if (error.status === 429 || error.status >= 500) {
        await sleep(Math.pow(2, i) * 1000);
        continue;
      }
      throw error;
    }
  }
}
```

---

## Best Practices

### Image Generation

1. **Be specific but concise** - "A golden retriever playing fetch" beats vague prompts
2. **Mention style explicitly** - "photorealistic", "watercolor", "3D render"
3. **Specify composition** - "portrait", "landscape", "close-up"
4. **Avoid negative prompts** - Say what you want, not what you don't want

### Video Generation

1. **Keep prompts short** - Complex scenes don't translate well
2. **Focus on motion** - Videos need action: "waves crashing" not just "ocean"
3. **Be patient** - Video generation takes time (30-60s typical)

### Audio Generation

1. **Match voice to content** - Professional tone with `alloy`, friendly with `nova`
2. **Adjust speed for clarity** - Slightly slower (1.0-1.1) for important messages
3. **Break long text** - Process paragraphs separately for better quality

---

## Cost Considerations

Content generation typically costs more compute points than text:

| Type | Relative Cost |
|------|---------------|
| Text (100 tokens) | 1x |
| Image (1024x1024) | ~10x |
| Video (5 seconds) | ~50x |
| Audio (30 seconds) | ~2x |

Monitor usage with:
```bash
poe-code usage list --filter dalle
poe-code usage list --filter video
```
