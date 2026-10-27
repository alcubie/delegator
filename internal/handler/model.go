package handler

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strings"

	acp "github.com/coder/acp-go-sdk"
)

// ErrNoModelOption reports an agent that advertises no ACP model config
// option. Callers wrap it with the model they could not set.
var ErrNoModelOption = errors.New("agent does not expose an ACP model config option")

// Model returns the model selected during session setup, or empty when the
// agent did not report one. It does not track switches during a prompt.
func (s *Session) Model() string { return s.model }

// Models launches the agent in cwd and returns the model IDs it advertises,
// then terminates it. Nothing is prompted and no tool is permitted, so the
// agent only reports what it offers. An agent that advertises no model
// selector returns ErrNoModelOption.
func Models(ctx context.Context, name string, argv []string, cwd string) ([]string, error) {
	session, configOptions, err := newSession(ctx, name, argv, Deny(), cwd, SessionOptions{}, io.Discard)
	if err != nil {
		return nil, err
	}
	// Ending the probe disconnects the agent, which the SDK reports on the
	// standard logger. The person at the terminal asked about models, not
	// about a connection that closed as intended.
	session.conn.SetLogger(slog.New(slog.DiscardHandler))
	defer session.Close()
	selectOption := modelOption(configOptions)
	if selectOption == nil {
		return nil, ErrNoModelOption
	}
	return modelValues(selectOption), nil
}

// setModel uses the agent's advertised selector and value IDs, including
// grouped model lists. Never silently fall back when a model was requested.
func (s *Session) setModel(ctx context.Context, model string, options []acp.SessionConfigOption) error {
	model = strings.TrimSpace(model)
	useDefault := model == "" || model == "null"
	selectOption := modelOption(options)
	if selectOption == nil {
		if useDefault {
			return nil
		}
		return fmt.Errorf("cannot set model %q: %w", model, ErrNoModelOption)
	}
	if useDefault {
		s.model = string(selectOption.CurrentValue)
		return nil
	}
	available := modelValues(selectOption)
	if !slices.Contains(available, model) {
		return fmt.Errorf("model %q is unavailable; available model IDs: %s", model, strings.Join(available, ", "))
	}
	// Drain notifications while the agent applies the setting, just as when
	// loading history, so a burst cannot block the response.
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

// modelOption returns the agent's model selector: the select option of the
// model category, or the one with the ID "model" when the agent sends no
// category. Nil means the agent advertises no model selector.
func modelOption(options []acp.SessionConfigOption) *acp.SessionConfigOptionSelect {
	for _, option := range options {
		selectOption := option.Select
		if selectOption == nil {
			continue
		}
		if selectOption.Category != nil {
			if *selectOption.Category == acp.SessionConfigOptionCategoryModel {
				return selectOption
			}
			continue
		}
		if selectOption.Id == "model" {
			return selectOption
		}
	}
	return nil
}

// modelValues returns the value IDs a selector advertises, flattening groups.
func modelValues(selectOption *acp.SessionConfigOptionSelect) []string {
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
	return available
}
