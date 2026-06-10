// Anthropic ↔ OpenAI translation
// Adapted from copilot-api/src/routes/messages/non-stream-translation.ts

import type {
  AnthropicRequest, AnthropicResponse, AnthropicContentBlock,
  AnthropicMessage, AnthropicTool,
  OpenAIRequest, OpenAIMessage, OpenAIToolCall, OpenAITool,
  OpenAIResponse,
  AnthropicSSEEvent,
} from "./types";

// ========== Model name (pass-through, no mapping) ==========
let allowedModels: Set<string> = new Set();

export function setAllowedModels(models: string[]) {
  allowedModels = new Set(models);
}

export function getAllowedModels(): string[] {
  return [...allowedModels];
}

function translateModelName(model: string): string {
  // Pass through: model names ARE the Copilot model IDs
  return model;
}

// ========== Request Translation: Anthropic → OpenAI ==========
export function translateRequest(req: AnthropicRequest): OpenAIRequest {
  const messages: OpenAIMessage[] = [];

  // System prompt
  if (req.system) {
    messages.push({ role: "system", content: req.system });
  }

  // Convert messages
  for (const msg of req.messages) {
    if (msg.role === "user") messages.push(...handleUserMessage(msg));
    else messages.push(...handleAssistantMessage(msg));
  }

  const openaiReq: OpenAIRequest = {
    model: translateModelName(req.model),
    messages,
    max_tokens: req.max_tokens,
    stream: req.stream ?? false,
    temperature: req.temperature,
    top_p: req.top_p,
    stop: req.stop_sequences,
  };

  // Translate tools
  if (req.tools?.length) {
    openaiReq.tools = req.tools.map(translateTool);
    openaiReq.tool_choice = translateToolChoice(req.tool_choice);
  }

  return openaiReq;
}

function handleUserMessage(msg: AnthropicMessage): OpenAIMessage[] {
  if (typeof msg.content === "string") {
    return [{ role: "user", content: msg.content }];
  }

  const toolResults = msg.content.filter(b => b.type === "tool_result");
  const otherBlocks = msg.content.filter(b => b.type !== "tool_result");

  const messages: OpenAIMessage[] = [];

  // Tool results → separate tool messages
  for (const block of toolResults) {
    if (block.type === "tool_result") {
      messages.push({
        role: "tool",
        tool_call_id: block.tool_use_id,
        content: block.content,
      });
    }
  }

  // Text + image blocks
  if (otherBlocks.length > 0) {
    const content = otherBlocks.map(block => {
      if (block.type === "text") return { type: "text" as const, text: block.text };
      if (block.type === "image") return {
        type: "image_url" as const,
        image_url: { url: `data:${block.source.media_type};base64,${block.source.data}` }
      };
      return { type: "text" as const, text: "" };
    });
    messages.push({ role: "user", content: content.length === 1 && content[0].type === "text" ? content[0].text : content });
  }

  return messages;
}

function handleAssistantMessage(msg: AnthropicMessage): OpenAIMessage[] {
  if (typeof msg.content === "string") {
    return [{ role: "assistant", content: msg.content }];
  }

  const textBlocks = msg.content.filter(b => b.type === "text");
  const toolUses = msg.content.filter(b => b.type === "tool_use");

  const text = textBlocks.map(b => (b as { text: string }).text).join("\n");

  const toolCalls: OpenAIToolCall[] = toolUses.map((b, i) => ({
    index: i,
    id: (b as { id: string }).id,
    type: "function" as const,
    function: {
      name: (b as { name: string }).name,
      arguments: JSON.stringify((b as { input: Record<string, unknown> }).input),
    },
  }));

  return [{
    role: "assistant",
    content: text || null,
    ...(toolCalls.length ? { tool_calls: toolCalls } : {}),
  }];
}

function translateTool(tool: AnthropicTool): OpenAITool {
  return {
    type: "function",
    function: {
      name: tool.name,
      description: tool.description,
      parameters: tool.input_schema,
    },
  };
}

function translateToolChoice(tc?: { type: string; name?: string }): string | { type: string; function?: { name: string } } | undefined {
  if (!tc) return undefined;
  if (tc.type === "auto") return "auto";
  if (tc.type === "any") return "required";
  if (tc.type === "tool" && tc.name) return { type: "function", function: { name: tc.name } };
  return undefined;
}

