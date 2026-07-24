package translate

import (
	"encoding/json"
	"strings"
	"testing"
)

func coreToolChunk(index int, id, name, arguments string) *OpenAIStreamChunk {
	return &OpenAIStreamChunk{
		Choices: []OpenAIStreamChoice{{
			Delta: OpenAIStreamDelta{
				ToolCalls: []OpenAIToolCallDelta{{
					Index: index,
					ID:    id,
					Function: &OpenAIToolFunctionDelta{
						Name:      name,
						Arguments: arguments,
					},
				}},
			},
		}},
	}
}

func TestCoreStream_InterleavedToolsEmitOrderedBlocksAtFinish(t *testing.T) {
	ctx := &StreamContext{
		MessageID: "msg_interleaved",
		Model:     "claude-opus-4.8",
		ToolCalls: make(map[int]*ToolCallState),
	}

	chunks := []*OpenAIStreamChunk{
		coreToolChunk(0, "call_0", "Read", `{"path":"a`),
		coreToolChunk(1, "call_1", "Grep", `{"query":"b"}`),
		coreToolChunk(0, "", "", `"}`),
	}
	for index, chunk := range chunks {
		events, err := OpenAIStreamChunkToCoreEvents(chunk, ctx)
		if err != nil {
			t.Fatal(err)
		}
		for _, event := range events {
			if strings.HasPrefix(event.Type, "content_block_") {
				t.Fatalf("chunk %d emitted tool block before finish: %+v", index, events)
			}
		}
	}

	finishReason := "tool_calls"
	events, err := OpenAIStreamChunkToCoreEvents(&OpenAIStreamChunk{
		Choices: []OpenAIStreamChoice{{FinishReason: &finishReason}},
	}, ctx)
	if err != nil {
		t.Fatal(err)
	}

	wantTypes := []string{
		"content_block_start", "content_block_delta", "content_block_stop",
		"content_block_start", "content_block_delta", "content_block_stop",
		"message_delta", "message_stop",
	}
	if len(events) != len(wantTypes) {
		t.Fatalf("events = %+v, want types %v", events, wantTypes)
	}
	for index, wantType := range wantTypes {
		if events[index].Type != wantType {
			t.Fatalf("event %d type = %q, want %q: %+v", index, events[index].Type, wantType, events)
		}
	}
	for _, index := range []int{0, 1, 2} {
		if events[index].Index != 0 {
			t.Fatalf("tool 0 event %d index = %d", index, events[index].Index)
		}
	}
	for _, index := range []int{3, 4, 5} {
		if events[index].Index != 1 {
			t.Fatalf("tool 1 event %d index = %d", index, events[index].Index)
		}
	}
	if events[1].PartialJSON != `{"path":"a"}` {
		t.Fatalf("tool 0 args = %q", events[1].PartialJSON)
	}
	if events[4].PartialJSON != `{"query":"b"}` {
		t.Fatalf("tool 1 args = %q", events[4].PartialJSON)
	}
}

