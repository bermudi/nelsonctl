# Agent Control Protocol (ACP) Specification

**Version:** 2.0
**Status:** Draft
**Date:** 2026-03-28
**Schema:** `acp-v2.json`

---

## 1. Abstract

The Agent Control Protocol (ACP) is an open, bidirectional, WebSocket-based
protocol that enables AI agents to operate existing application user interfaces
through structured manifests and commands. Rather than relying on fragile
techniques such as computer vision, DOM scraping, or robotic process automation,
ACP provides a well-defined contract between the application (which declares its
UI structure) and the agent (which decides how to operate it). The protocol is
platform-agnostic and works with any UI framework -- web, mobile, or desktop.

---

## 2. Status of This Document

This is a **draft specification** for ACP version 1.1. It is published for
review and early implementation feedback. Comments and suggestions are welcome
via GitHub issues on the protocol repository.

This document is intended to become the definitive reference for implementors
of ACP engines and SDKs.

---

## 3. Table of Contents

1. [Abstract](#1-abstract)
2. [Status of This Document](#2-status-of-this-document)
3. [Table of Contents](#3-table-of-contents)
4. [Introduction](#4-introduction)
5. [Terminology](#5-terminology)
6. [Conventions and Keywords](#6-conventions-and-keywords)
7. [Transport](#7-transport)
8. [Connection Lifecycle](#8-connection-lifecycle)
9. [Message Reference](#9-message-reference)
    - 9.1 [Client Messages](#91-client-messages)
    - 9.2 [Server Messages](#92-server-messages)
10. [Manifest Structure](#10-manifest-structure)
11. [Field Types](#11-field-types)
12. [UI Actions](#12-ui-actions)
13. [Command Execution Model](#13-command-execution-model)
14. [Streaming](#14-streaming)
15. [Error Handling](#15-error-handling)
16. [Security Considerations](#16-security-considerations)
17. [Versioning](#17-versioning)
18. [Extensions](#18-extensions)
19. [Conformance](#19-conformance)
- [Appendix A: Complete Message Type Summary](#appendix-a-complete-message-type-summary)
- [Appendix B: JSON Schema](#appendix-b-json-schema)

---

## 4. Introduction

### 4.1 Problem Statement

AI agents increasingly need to interact with application user interfaces on
behalf of users. Existing approaches to this problem have significant
limitations:

- **Computer vision** (screenshot analysis) is slow, expensive, and brittle in
  the face of UI changes, theme variations, or resolution differences.
- **DOM scraping** is tightly coupled to specific web frameworks and breaks when
  markup changes.
- **Robotic process automation (RPA)** depends on pixel coordinates, element
  selectors, or accessibility trees that vary across platforms and versions.

All of these approaches attempt to reverse-engineer the application's user
interface. They treat the UI as an opaque surface and try to derive meaning from
its visual or structural representation.

### 4.2 Solution

ACP inverts this relationship. Instead of the agent guessing what the UI
contains, the application **declares** its interface to the agent using a
structured manifest. The manifest describes screens, fields, actions, and modals
in a semantic, machine-readable format. The agent then issues structured commands
to operate the UI, and the application reports the results.

This approach provides the agent with certainty about what exists in the
interface, eliminates guesswork, and creates a stable contract that does not
break when visual styling or layout changes.

### 4.3 Design Principles

- **Declarative.** Applications describe what exists in their UI. Agents decide
  what to do based on that description and the user's intent.
- **Bidirectional.** Both sides communicate through typed, structured messages
  over a persistent connection.
- **Platform-agnostic.** The protocol operates at the semantic level and is not
  tied to any UI framework. It works on web, mobile, desktop, or any platform
  capable of WebSocket communication.
- **Semantic.** Agents operate on named fields and labeled actions, not on
  pixels, DOM nodes, or accessibility identifiers.
- **Extensible.** The core specification can be extended with additional message
  types and modalities (such as voice interaction) without breaking existing
  implementations.

---

## 5. Terminology

The following terms are used throughout this specification:

- **Engine:** The server-side component that implements the ACP protocol. The
  engine receives manifests, processes user messages using an AI model, and
  issues commands to operate the application's UI. Also referred to as the
  "server" in transport-level discussions.

- **SDK:** The client-side library that implements the ACP protocol within an
  application. The SDK sends manifests describing the UI, executes commands
  received from the engine, and reports results. Also referred to as the
  "client" in transport-level discussions.

- **Manifest:** A structured JSON message sent by the SDK that describes the
  application's user interface, including its screens, fields, actions, modals,
  and metadata.

- **Screen:** A distinct view or page within the application, described by a
  `ScreenDescriptor` in the manifest. Each screen has a unique identifier and
  contains fields and actions.

- **Field:** An interactive input element within a screen, described by a
  `FieldDescriptor`. Fields have a type (e.g., `text`, `select`, `date`), a
  unique identifier, and a human-readable label.

- **Action:** A triggerable operation within a screen, described by an
  `ActionDescriptor`. Actions represent buttons, submit operations, or other
  user-initiated behaviors.

- **Modal:** A dialog overlay within a screen, described by a
  `ModalDescriptor`. Modals have a unique identifier and may support search
  functionality.

- **Command:** A server message containing an ordered array of UI actions for
  the SDK to execute. Each command carries a sequence number for correlation.

- **Sequence ID (seq):** An integer that uniquely identifies a command within a
  session. The SDK uses the same sequence ID when reporting results or
  confirmations for that command.

---

## 6. Conventions and Keywords

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT", "SHOULD",
"SHOULD NOT", "RECOMMENDED", "MAY", and "OPTIONAL" in this document are to be
interpreted as described in [RFC 2119](https://www.ietf.org/rfc/rfc2119.txt).

---

## 7. Transport

### 7.1 Protocol

ACP operates over WebSocket as defined in
[RFC 6455](https://www.ietf.org/rfc/rfc6455.txt).

### 7.2 Endpoint

The default WebSocket endpoint path is `/connect`. Implementations MAY use a
different path, but MUST document the chosen endpoint.

### 7.3 Message Format

All messages are JSON-encoded UTF-8 text frames. Binary frames MUST NOT be used
for protocol messages. Each WebSocket text frame contains exactly one JSON
object.

### 7.4 Connection Parameters

Authentication and connection parameters are implementation-defined.
Implementations MAY use query string parameters, HTTP headers, or cookies to
pass authentication tokens (such as JWT or API keys) during the WebSocket
handshake. The protocol itself does not mandate a specific authentication
mechanism.

### 7.5 Reconnection

SDKs SHOULD implement automatic reconnection with exponential backoff when the
WebSocket connection is lost unexpectedly. The RECOMMENDED initial delay is
1 second, with a maximum delay of 30 seconds. After reconnecting, the SDK
SHOULD re-send the manifest message to re-establish the session.

### 7.6 Keep-Alive

Implementations SHOULD use WebSocket ping/pong frames (as defined in RFC 6455,
Section 5.5.2 and 5.5.3) to detect broken connections. The RECOMMENDED ping
interval is 30 seconds.

---

## 8. Connection Lifecycle

The following sequence describes the standard connection lifecycle:

```
Client (SDK)                            Server (Engine)
    |                                        |
    |--- WebSocket connect ----------------->|
    |<------------- config (sessionId, ...) -|  (1)
    |--- manifest (app UI description) ----->|  (2)
    |<---------------------- status (idle) --|  (3)
    |<------------ chat (greeting message) --|  (4)
    |                                        |
    |    ... interactive session ...          |
    |                                        |
    |--- text (user message) --------------->|  (5)
    |<----------------- status (thinking) ---|  (6)
    |<-------- chat (delta / final) ----------|  (7)
    |<------------- command (UI actions) ----|  (8)
    |--- result (execution results) -------->|  (9)
    |                                        |
    |--- WebSocket close ------------------->|  (10)
    |                                        |
```

**Rules:**

1. The engine MUST send a `config` message as the first message after the
   WebSocket connection is established. This message provides the session
   identifier and any feature flags or provider information.

2. The SDK MUST send a `manifest` message as the first message after receiving
   `config`. The manifest describes the application's complete UI structure.

3. The engine SHOULD send a `status` message with `status: "idle"` after it
   has finished processing the manifest, to signal that it is ready to accept
   user input.

4. The engine MAY send a greeting `chat` message after processing the manifest.

5. Either side MAY close the WebSocket connection at any time. The SDK SHOULD
   handle server-initiated closures gracefully and attempt reconnection as
   described in Section 7.5.

### 8.1 Protocol State Machine

The following state machine defines the valid states and transitions for an ACP
connection. Implementations MUST enforce these transitions; messages received in
an invalid state SHOULD result in an `error` message with code `invalid_message`.

```
                       WebSocket open
                            │
                            ▼
                    ┌───────────────┐
                    │  CONNECTED    │
                    │ (await config)│
                    └──────┬────────┘
                    config │
                            ▼
                    ┌───────────────┐
                    │  CONFIGURING  │
                    │(await manifest│
                    └──────┬────────┘
                  manifest │
                            ▼
              ┌─────────────────────────┐
              │                         │
              │         IDLE            │◄──────────────────┐
              │  (ready for user input) │                   │
              │                         │                   │
              └────────┬────────────────┘                   │
                       │ text message                       │
                       ▼                                    │
              ┌─────────────────┐                           │
              │                 │  chat (delta/final)        │
              │    THINKING     │──────────────────►(stream) │
              │ (LLM processing)│                           │
              │                 │                           │
              └────────┬────────┘                           │
                       │ command                            │
                       ▼                                    │
              ┌─────────────────┐                           │
              │                 │  result                   │
              │   EXECUTING     │───────► THINKING ─────────┘
              │(await SDK result│         (may loop)
              │                 │
              └─────────────────┘

  Any state ──── WebSocket close / error ────► DISCONNECTED
```

**State descriptions:**

| State | Entry condition | Valid client messages | Valid server messages |
|-------|----------------|----------------------|----------------------|
| CONNECTED | WebSocket opened | *(none — await config)* | `config` |
| CONFIGURING | `config` received | `manifest` | `error` |
| IDLE | Manifest processed or agent loop complete | `text`, `state`, `llm_config`, `response_lang_config` | `chat`, `status`, `error` |
| THINKING | User text sent or result received | *(none — await agent)* | `chat`, `command`, `status`, `error` |
| EXECUTING | `command` sent to SDK | `result`, `confirm` | `error` |
| DISCONNECTED | WebSocket closed | *(none)* | *(none)* |

**Transition rules:**

1. The engine MUST NOT send `command` messages while in IDLE state.
2. The SDK MUST NOT send `text` messages while in THINKING or EXECUTING state.
   If received, the engine SHOULD respond with an `error` (code: `invalid_message`).
3. The THINKING → EXECUTING → THINKING loop MAY repeat up to the engine's
   configured maximum rounds (RECOMMENDED: 5).
4. After the final agent loop round, the engine MUST transition to IDLE.

---

## 9. Message Reference

Every ACP message is a JSON object containing at minimum a `type` field that
identifies the message type. Messages are categorized by direction: client
messages (SDK to Engine) and server messages (Engine to SDK).

### 9.1 Client Messages

#### 9.1.1 manifest

**Direction:** Client to Server

Declares the application's UI structure. This MUST be the first message the SDK
sends after receiving `config`.

**Required fields:**

| Field | Type | Description |
|-------|------|-------------|
| `type` | `"manifest"` | Message type discriminator. |
| `app` | string | Application name (non-empty). |
| `screens` | object | Map of screen ID to `ScreenDescriptor`. |

**Optional fields:**

| Field | Type | Description |
|-------|------|-------------|
| `version` | string | Application or manifest version. |
| `currentScreen` | string | ID of the currently active screen. |
| `user` | `UserInfo` | Information about the current user. |
| `context` | object | Arbitrary application context data. |
| `persona` | `Persona` | Agent persona configuration. |

**Example:**

```json
{
    "type": "manifest",
    "app": "Acme CRM",
    "version": "2.1.0",
    "currentScreen": "contacts",
    "screens": {
        "contacts": {
            "id": "contacts",
            "label": "Contact List",
            "route": "/contacts",
            "fields": [
                {
                    "id": "search",
                    "type": "text",
                    "label": "Search contacts"
                }
            ],
            "actions": [
                {
                    "id": "add_contact",
                    "label": "Add Contact"
                }
            ],
            "modals": []
        }
    },
    "user": {
        "name": "Alice",
        "email": "alice@acme.com",
        "role": "admin"
    },
    "persona": {
        "name": "CRM Assistant",
        "role": "helper",
        "instructions": "Help the user manage their contacts."
    }
}
```

---

#### 9.1.2 text

**Direction:** Client to Server

Sends a user chat message to the engine.

**Required fields:**

| Field | Type | Description |
|-------|------|-------------|
| `type` | `"text"` | Message type discriminator. |
| `message` | string | The user's message (non-empty). |

**Example:**

```json
{
    "type": "text",
    "message": "Fill in the contact form for John Doe, email john@example.com"
}
```

---

#### 9.1.3 state

**Direction:** Client to Server

Reports the current field state of the active screen. SDKs SHOULD send this
message when significant state changes occur (e.g., after the user modifies
a field), or when the engine needs an updated snapshot.

**Required fields:**

| Field | Type | Description |
|-------|------|-------------|
| `type` | `"state"` | Message type discriminator. |
| `screen` | string | ID of the current screen. |

**Optional fields:**

| Field | Type | Description |
|-------|------|-------------|
| `fields` | object | Map of field ID to `FieldState`. |
| `canSubmit` | boolean | Whether the form is currently valid for submission. |

Each `FieldState` object contains:

| Field | Type | Description |
|-------|------|-------------|
| `value` | any | Current value of the field. |
| `valid` | boolean | Whether the field passes validation. |
| `error` | string | Validation error message, if any. |
| `dirty` | boolean | Whether the field has been modified by the user. |

**Example:**

```json
{
    "type": "state",
    "screen": "contact_form",
    "fields": {
        "first_name": {
            "value": "John",
            "valid": true,
            "dirty": true
        },
        "email": {
            "value": "",
            "valid": false,
            "error": "Email is required",
            "dirty": false
        }
    },
    "canSubmit": false
}
```

---

#### 9.1.4 result

**Direction:** Client to Server

Reports the outcome of executing a command's actions. The SDK MUST send this
message after executing a command, using the same `seq` number from the
command.

**Required fields:**

| Field | Type | Description |
|-------|------|-------------|
| `type` | `"result"` | Message type discriminator. |
| `seq` | integer | Sequence number matching the command (>= 0). |
| `results` | array | Array of `ActionResult` objects. |

**Optional fields:**

| Field | Type | Description |
|-------|------|-------------|
| `state` | `InlineState` | A state snapshot taken after command execution. |

Each `ActionResult` object contains:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `index` | integer | Yes | Zero-based index of the action in the command's `actions` array. |
| `success` | boolean | Yes | Whether the action completed successfully. |
| `error` | string | No | Human-readable error description on failure. |

The `InlineState` object has the same structure as a `state` message body but
without the `type` field. It contains `screen`, `fields`, and `canSubmit`.

**Example:**

```json
{
    "type": "result",
    "seq": 1,
    "results": [
        { "index": 0, "success": true },
        { "index": 1, "success": true },
        { "index": 2, "success": false, "error": "Field 'zipcode' not found" }
    ],
    "state": {
        "screen": "contact_form",
        "fields": {
            "first_name": { "value": "John", "valid": true, "dirty": true },
            "last_name": { "value": "Doe", "valid": true, "dirty": true }
        },
        "canSubmit": false
    }
}
```

---

#### 9.1.5 confirm

**Direction:** Client to Server

Responds to an `ask_confirm` action. When a command contains an `ask_confirm`
action, the SDK MUST present the confirmation prompt to the user and send this
message with the user's response.

**Required fields:**

| Field | Type | Description |
|-------|------|-------------|
| `type` | `"confirm"` | Message type discriminator. |
| `seq` | integer | Sequence number matching the command (>= 0). |
| `confirmed` | boolean | `true` if the user confirmed, `false` if declined. |

**Example:**

```json
{
    "type": "confirm",
    "seq": 3,
    "confirmed": true
}
```

---

#### 9.1.6 llm_config

**Direction:** Client to Server

Requests a change in the LLM provider used by the engine.

**Required fields:**

| Field | Type | Description |
|-------|------|-------------|
| `type` | `"llm_config"` | Message type discriminator. |
| `provider` | string | Provider identifier (as listed in the `config` message's `providers` array). |

**Example:**

```json
{
    "type": "llm_config",
    "provider": "anthropic-claude"
}
```

---

#### 9.1.7 response_lang_config

**Direction:** Client to Server

Requests a change in the language used for the engine's responses.

**Required fields:**

| Field | Type | Description |
|-------|------|-------------|
| `type` | `"response_lang_config"` | Message type discriminator. |
| `language` | string | Language code (e.g., `"en"`, `"pt-BR"`, `"es"`). |

**Example:**

```json
{
    "type": "response_lang_config",
    "language": "pt-BR"
}
```

---

### 9.2 Server Messages

#### 9.2.1 config

**Direction:** Server to Client

Provides session configuration. This MUST be the first message the engine sends
after the WebSocket connection is established.

**Required fields:**

| Field | Type | Description |
|-------|------|-------------|
| `type` | `"config"` | Message type discriminator. |
| `sessionId` | string | Unique session identifier. |

**Optional fields:**

| Field | Type | Description |
|-------|------|-------------|
| `features` | object | Feature flags (e.g., `{ "chat": true }`). Supports additional properties for extensions. |
| `providers` | array | Available LLM providers. Each element is a `ProviderInfo` with `id`, `name`, and `model`. |
| `current_provider` | string | ID of the currently active provider. |

**Example:**

```json
{
    "type": "config",
    "sessionId": "sess_abc123def456",
    "features": {
        "chat": true
    },
    "providers": [
        { "id": "openai-gpt4", "name": "GPT-4", "model": "gpt-4o" },
        { "id": "anthropic-claude", "name": "Claude", "model": "claude-sonnet-4-20250514" }
    ],
    "current_provider": "anthropic-claude"
}
```

---

#### 9.2.2 command

**Direction:** Server to Client

Instructs the SDK to execute one or more UI actions. Commands are the primary
mechanism by which the agent operates the application's interface.

**Required fields:**

| Field | Type | Description |
|-------|------|-------------|
| `type` | `"command"` | Message type discriminator. |
| `seq` | integer | Sequence number for this command (>= 0). Must be unique within the session. |
| `actions` | array | Ordered array of `UIAction` objects (at least one). |

See [Section 12: UI Actions](#12-ui-actions) for the full specification of each
action type.

**Example:**

```json
{
    "type": "command",
    "seq": 1,
    "actions": [
        { "do": "set_field", "field": "first_name", "value": "John" },
        { "do": "set_field", "field": "last_name", "value": "Doe" },
        { "do": "set_field", "field": "email", "value": "john@example.com" },
        { "do": "click", "action": "save" }
    ]
}
```

---

#### 9.2.3 chat

**Direction:** Server to Client

Delivers a chat message from the agent (or system). This message type is also
used for streaming: when `delta` is `true`, the `message` field contains a text
fragment (token) of the agent's response. See [Section 14: Streaming](#14-streaming)
for details.

**Required fields:**

| Field | Type | Description |
|-------|------|-------------|
| `type` | `"chat"` | Message type discriminator. |
| `from` | string | Message sender. One of: `"agent"`, `"user"`, `"system"`. |
| `message` | string | The message text (complete message or streaming token). |

**Optional fields:**

| Field | Type | Description |
|-------|------|-------------|
| `final` | boolean | When `true`, indicates this is the complete, final message (concludes a streaming sequence). |
| `delta` | boolean | When `true`, indicates this is a streaming token (a fragment of the full response). |

> **Note:** `delta` and `final` are mutually exclusive. A message MUST NOT have
> both `delta: true` and `final: true`.

**Example (final message):**

```json
{
    "type": "chat",
    "from": "agent",
    "message": "I've filled in the contact form for John Doe. Would you like me to save it?",
    "final": true
}
```

**Example (streaming token):**

```json
{
    "type": "chat",
    "from": "agent",
    "message": "I've filled",
    "delta": true
}
```

---

#### 9.2.4 status

**Direction:** Server to Client

Informs the SDK of the agent's current processing status.

**Required fields:**

| Field | Type | Description |
|-------|------|-------------|
| `type` | `"status"` | Message type discriminator. |
| `status` | string | One of: `"idle"`, `"thinking"`, `"executing"`. |

**Status values:**

| Value | Meaning |
|-------|---------|
| `idle` | The agent is ready to receive input. |
| `thinking` | The agent is processing a request (e.g., waiting for the LLM). |
| `executing` | The agent is executing UI commands. |

**Example:**

```json
{
    "type": "status",
    "status": "thinking"
}
```

---

#### 9.2.5 error

**Direction:** Server to Client

Notifies the SDK of an error condition. See
[Section 15: Error Handling](#15-error-handling) for error codes.

**Required fields:**

| Field | Type | Description |
|-------|------|-------------|
| `type` | `"error"` | Message type discriminator. |
| `message` | string | Human-readable error description. |

**Optional fields:**

| Field | Type | Description |
|-------|------|-------------|
| `code` | string | Machine-readable error code. |

**Example:**

```json
{
    "type": "error",
    "code": "invalid_manifest",
    "message": "Screen 'checkout' references unknown field type 'color_picker'."
}
```

---

## 10. Manifest Structure

The manifest is the SDK's declaration of the application's user interface. It
is sent once at the beginning of a session and provides the engine with all the
information it needs to understand and operate the UI.

### 10.1 Top-Level Structure

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `type` | `"manifest"` | Yes | Message type discriminator. |
| `app` | string | Yes | Application name (non-empty). |
| `screens` | object | Yes | Map of screen ID (string) to `ScreenDescriptor`. |
| `version` | string | No | Application or manifest version string. |
| `currentScreen` | string | No | ID of the currently active screen. |
| `user` | `UserInfo` | No | Information about the authenticated user. |
| `context` | object | No | Arbitrary key-value data providing application context to the agent. |
| `persona` | `Persona` | No | Configuration for the agent's persona. |

### 10.2 ScreenDescriptor

Each screen represents a distinct view or page in the application.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `id` | string | Yes | Unique screen identifier (non-empty). |
| `label` | string | Yes | Human-readable screen name. |
| `route` | string | No | Application route or URL path associated with this screen. |
| `fields` | array | No | Array of `FieldDescriptor` objects. |
| `actions` | array | No | Array of `ActionDescriptor` objects. |
| `modals` | array | No | Array of `ModalDescriptor` objects. |

### 10.3 FieldDescriptor

Each field represents an interactive input element within a screen.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `id` | string | Yes | Unique field identifier within the screen (non-empty). |
| `type` | string | Yes | Field type (see [Section 11](#11-field-types)). |
| `label` | string | Yes | Human-readable field label. |
| `required` | boolean | No | Whether the field must be filled before submission. |
| `mask` | string | No | Input mask pattern (e.g., `"###.###.###-##"`). Applicable to `masked` fields. |
| `placeholder` | string | No | Placeholder text displayed when the field is empty. |
| `options` | array | No | Array of `SelectOption` objects. Applicable to `select`, `radio`, and `autocomplete` fields. |
| `source` | string | No | Data source identifier for dynamic options (e.g., API endpoint). |
| `min` | number | No | Minimum value. Applicable to `number`, `currency`, `date`, and `datetime` fields. |
| `max` | number | No | Maximum value. Applicable to `number`, `currency`, `date`, and `datetime` fields. |
| `maxLength` | integer | No | Maximum input length (>= 0). |
| `readOnly` | boolean | No | Whether the field is read-only. |

Each `SelectOption` object contains:

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `value` | string | Yes | The option's programmatic value. |
| `label` | string | Yes | The option's human-readable label. |

### 10.4 ActionDescriptor

Each action represents a triggerable operation within a screen (e.g., a button).

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `id` | string | Yes | Unique action identifier within the screen (non-empty). |
| `label` | string | Yes | Human-readable action label. |
| `requiresConfirmation` | boolean | No | Whether the agent should confirm with the user before triggering this action. |
| `destructive` | boolean | No | Whether this action is destructive (e.g., delete). Engines SHOULD treat destructive actions with extra caution. |
| `disabled` | boolean | No | Whether the action is currently disabled. |

### 10.5 ModalDescriptor

Each modal represents a dialog overlay that can be opened within a screen.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `id` | string | Yes | Unique modal identifier within the screen (non-empty). |
| `label` | string | Yes | Human-readable modal title. |
| `searchable` | boolean | No | Whether the modal supports search functionality. |

### 10.6 UserInfo

User information provided to the engine for personalization.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | No | User's display name. |
| `email` | string | No | User's email address. |
| `org` | string | No | User's organization. |
| `role` | string | No | User's role within the application. |

The `UserInfo` object supports `additionalProperties`, allowing implementations
to include custom user attributes.

### 10.7 Persona

Configuration for the agent's persona and behavior.

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| `name` | string | No | The agent persona's name. |
| `role` | string | No | The agent persona's role. |
| `instructions` | string | No | Custom instructions for the agent's behavior. |

The `Persona` object supports `additionalProperties` (`additionalProperties: true`),
allowing implementations to include custom persona attributes for extensibility.

---

## 11. Field Types

ACP defines 15 standard field types. Engines and SDKs MUST support all of these
types.

| Type | Description | Typical HTML Equivalent | Example Use |
|------|-------------|------------------------|-------------|
| `text` | Single-line text input. | `<input type="text">` | Name, address |
| `number` | Numeric input. | `<input type="number">` | Quantity, age |
| `currency` | Monetary value input. | `<input type="text">` with formatting | Price, total |
| `date` | Date picker. | `<input type="date">` | Birth date, due date |
| `datetime` | Date and time picker. | `<input type="datetime-local">` | Appointment, event |
| `email` | Email address input. | `<input type="email">` | Contact email |
| `phone` | Phone number input. | `<input type="tel">` | Contact phone |
| `masked` | Input with a mask pattern. | `<input>` with mask library | SSN (`###-##-####`), CPF (`###.###.###-##`) |
| `select` | Dropdown single selection. | `<select>` | Country, category |
| `autocomplete` | Search-and-select input. | `<input>` with dropdown | City, product |
| `checkbox` | Boolean toggle. | `<input type="checkbox">` | Agree to terms |
| `radio` | Single choice from a group. | `<input type="radio">` | Gender, plan tier |
| `textarea` | Multi-line text input. | `<textarea>` | Description, notes |
| `file` | File upload. | `<input type="file">` | Attachment, avatar |
| `hidden` | Non-visible field. | `<input type="hidden">` | Internal ID, token |

---

## 12. UI Actions

UI actions are the individual operations within a command that instruct the SDK
to manipulate the application's interface. Each action is a JSON object with a
`do` field that identifies the action type.

Actions are divided into two execution categories:

- **Sequential actions** MUST be executed one at a time, in the order they
  appear in the command's `actions` array. The SDK MUST wait for a sequential
  action to complete before proceeding to the next action.
- **Parallel actions** MAY be executed concurrently with other parallel actions.
  When a contiguous group of parallel actions appears in the `actions` array,
  the SDK MAY execute them simultaneously.

When sequential and parallel actions are interleaved in a command, the SDK MUST
respect the ordering: parallel actions between two sequential actions MAY run
concurrently, but the SDK MUST NOT proceed past a sequential action until it
completes.

### 12.1 Sequential Actions

#### 12.1.1 navigate

Switch the application to a different screen.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `do` | `"navigate"` | Yes | Action type. |
| `screen` | string | Yes | Target screen ID (must exist in the manifest). |

**Behavior:** The SDK MUST transition the application to the specified screen.
If the screen ID does not exist in the manifest, the SDK MUST report an error
in the action result.

**Example:**

```json
{ "do": "navigate", "screen": "contact_form" }
```

---

#### 12.1.2 click

Trigger a registered action (button press).

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `do` | `"click"` | Yes | Action type. |
| `action` | string | Yes | Target action ID (must exist in the current screen's actions). |

**Behavior:** The SDK MUST invoke the callback associated with the specified
action. If the action ID does not exist or is disabled, the SDK MUST report an
error in the action result.

**Example:**

```json
{ "do": "click", "action": "save" }
```

---

#### 12.1.3 show_toast

Display a toast notification to the user.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `do` | `"show_toast"` | Yes | Action type. |
| `message` | string | Yes | Toast message text. |
| `level` | string | No | Severity level. One of: `"info"`, `"success"`, `"warning"`, `"error"`. Default: `"info"`. |
| `duration` | integer | No | Display duration in milliseconds (>= 0). Default: implementation-defined (RECOMMENDED: 3000). |

**Behavior:** The SDK MUST display the toast message to the user with the
specified level and duration. The SDK SHOULD use the application's existing
toast/notification system.

**Example:**

```json
{ "do": "show_toast", "message": "Contact saved successfully.", "level": "success", "duration": 4000 }
```

---

#### 12.1.4 ask_confirm

Request user confirmation before proceeding.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `do` | `"ask_confirm"` | Yes | Action type. |
| `message` | string | Yes | Confirmation prompt text. |

**Behavior:** The SDK MUST present the confirmation prompt to the user and
pause command execution until the user responds. The SDK MUST then send a
`confirm` message (not a `result` message) with the user's response. If the
user declines (`confirmed: false`), the engine decides how to proceed.

An `ask_confirm` action MUST NOT appear together with other actions in the same
command. If it does, the SDK SHOULD execute only the `ask_confirm` and ignore
remaining actions.

**Example:**

```json
{ "do": "ask_confirm", "message": "Are you sure you want to delete this contact?" }
```

---

#### 12.1.5 open_modal

Open a modal dialog.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `do` | `"open_modal"` | Yes | Action type. |
| `modal` | string | Yes | Target modal ID (must exist in the current screen's modals). |
| `query` | string | No | Pre-fill the modal's search field with this value. |

**Behavior:** The SDK MUST open the specified modal. If a `query` is provided
and the modal is searchable, the SDK SHOULD populate the search field. If the
modal ID does not exist, the SDK MUST report an error.

**Example:**

```json
{ "do": "open_modal", "modal": "product_search", "query": "Widget Pro" }
```

---

#### 12.1.6 close_modal

Close the currently open modal.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `do` | `"close_modal"` | Yes | Action type. |

**Behavior:** The SDK MUST close the currently open modal dialog. If no modal
is open, the SDK MAY treat this as a no-op and report success.

**Example:**

```json
{ "do": "close_modal" }
```

---

### 12.2 Parallel Actions

#### 12.2.1 set_field

Set the value of a field. This action handles all field types including text
inputs, select dropdowns, radio buttons, and autocomplete fields.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `do` | `"set_field"` | Yes | Action type. |
| `field` | string | Yes | Target field ID. |
| `value` | any | Yes | The value to set. |

**Behavior:** The SDK MUST set the specified field to the given value. For
`select`, `radio`, and `autocomplete` fields, the value MUST match one of the
available options; if it does not, the SDK MUST report an error. Animation of
value changes (e.g., typewriter effects) is a presentation concern left to the
SDK implementation.

**Example:**

```json
{ "do": "set_field", "field": "first_name", "value": "John" }
```

---

#### 12.2.2 clear

Clear a field's value.

| Parameter | Type | Required | Description |
|-----------|------|----------|-------------|
| `do` | `"clear"` | Yes | Action type. |
| `field` | string | Yes | Target field ID. |

**Behavior:** The SDK MUST clear the specified field, resetting it to its
default empty state.

**Example:**

```json
{ "do": "clear", "field": "email" }
```

---

## 13. Command Execution Model

This section defines how SDKs MUST process commands received from the engine.

### 13.1 Command Structure

A command message contains:

- A `seq` (sequence number) that uniquely identifies the command within the
  session.
- An `actions` array containing one or more `UIAction` objects.

### 13.2 Execution Rules

1. The SDK MUST attempt to execute **all** actions in the command.

2. The SDK MUST respect the distinction between sequential and parallel actions
   as defined in [Section 12](#12-ui-actions).

3. After executing all actions, the SDK MUST send a `result` message with the
   same `seq` number as the command.

4. The `result` message MUST contain one `ActionResult` entry for each action
   in the command, with the `index` field corresponding to the action's
   zero-based position in the `actions` array.

5. Each `ActionResult` MUST include `index` (integer) and `success` (boolean).
   On failure, it SHOULD also include `error` (string) with a human-readable
   description.

6. The SDK SHOULD include an `InlineState` snapshot in the `result` message
   to inform the engine of the current field state after execution.

### 13.3 Confirmation Handling

When a command contains an `ask_confirm` action:

1. The SDK MUST present the confirmation prompt to the user.
2. The SDK MUST pause execution of any remaining actions in the command.
3. The SDK MUST send a `confirm` message (not a `result` message) with the
   same `seq` number and `confirmed` set to the user's response.
4. The engine decides the next course of action based on the confirmation.

### 13.4 Retry Policy

SDKs SHOULD implement automatic retry logic for failed actions:

- Retry up to **3 times** per action.
- Wait **300 milliseconds** between retry attempts.
- If all retries fail, report the action as failed in the `result` message.

Retries are particularly useful for actions that depend on UI rendering timing
(e.g., a field that has not yet appeared after a screen transition).

### 13.5 Sequence Number Correlation

The engine assigns monotonically increasing sequence numbers to commands. The
SDK MUST include the exact same `seq` value in the corresponding `result` or
`confirm` response. Failure to correlate sequence numbers is a protocol
violation.

---

## 14. Streaming

The engine MAY stream agent responses incrementally using `chat` messages with
`delta: true`.

### 14.1 Streaming Sequence

1. The engine sends zero or more `chat` messages with `delta: true`, each
   containing a `message` field with a text fragment (token).
2. The engine sends a `chat` message containing the complete response, with
   `final: true`.

### 14.2 SDK Behavior

- SDKs SHOULD render `chat` messages with `delta: true` in real-time to provide
  a responsive user experience (e.g., character-by-character or word-by-word
  display).
- When the final `chat` message arrives (with `final: true`), SDKs SHOULD
  replace the accumulated tokens with the complete message to ensure accuracy.
- SDKs that do not support streaming MAY ignore `chat` messages with
  `delta: true` and only render the final `chat` message.

---

## 15. Error Handling

### 15.1 Error Messages

The engine sends `error` messages to notify the SDK of error conditions. Each
error message includes a human-readable `message` field and an optional
machine-readable `code` field.

### 15.2 Error Codes

The following error codes are RECOMMENDED for use by engine implementations:

| Code | Description |
|------|-------------|
| `invalid_manifest` | The manifest message is malformed or contains invalid data. |
| `invalid_message` | A message from the SDK is malformed or violates the protocol schema. |
| `rate_limited` | The client has exceeded the allowed request rate. |
| `provider_error` | The LLM provider returned an error or is unavailable. |
| `session_expired` | The session has timed out or been invalidated. |
| `unauthorized` | Authentication failed or credentials are missing. |
| `internal_error` | An unexpected error occurred within the engine. |

### 15.3 SDK Error Handling

- SDKs SHOULD display error messages to the user in an appropriate manner.
- On receiving `session_expired` or `unauthorized`, SDKs SHOULD close the
  connection and either prompt the user to re-authenticate or attempt
  reconnection with fresh credentials.
- On receiving `rate_limited`, SDKs SHOULD implement backoff before sending
  additional messages.

### 15.4 Error Recovery Patterns

This section describes RECOMMENDED recovery patterns for common error scenarios.

#### 15.4.1 Field Not Found

When the agent references a field that does not exist in the current screen:

```json
// Engine sends command
{ "type": "command", "seq": 3, "actions": [{ "do": "set_field", "field": "nonexistent_field", "value": "test" }] }

// SDK returns error result
{ "type": "result", "seq": 3, "results": [{ "index": 0, "success": false, "error": "Field 'nonexistent_field' not found on screen 'main'" }] }
```

The engine SHOULD relay this error to the LLM, which can use the manifest
information to identify the correct field and retry.

#### 15.4.2 Invalid Field Value

When the agent provides a value incompatible with the field type:

```json
// Engine sends command (text value for number field)
{ "type": "command", "seq": 4, "actions": [{ "do": "set_field", "field": "age", "value": "not-a-number" }] }

// SDK returns error result
{ "type": "result", "seq": 4, "results": [{ "index": 0, "success": false, "error": "Field 'age' expects a numeric value" }] }
```

SDKs SHOULD validate values against field types before applying them.

#### 15.4.3 Network Interruption During Execution

If the WebSocket connection drops while the SDK is executing commands:

1. The SDK SHOULD attempt reconnection as described in Section 7.5.
2. On reconnection, the SDK MUST send a fresh `manifest` message reflecting
   the current UI state (including any partially-applied changes).
3. The engine MUST treat the reconnection as a new session context.
4. Any in-flight `result` messages for the previous connection are discarded.

#### 15.4.4 Command Timeout

If the SDK does not send a `result` within the expected timeout period
(RECOMMENDED: 30 seconds):

1. The engine SHOULD send an `error` message with a descriptive message.
2. The engine SHOULD transition to IDLE state.
3. The SDK MAY discard any pending command execution.

#### 15.4.5 LLM Provider Failure

If the LLM provider returns an error or becomes unreachable:

```json
{ "type": "error", "code": "provider_error", "message": "LLM provider returned HTTP 503" }
```

The engine SHOULD transition to IDLE state after sending the error, allowing
the user to retry their request.

---

## 16. Security Considerations

### 16.1 Transport Security

Implementations MUST use TLS (i.e., `wss://` WebSocket URIs) in production
environments. Unencrypted `ws://` connections SHOULD only be used in local
development.

### 16.2 Authentication

Authentication is implementation-defined. Engines SHOULD support at least one
of the following mechanisms:

- JWT tokens passed as a query string parameter or HTTP header during the
  WebSocket handshake.
- API keys passed as a query string parameter or HTTP header.
- Cookie-based session authentication.

### 16.3 Manifest Trust

The manifest is authored by the SDK and describes the application's own UI.
The engine trusts the manifest as an accurate representation of the
application's interface. However:

- Engines SHOULD validate manifest structure against the ACP schema.
- Engines SHOULD verify that field types are valid and screen references are
  consistent.
- Engines MUST NOT execute arbitrary code based on manifest content.

### 16.4 Rate Limiting

Engines SHOULD implement rate limiting per client to prevent abuse. The
RECOMMENDED approach is to limit the number of `text` messages per time window
(e.g., 60 messages per minute).

### 16.5 Message Validation

SDKs SHOULD validate incoming server messages against the ACP schema before
processing them. This protects against malformed messages from compromised or
buggy engine implementations.

---

## 17. Versioning

### 17.1 Version Identification

The protocol version is identified by the `$id` URL in the JSON Schema:

```
https://acp-protocol.org/schemas/acp-v1.json
```

The `v1` segment indicates version 1 of the protocol. Future versions will
use `v2`, `v3`, and so on.

### 17.2 Manifest Version

Implementations SHOULD include a `version` field in the `manifest` message to
indicate the application version. This is distinct from the protocol version and
serves as metadata for the engine.

### 17.3 Compatibility Policy

- Minor, backward-compatible changes (new optional fields, new optional message
  types) MAY be made within a major version.
- Breaking changes (removal of fields, changes to required fields, changes to
  message semantics) MUST increment the major version number.
- Implementations SHOULD be tolerant of unknown fields in messages they receive,
  to support forward compatibility with minor revisions.

---

## 18. Extensions

### 18.1 Extension Mechanism

The core ACP protocol handles text-based interaction and UI control.
Implementations MAY extend the protocol to support additional capabilities and
modalities.

### 18.2 Extension Guidelines

Extensions SHOULD follow these principles:

1. **Add, don't modify.** Extensions SHOULD add new message types rather than
   modifying the semantics of existing ones.

2. **Extend enums additively.** Extensions MAY add new values to string enums
   (such as `status`) but MUST NOT remove existing values.

3. **Use extension points.** Objects marked with `additionalProperties: true`
   in the schema (such as `UserInfo`, `Persona`, and `features`) are designed as extension
   points. Extensions SHOULD use these where appropriate.

4. **Document separately.** Extensions SHOULD be documented in their own
   specification documents that reference this core specification.

5. **Namespace extension fields.** Extensions SHOULD use a consistent prefix
   for custom fields to avoid collisions (e.g., `voice_` for voice extension
   fields).

### 18.3 Example: Voice Extension

A voice interaction extension might add:

- A separate WebSocket endpoint (e.g., `/ws/stream`) for audio data.
- New message types: `config` (audio settings), `partial` (interim
  transcription), `transcription` (final transcription), `tts_start`,
  `tts_end`.
- New status values: `listening`, `speaking`.
- New feature flags: `{ "voice": true, "wake_word": true }`.

Such an extension would be documented separately and would not alter the core
protocol messages defined in this specification.

---

## 19. Conformance

### 19.1 Conformance Requirements

An implementation is considered ACP-compliant if it satisfies all of the
following requirements:

1. **Message types.** The implementation MUST support all core message types
   defined in this specification (7 client message types and 5 server message
   types).

2. **UI actions.** The implementation MUST support all 8 UI action types
   defined in [Section 12](#12-ui-actions).

3. **Connection lifecycle.** The implementation MUST follow the connection
   lifecycle defined in [Section 8](#8-connection-lifecycle):
   - Engines MUST send `config` as the first message.
   - SDKs MUST send `manifest` as the first message after `config`.

4. **Sequence correlation.** The implementation MUST correctly correlate
   command `seq` numbers with `result` or `confirm` responses.

5. **Field types.** The implementation MUST support all 15 field types defined
   in [Section 11](#11-field-types).

6. **Schema validation.** Messages produced by the implementation MUST conform
   to the ACP v1 JSON Schema (`acp-v1.json`).

### 19.2 Conformance Levels

- **ACP Engine Conformant:** The server implementation satisfies all engine-side
  requirements (sends valid `config`, `command`, `chat`, `status`, and `error`
  messages; correctly processes all client message types).

- **ACP SDK Conformant:** The client implementation satisfies all SDK-side
  requirements (sends valid `manifest`, `text`, `state`, `result`, `confirm`,
  `llm_config`, and `response_lang_config` messages; correctly executes all
  8 UI actions; correctly processes all server message types).

### 19.3 Conformance Test Suite

The official ACP conformance test suite is available in the `conformance/`
directory of the protocol repository. Implementations SHOULD pass all tests
in the suite before claiming conformance.

---

## Appendix A: Complete Message Type Summary

### Client Messages (SDK to Engine)

| Type | Required Fields | Optional Fields | Description |
|------|----------------|-----------------|-------------|
| `manifest` | `type`, `app`, `screens` | `version`, `currentScreen`, `user`, `context`, `persona` | Declares the application UI. |
| `text` | `type`, `message` | -- | User chat message. |
| `state` | `type`, `screen` | `fields`, `canSubmit` | Current field state snapshot. |
| `result` | `type`, `seq`, `results` | `state` | Command execution results. |
| `confirm` | `type`, `seq`, `confirmed` | -- | Response to `ask_confirm`. |
| `llm_config` | `type`, `provider` | -- | Switch LLM provider. |
| `response_lang_config` | `type`, `language` | -- | Switch response language. |

### Server Messages (Engine to SDK)

| Type | Required Fields | Optional Fields | Description |
|------|----------------|-----------------|-------------|
| `config` | `type`, `sessionId` | `features`, `providers`, `current_provider` | Session configuration. |
| `command` | `type`, `seq`, `actions` | -- | UI actions to execute. |
| `chat` | `type`, `from`, `message` | `final`, `delta` | Agent chat message (also used for streaming tokens). |
| `status` | `type`, `status` | -- | Agent status update. |
| `error` | `type`, `message` | `code` | Error notification. |

---

## Appendix B: JSON Schema

The machine-readable JSON Schema definition for ACP v1 is available at:

- **File:** [`acp-v1.json`](acp-v1.json)
- **Canonical URL:** `https://acp-protocol.org/schemas/acp-v1.json`

The schema conforms to [JSON Schema draft 2020-12](https://json-schema.org/draft/2020-12/schema)
and defines all message types, UI actions, manifest structures, and validation
rules specified in this document.

Implementors MAY use the schema for:

- **Compile-time code generation** of message types in any language.
- **Runtime validation** of incoming and outgoing messages.
- **Documentation generation** from the schema annotations.

---

*End of specification.*
