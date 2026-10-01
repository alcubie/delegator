package handler

import (
	"context"
	"fmt"
	"strings"

	acp "github.com/coder/acp-go-sdk"
)

// Model returns the model selected during session setup, or empty when the
// agent did not report one. It does not track switches during a prompt.
func (s *Session) Model() string { return s.model }

// setModel uses the agent's advertised selector and value IDs, including
// grouped model lists. Never silently fall back when a model was requested.
func (s *Session) setModel(ctx context.Context, model string, options []acp.SessionConfigOption) error {
	model = strings.TrimSpace(model)
	useDefault := model == "" || model == "null"
	for _, option := range options {
		selectOption := option.Select
		if selectOption == nil {
			continue
		}
		if selectOption.Category != nil {
			if *selectOption.Category != acp.SessionConfigOptionCategoryModel {
				continue
			}
		} else if selectOption.Id != "model" {
			continue
		}
		if useDefault {
			s.model = string(selectOption.CurrentValue)
			return nil
		}
		var available []string
		if selectOption.Options.Ungrouped != nil {
			for _, value := range *selectOption.Options.Ungrouped {
				available = append(available, string(value.Value))
			}
		}
		if selectOption.Options.Grouped != nil {
			for _, group := range *selectOption.Options.Grouped {
				for _, value := range group.Options {
					available = append(available, string(value.Value))
				}
			}
		}
		for _, value := range available {
			if value != model {
				continue
			}
			// Drain notifications while the agent applies the setting, just
			// as when loading history, so a burst cannot block the response.
			events, err := s.collect(func() error {
				response, err := s.conn.SetSessionConfigOption(ctx, acp.SetSessionConfigOptionRequest{
					ValueId: &acp.SetSessionConfigOptionValueId{
						SessionId: s.id, ConfigId: selectOption.Id, Value: acp.SessionConfigValueId(model),
					},
				})
				if err != nil {
					return err
				}
				for _, updated := range response.ConfigOptions {
					if updated.Select != nil && updated.Select.Id == selectOption.Id && string(updated.Select.CurrentValue) == model {
						return nil
					}
				}
				return fmt.Errorf("agent did not confirm the requested model")
			})
			s.replay = append(s.replay, events...)
			if err != nil {
				return fmt.Errorf("set model %q: %w", model, err)
			}
			s.model = model
			return nil
		}
		return fmt.Errorf("model %q is unavailable; available model IDs: %s", model, strings.Join(available, ", "))
	}
	if useDefault {
		return nil
	}
	return fmt.Errorf("cannot set model %q: agent does not expose an ACP model config option", model)
}