func TestCoreStream_BuffersArgumentsBeforeName(t *testing.T) {
	ctx := &StreamContext{
		MessageID: "msg_delayed_name",
		Model:     "claude-opus-4.8",
		ToolCalls: make(map[int]*ToolCallState),
	}

	first, err := OpenAIStreamChunkToCoreEvents(coreToolChunk(0, "call_0", "", `{"path":"`), ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 || first[0].Type != "message_start" {
		t.Fatalf("unexpected pre-name events: %+v", first)
	}

	_, err = OpenAIStreamChunkToCoreEvents(coreToolChunk(0, "", "Read", `a"}`), ctx)
	if err != nil {
		t.Fatal(err)
	}
	finishReason := "tool_calls"
	events, err := OpenAIStreamChunkToCoreEvents(&OpenAIStreamChunk{
		Choices: []OpenAIStreamChoice{{FinishReason: &finishReason}},
	}, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) < 2 || events[1].PartialJSON != `{"path":"a"}` {
		t.Fatalf("arguments were not preserved: %+v", events)
	}
}

func TestCoreStream_RejectsMissingToolNameAtFinish(t *testing.T) {
	ctx := &StreamContext{
		MessageID: "msg_missing_name",
		Model:     "claude-opus-4.8",
		ToolCalls: make(map[int]*ToolCallState),
	}

	_, err := OpenAIStreamChunkToCoreEvents(coreToolChunk(0, "call_0", "", `{}`), ctx)
	if err != nil {
		t.Fatal(err)
	}
	finishReason := "tool_calls"
	_, err = OpenAIStreamChunkToCoreEvents(&OpenAIStreamChunk{
		Choices: []OpenAIStreamChoice{{FinishReason: &finishReason}},
	}, ctx)
	if err == nil || !strings.Contains(err.Error(), "missing tool name") {
		t.Fatalf("error = %v, want missing tool name", err)
	}
}

// TestStreamChunk_SingleToolMultiChunkArgs reproduces the exact bug that caused
// "Content block not found": a single tool call whose arguments arrive across
// MULTIPLE chunks. The old code closed the content block on every tool chunk,
// so the 2nd args chunk's delta referenced a stale/closed block index.
func TestStreamChunk_SingleToolMultiChunkArgs(t *testing.T) {
	ctx := &StreamContext{
		MessageID: "msg_test",
		Model:     "claude-opus-4.8",
		ToolCalls: make(map[int]*ToolCallState),
	}

	chunks := []*OpenAIStreamChunk{
		// chunk 1: id + name + first args fragment (typical OpenAI first delta)
		{Choices: []OpenAIStreamChoice{{Index: 0, Delta: OpenAIStreamDelta{
			ToolCalls: []OpenAIToolCallDelta{
				{Index: 0, ID: "call_1", Function: &OpenAIToolFunctionDelta{Name: "Read", Arguments: `{"file_`}},
			},
		}}}},
		// chunk 2: more args
		{Choices: []OpenAIStreamChoice{{Index: 0, Delta: OpenAIStreamDelta{
			ToolCalls: []OpenAIToolCallDelta{
				{Index: 0, Function: &OpenAIToolFunctionDelta{Arguments: `path":"/tmp/`}},
			},
		}}}},
		// chunk 3: final args
		{Choices: []OpenAIStreamChoice{{Index: 0, Delta: OpenAIStreamDelta{
			ToolCalls: []OpenAIToolCallDelta{
				{Index: 0, Function: &OpenAIToolFunctionDelta{Arguments: `x.go"}`}},
			},
		}}}},
	}

	var allEvents []SSEEvent
	for _, c := range chunks {
		evs, err := TranslateStreamChunk(c, ctx)
		if err != nil {
			t.Fatal(err)
		}
		allEvents = append(allEvents, evs...)
	}
	// finish
	finish := "tool_calls"
	evs, _ := TranslateStreamChunk(&OpenAIStreamChunk{
		Choices: []OpenAIStreamChoice{{Index: 0, FinishReason: &finish}},
	}, ctx)
	allEvents = append(allEvents, evs...)

	// --- Assertions ---
	var blockStartIdx = -99
	var deltaIndices []int
	var blockStopIdx = -99
	startCount, stopCount := 0, 0
	var accumulatedArgs string

	for _, ev := range allEvents {
		switch ev.Type {
		case "content_block_start":
			startCount++
			if ev.Index != nil {
				blockStartIdx = *ev.Index
			}
		case "content_block_delta":
			if ev.Index != nil {
				deltaIndices = append(deltaIndices, *ev.Index)
			}
			if ev.Delta != nil && ev.Delta.Type == "input_json_delta" {
				accumulatedArgs += ev.Delta.PartialJSON
			}
		case "content_block_stop":
			stopCount++
			if ev.Index != nil {
				blockStopIdx = *ev.Index
			}
		}
	}

	// Exactly ONE block_start and ONE block_stop
	if startCount != 1 {
		t.Errorf("expected exactly 1 content_block_start, got %d", startCount)
	}
	if stopCount != 1 {
		t.Errorf("expected exactly 1 content_block_stop, got %d", stopCount)
	}

	// CRITICAL: every delta index MUST match the block_start index
	for _, di := range deltaIndices {
		if di != blockStartIdx {
			t.Errorf("delta index %d != block_start index %d (Content block not found bug!)", di, blockStartIdx)
		}
	}
	if blockStopIdx != blockStartIdx {
		t.Errorf("block_stop index %d != block_start index %d", blockStopIdx, blockStartIdx)
	}

	// All args fragments must accumulate into valid JSON
	if accumulatedArgs != `{"file_path":"/tmp/x.go"}` {
		t.Errorf("args not accumulated correctly: %q", accumulatedArgs)
	}
	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(accumulatedArgs), &parsed); err != nil {
		t.Errorf("accumulated args not valid JSON: %v", err)
	}
	if parsed["file_path"] != "/tmp/x.go" {
		t.Errorf("file_path lost: got %v", parsed["file_path"])
	}
}

