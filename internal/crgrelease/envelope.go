package crgrelease

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// CallResult is the pinned release's `tools/call` result envelope.
//
// Two properties of it are easy to get wrong and are both observable:
//
//   - A tool failure is NOT a JSON-RPC error. It is a successful response
//     whose result carries `isError: true` and the message in one text block,
//     with no structured content. Clients that branch on JSON-RPC errors
//     would never see a validation failure from the real release.
//   - A success carries BOTH `structuredContent` (the tool's dict) and a
//     `content` text block holding the compact JSON rendering of that same
//     dict. Emitting only one of the two is a visible protocol difference.
type CallResult struct {
	Content           []ContentBlock  `json:"content"`
	StructuredContent json.RawMessage `json:"structuredContent,omitempty"`
	IsError           bool            `json:"isError"`
}

// ContentBlock is one MCP content block. The release emits only text blocks.
type ContentBlock struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// SuccessResult builds the success envelope for a tool's structured payload.
func SuccessResult(payload any) (CallResult, error) {
	encoded, err := compactJSON(payload)
	if err != nil {
		return CallResult{}, err
	}
	return CallResult{
		Content:           []ContentBlock{{Type: "text", Text: string(encoded)}},
		StructuredContent: encoded,
		IsError:           false,
	}, nil
}

// ErrorResult builds the failure envelope for a message.
func ErrorResult(message string) CallResult {
	return CallResult{
		Content: []ContentBlock{{Type: "text", Text: message}},
		IsError: true,
	}
}

// compactJSON renders a payload the way the release's transport does: compact
// separators, no HTML escaping. The rendering is part of the contract because
// it is the literal text a client reads out of the content block.
func compactJSON(payload any) (json.RawMessage, error) {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(payload); err != nil {
		return nil, fmt.Errorf("crgrelease: encode tool result: %w", err)
	}
	return json.RawMessage(bytes.TrimRight(buf.Bytes(), "\n")), nil
}

// ToolExceptionResult is the envelope the release produces when an exception
// escapes a tool implementation: a uniform `Error calling tool '<name>': `
// prefix, the exception's message, and NO structured content.
//
// It is distinct from a tool's in-band error payload (a successful result
// carrying `status: "error"`), and the distinction is observable: a bound
// violation such as `max_results: 0` escapes — the release validates those
// bounds BEFORE its try block — so a handler that wrapped it in an in-band
// error payload would differ on both `isError` and `structuredContent`.
func ToolExceptionResult(tool string, err error) CallResult {
	return ErrorResult(fmt.Sprintf("Error calling tool '%s': %s", tool, err.Error()))
}
