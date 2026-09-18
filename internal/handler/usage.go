package handler

import acp "github.com/coder/acp-go-sdk"

// Usage is the aggregate token usage an agent reports for one prompt. Input,
// output and total are required by ACP when Usage is present. The other
// categories are pointers because an agent can omit each one independently;
// a pointer to zero is a reported zero, while nil means it was not reported.
// Thought tokens are part of output tokens and remain a separate observation,
// not another category to add to output or total.
type Usage struct {
	InputTokens       int
	CachedWriteTokens *int
	CachedReadTokens  *int
	OutputTokens      int
	ThoughtTokens     *int
	TotalTokens       int
}

// promptUsage copies the unstable ACP shape into the stable value exposed by
// the handler. Nothing is inferred when an optional category is absent.
func promptUsage(u *acp.Usage) *Usage {
	if u == nil {
		return nil
	}
	return &Usage{
		InputTokens:       u.InputTokens,
		CachedWriteTokens: copyInt(u.CachedWriteTokens),
		CachedReadTokens:  copyInt(u.CachedReadTokens),
		OutputTokens:      u.OutputTokens,
		ThoughtTokens:     copyInt(u.ThoughtTokens),
		TotalTokens:       u.TotalTokens,
	}
}

func copyInt(value *int) *int {
	if value == nil {
		return nil
	}
	result := *value
	return &result
}