// TestStreamChunk_TwoToolsMultiChunk verifies two sequential tool calls, each
// with args spread across chunks, get distinct content block indices.
func TestStreamChunk_TwoToolsMultiChunk(t *testing.T) {
	ctx := &StreamContext{
		MessageID: "msg_test",
		Model:     "claude-opus-4.8",
		ToolCalls: make(map[int]*ToolCallState),
	}

	chunks := []*OpenAIStreamChunk{
		// tool 0: name + partial args
		{Choices: []OpenAIStreamChoice{{Index: 0, Delta: OpenAIStreamDelta{
			ToolCalls: []OpenAIToolCallDelta{
				{Index: 0, ID: "call_0", Function: &OpenAIToolFunctionDelta{Name: "Read", Arguments: `{"p":`}},
			},
		}}}},
		// tool 0: more args
		{Choices: []OpenAIStreamChoice{{Index: 0, Delta: OpenAIStreamDelta{
			ToolCalls: []OpenAIToolCallDelta{
				{Index: 0, Function: &OpenAIToolFunctionDelta{Arguments: `"a"}`}},
			},
		}}}},
		// tool 1: name + partial args (different index → new block)
		{Choices: []OpenAIStreamChoice{{Index: 0, Delta: OpenAIStreamDelta{
			ToolCalls: []OpenAIToolCallDelta{
				{Index: 1, ID: "call_1", Function: &OpenAIToolFunctionDelta{Name: "Grep", Arguments: `{"q":`}},
			},
		}}}},
		// tool 1: more args
		{Choices: []OpenAIStreamChoice{{Index: 0, Delta: OpenAIStreamDelta{
			ToolCalls: []OpenAIToolCallDelta{
				{Index: 1, Function: &OpenAIToolFunctionDelta{Arguments: `"b"}`}},
			},
		}}}},
	}

	var allEvents []SSEEvent
	for _, c := range chunks {
		evs, _ := TranslateStreamChunk(c, ctx)
		allEvents = append(allEvents, evs...)
	}
	finish := "tool_calls"
	evs, _ := TranslateStreamChunk(&OpenAIStreamChunk{
		Choices: []OpenAIStreamChoice{{Index: 0, FinishReason: &finish}},
	}, ctx)
	allEvents = append(allEvents, evs...)

	// Map block index → accumulated args
	startCount, stopCount := 0, 0
	argsByIndex := map[int]string{}
	startIndices := map[int]bool{}

	for _, ev := range allEvents {
		switch ev.Type {
		case "content_block_start":
			startCount++
			if ev.Index != nil {
				startIndices[*ev.Index] = true
			}
		case "content_block_delta":
			if ev.Index != nil && ev.Delta != nil && ev.Delta.Type == "input_json_delta" {
				argsByIndex[*ev.Index] += ev.Delta.PartialJSON
			}
		case "content_block_stop":
			stopCount++
		}
	}

	// Two distinct blocks, two stops
	if startCount != 2 {
		t.Errorf("expected 2 content_block_start, got %d", startCount)
	}
	if stopCount != 2 {
		t.Errorf("expected 2 content_block_stop, got %d", stopCount)
	}
	if len(startIndices) != 2 {
		t.Errorf("expected 2 distinct block indices, got %d: %v", len(startIndices), startIndices)
	}

	// Every delta index must correspond to a started block
	for idx := range argsByIndex {
		if !startIndices[idx] {
			t.Errorf("delta on index %d but no block_start for it", idx)
		}
	}
}

