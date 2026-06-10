// Types based on Anthropic Messages API + copilot-api reference

// ========== Anthropic Request (what we receive) ==========
export interface AnthropicMessage {
  role: "user" | "assistant";
  content: string | AnthropicContentBlock[];
}

export type AnthropicContentBlock =
  | { type: "text"; text: string }
  | { type: "image"; source: { type: "base64"; media_type: string; data: string } }
  | { type: "tool_use"; id: string; name: string; input: Record<string, unknown> }
  | { type: "tool_result"; tool_use_id: string; content: string };

export interface AnthropicRequest {
  model: string;
  messages: AnthropicMessage[];
  system?: string;
  max_tokens: number;
  stream?: boolean;
  temperature?: number;
  top_p?: number;
  stop_sequences?: string[];
  tools?: AnthropicTool[];
  tool_choice?: { type: "auto" | "any" | "tool"; name?: string };
  metadata?: { user_id?: string };
}

export interface AnthropicTool {
  name: string;
  description?: string;
  input_schema: Record<string, unknown>;
}

// ========== Anthropic Response (what we return) ==========
export interface AnthropicResponse {
  id: string;
  type: "message";
  role: "assistant";
  content: AnthropicContentBlock[];
  model: string;
  stop_reason: "end_turn" | "max_tokens" | "stop_sequence" | "tool_use" | null;
  stop_sequence: string | null;
  usage: { input_tokens: number; output_tokens: number };
}

// ========== Anthropic SSE Events (streaming) ==========
export interface AnthropicSSEEvent {
  type: "message_start" | "content_block_start" | "content_block_delta" | "content_block_stop" | "message_delta" | "message_stop" | "ping";
  message?: unknown;
  index?: number;
  content_block?: unknown;
  delta?: { type: string; text?: string; partial_json?: string };
  usage?: { output_tokens: number };
}

// ========== OpenAI Request (what we send to Copilot) ==========
export interface OpenAIMessage {
  role: "system" | "user" | "assistant" | "tool";
  content: string | { type: string; text?: string; image_url?: { url: string } }[];
  tool_calls?: OpenAIToolCall[];
  tool_call_id?: string;
}

export interface OpenAIToolCall {
  index: number;
  id?: string;
  type: "function";
  function: { name: string; arguments: string };
}

export interface OpenAIRequest {
  model: string;
  messages: OpenAIMessage[];
  max_tokens: number;
  stream?: boolean;
  temperature?: number;
  top_p?: number;
  stop?: string[];
  tools?: OpenAITool[];
  tool_choice?: string | { type: string; function?: { name: string } };
}

export interface OpenAITool {
  type: "function";
  function: {
    name: string;
    description?: string;
    parameters: Record<string, unknown>;
  };
}

// ========== OpenAI Response ==========
export interface OpenAIResponse {
  id: string;
  object: string;
  model: string;
  choices: {
    index: number;
    message: OpenAIMessage;
    finish_reason: string;
  }[];
  usage: { prompt_tokens: number; completion_tokens: number; total_tokens: number };
}

// ========== Config ==========
export interface ModelMapping {
  [customName: string]: string;  // customName -> copilotInternalId
}

export interface AppConfig {
  port: number;
  copilotBaseUrl: string;
  modelMappings: ModelMapping;
}
