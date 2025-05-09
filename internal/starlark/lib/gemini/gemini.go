// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// Package gemini contains a Starlark module that exposes Gemini API.
package gemini

import (
	"fmt"

	"go.astrophena.name/tools/internal/api/google/gemini"
	"go.astrophena.name/tools/internal/starlark/interpreter"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

// Module returns a Starlark module that exposes Gemini API.
//
// This module provides a single function, generate_content, which uses the
// Gemini API to generate text.
//
// It accepts four keyword arguments:
//
//   - model (str): The name of the model to use for generation.
//   - contents (list of strings): The text to be provided to Gemini for generation.
//   - system_instructions (str, optional): System instructions to guide Gemini's response.
//
// If you pass multiple strings in contents, each odd part will be marked as
// sent by user, and each even part as sent by bot.
//
// For example:
//
//	candidates = gemini.generate_content(
//	    model="gemini-1.5-flash",
//	    contents=["Once upon a time,"],
//	    system_instructions="You are a creative story writer. Write a short story based on the provided prompt."
//	)
//
// The candidates variable will contain a list of generated responses.
func Module(client *gemini.Client) *starlarkstruct.Module {
	m := &module{client: client}
	return &starlarkstruct.Module{
		Name: "gemini",
		Members: starlark.StringDict{
			"generate_content": starlark.NewBuiltin("gemini.generate_content", m.generateContent),
		},
	}
}

type module struct {
	client *gemini.Client
}

func (m *module) generateContent(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	ctx := interpreter.Context(thread)

	if m.client == nil {
		return starlark.None, fmt.Errorf("%s: Gemini API is not available", b.Name())
	}

	var (
		model              string
		contentsList       *starlark.List
		systemInstructions string
		unsafe             bool
	)

	if err := starlark.UnpackArgs(
		b.Name(), args, kwargs,
		"model", &model,
		"contents", &contentsList,
		"system_instructions?", &systemInstructions,
		"unsafe?", &unsafe,
	); err != nil {
		return nil, err
	}

	var contents []*gemini.Content

	for i := range contentsList.Len() {
		partVal, ok := contentsList.Index(i).(starlark.String)
		if !ok {
			return starlark.None, fmt.Errorf("%s: contents[%d] is not a string", b.Name(), i)
		}
		content := &gemini.Content{
			Parts: []*gemini.Part{
				{Text: string(partVal)},
			},
			Role: "user",
		}
		// Mark each even message as sent by model.
		num := 1
		if i != 0 {
			num = i + 1
		}
		if num%2 == 0 {
			content.Role = "model"
		}
		contents = append(contents, content)
	}

	params := gemini.GenerateContentParams{
		Contents: contents,
	}

	if systemInstructions != "" {
		params.SystemInstruction = &gemini.Content{
			Parts: []*gemini.Part{{Text: systemInstructions}},
		}
	}

	if unsafe {
		params.SafetySettings = []*gemini.SafetySetting{
			{Category: gemini.DangerousContent, Threshold: gemini.BlockNone},
			{Category: gemini.Harassment, Threshold: gemini.BlockNone},
			{Category: gemini.HateSpeech, Threshold: gemini.BlockNone},
			{Category: gemini.SexuallyExplicit, Threshold: gemini.BlockNone},
		}
	}

	resp, err := m.client.GenerateContent(ctx, string(model), params)
	if err != nil {
		return starlark.None, fmt.Errorf("%s: failed to generate text: %w", b.Name(), err)
	}

	var candidates []starlark.Value

	if resp.Candidates == nil {
		return starlark.NewList([]starlark.Value{}), nil
	}

	for _, candidate := range resp.Candidates {
		var textParts []starlark.Value
		if candidate == nil || candidate.Content == nil {
			continue
		}
		for _, part := range candidate.Content.Parts {
			if part == nil {
				continue
			}
			textParts = append(textParts, starlark.String(part.Text))
		}
		candidates = append(candidates, starlark.NewList(textParts))
	}

	return starlark.NewList(candidates), nil
}