func TestCoreStream_TextDelta(t *testing.T) {
	ctx := &StreamContext{
		MessageID: "msg_core",
		Model:     "claude-opus-4.8",
		ToolCalls: make(map[int]*ToolCallState),
	}
	chunk := &OpenAIStreamChunk{
		Choices: []OpenAIStreamChoice{{
			Index: 0,
			Delta: OpenAIStreamDelta{Content: "hello"},
		}},
	}

	events, err := OpenAIStreamChunkToCoreEvents(chunk, ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 3 {
		t.Fatalf("expected message_start, block_start, delta; got %+v", events)
	}
	if events[0].Type != "message_start" || events[0].ID != "msg_core" || events[0].Model != "claude-opus-4.8" {
		t.Fatalf("message_start mismatch: %+v", events[0])
	}
	if events[1].Type != "content_block_start" || events[1].ContentBlock.Type != "text" {
		t.Fatalf("text block start mismatch: %+v", events[1])
	}
	if events[2].Type != "content_block_delta" || events[2].Text != "hello" {
		t.Fatalf("text delta mismatch: %+v", events[2])
	}
}

func TestCoreStream_DelayedToolName(t *testing.T) {
	ctx := &StreamContext{
		MessageID: "msg_core",
		Model:     "claude-opus-4.8",
		ToolCalls: make(map[int]*ToolCallState),
	}

	events, err := OpenAIStreamChunkToCoreEvents(&OpenAIStreamChunk{
		Choices: []OpenAIStreamChoice{{Index: 0, Delta: OpenAIStreamDelta{
			ToolCalls: []OpenAIToolCallDelta{{Index: 0, ID: "call_1"}},
		}}},
	}, ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if event.Type == "content_block_start" {
			t.Fatalf("should not start tool block before name arrives: %+v", events)
		}
	}

	events, err = OpenAIStreamChunkToCoreEvents(&OpenAIStreamChunk{
		Choices: []OpenAIStreamChoice{{Index: 0, Delta: OpenAIStreamDelta{
			ToolCalls: []OpenAIToolCallDelta{{
				Index:    0,
				Function: &OpenAIToolFunctionDelta{Name: "read", Arguments: `{"path":"a`},
			}},
		}}},
	}, ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if strings.HasPrefix(event.Type, "content_block_") {
			t.Fatalf("tool blocks must wait for finish: %+v", events)
		}
	}
	finishReason := "tool_calls"
	events, err = OpenAIStreamChunkToCoreEvents(&OpenAIStreamChunk{
		Choices: []OpenAIStreamChoice{{FinishReason: &finishReason}},
	}, ctx)
	if err != nil {
		t.Fatal(err)
	}
	var start *CoreStreamEvent
	var delta *CoreStreamEvent
	for i := range events {
		if events[i].Type == "content_block_start" {
			start = &events[i]
		}
		if events[i].Type == "content_block_delta" {
			delta = &events[i]
		}
	}
	if start == nil || start.ContentBlock.ToolUseID != "call_1" || start.ContentBlock.ToolName != "read" {
		t.Fatalf("tool start mismatch: %+v", events)
	}
	if delta == nil || delta.PartialJSON != `{"path":"a` || delta.Index != start.Index {
		t.Fatalf("tool delta mismatch: start=%+v delta=%+v", start, delta)
	}
}