// ========== Response Translation: OpenAI → Anthropic ==========
export function translateResponse(openai: OpenAIResponse, originalModel: string): AnthropicResponse {
  const choice = openai.choices[0];
  const message = choice.message;

  const content: AnthropicContentBlock[] = [];

  // Text content
  if (message.content) {
    content.push({
      type: "text",
      text: typeof message.content === "string" ? message.content : "",
    });
  }

  // Tool calls
  if (message.tool_calls) {
    for (const tc of message.tool_calls) {
      let input: Record<string, unknown> = {};
      try { input = JSON.parse(tc.function.arguments); } catch { /* */ }
      content.push({
        type: "tool_use",
        id: tc.id || `toolu_${Math.random().toString(36).slice(2)}`,
        name: tc.function.name,
        input,
      });
    }
  }

  return {
    id: openai.id,
    type: "message",
    role: "assistant",
    content,
    model: originalModel,
    stop_reason: mapStopReason(choice.finish_reason),
    stop_sequence: null,
    usage: {
      input_tokens: openai.usage.prompt_tokens,
      output_tokens: openai.usage.completion_tokens,
    },
  };
}

function mapStopReason(finish: string): AnthropicResponse["stop_reason"] {
  switch (finish) {
    case "stop": return "end_turn";
    case "length": return "max_tokens";
    case "tool_calls": return "tool_use";
    default: return null;
  }
}

// ========== Streaming: OpenAI SSE → Anthropic SSE ==========
export interface StreamContext {
  messageStartSent: boolean;
  messageId: string;
  model: string;
  contentBlockIndex: number;
  contentBlockOpen: boolean;
  toolCalls: Record<number, { id: string; name: string; args: string }>;
}

export function translateStreamChunk(
  chunk: { choices: { index: number; delta: { role?: string; content?: string; tool_calls?: { index: number; id?: string; function?: { name?: string; arguments?: string } }[] }; finish_reason: string | null }[] },
  ctx: StreamContext
): AnthropicSSEEvent[] {
  const events: AnthropicSSEEvent[] = [];
  const delta = chunk.choices[0]?.delta;
  if (!delta) return events;

  // message_start
  if (!ctx.messageStartSent) {
    ctx.messageStartSent = true;
    events.push({
      type: "message_start",
      message: { id: ctx.messageId, type: "message", role: "assistant", content: [], model: ctx.model, stop_reason: null, stop_sequence: null, usage: { input_tokens: 0, output_tokens: 0 } },
    });
  }

  // Tool calls
  if (delta.tool_calls) {
    for (const tc of delta.tool_calls) {
      const idx = tc.index;
      if (!ctx.toolCalls[idx]) {
        ctx.toolCalls[idx] = { id: tc.id || `toolu_${Math.random().toString(36).slice(2)}`, name: tc.function?.name || "", args: "" };
      }
      if (tc.id) ctx.toolCalls[idx].id = tc.id;
      if (tc.function?.name) ctx.toolCalls[idx].name = tc.function.name;

      // Close previous content block if open
      if (ctx.contentBlockOpen) {
        events.push({ type: "content_block_stop", index: ctx.contentBlockIndex });
        ctx.contentBlockOpen = false;
        ctx.contentBlockIndex++;
      }

      const t = ctx.toolCalls[idx];

      if (tc.function?.arguments) {
        t.args += tc.function.arguments;
      } else {
        // Start of tool use
        events.push({
          type: "content_block_start",
          index: ctx.contentBlockIndex,
          content_block: { type: "tool_use", id: t.id, name: t.name, input: {} },
        });
      }

      if (tc.function?.arguments) {
        events.push({
          type: "content_block_delta",
          index: ctx.contentBlockIndex,
          delta: { type: "input_json_delta", partial_json: tc.function.arguments },
        });
      }

      ctx.contentBlockOpen = true;
    }
    return events;
  }

  // Text content
  if (delta.content) {
    if (!ctx.contentBlockOpen) {
      events.push({
        type: "content_block_start",
        index: ctx.contentBlockIndex,
        content_block: { type: "text", text: "" },
      });
      ctx.contentBlockOpen = true;
    }
    events.push({
      type: "content_block_delta",
      index: ctx.contentBlockIndex,
      delta: { type: "text_delta", text: delta.content },
    });
  }

  // Finish
  if (chunk.choices[0].finish_reason) {
    if (ctx.contentBlockOpen) {
      events.push({ type: "content_block_stop", index: ctx.contentBlockIndex });
      ctx.contentBlockOpen = false;
    }
    events.push({
      type: "message_delta",
      delta: { stop_reason: mapAnthropicStopReason(chunk.choices[0].finish_reason) },
      usage: { output_tokens: 0 },
    });
    events.push({ type: "message_stop" });
  }

  return events;
}

function mapAnthropicStopReason(finish: string): string {
  switch (finish) {
    case "stop": return "end_turn";
    case "length": return "max_tokens";
    case "tool_calls": return "tool_use";
    default: return "end_turn";
  }
}
