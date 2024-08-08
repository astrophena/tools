// Package gemini contains a Starlark module that exposes Gemini API.
package gemini

import (
	"context"
	"encoding/json"
	"fmt"

	"go.astrophena.name/tools/internal/api/google/gemini"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

// Module returns a Starlark module that exposes Gemini API.
//
// This module provides a single function, generate_content, which uses the
// Gemini API to generate text.
//
// It accepts three keyword arguments:
//
//   - contents (list of strings): The text to be provided to Gemini for generation.
//   - system (dict, optional): System instructions to guide Gemini's response.
//   - dump_request (bool, False by default): Whether to log request body for inspection.
//
// The system dictionary has a single key, text, which should contain a string
// representing the system instructions.
//
// For example:
//
//	result = gemini.generate_content(
//	    contents = ["Once upon a time,"],
//	    system = {
//	        "text": "You are a creative story writer. Write a short story based on the provided prompt."
//	    }
//	)
//
// The result variable will contain a list of candidates, where each candidate
// is a list of generated text parts.
//
// The system dictionary is optional and can be used to provide system
// instructions to guide Gemini's response.
//
// The system dictionary has a single key, text, which should contain a
// string representing the system instructions.
//
// For example, the following system instructions will tell Gemini to write a
// short story based on the provided prompt:
//
//	system = {
//	    "text": "You are a creative story writer. Write a short story based on the provided prompt."
//	}
func Module(client *gemini.Client) *starlarkstruct.Module {
	m := &module{c: client}
	return &starlarkstruct.Module{
		Name: "gemini",
		Members: starlark.StringDict{
			"generate_content": starlark.NewBuiltin("gemini.generate_content", m.generateContent),
		},
	}
}

type module struct {
	c *gemini.Client
}

func (m *module) generateContent(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	ctx, ok := thread.Local("context").(context.Context)
	if !ok {
		ctx = context.Background()
	}

	if m.c == nil {
		return starlark.None, fmt.Errorf("%s: Gemini API is not available", b.Name())
	}
	if len(args) > 0 {
		return starlark.None, fmt.Errorf("%s: unexpected positional arguments", b.Name())
	}
	var (
		contentsList *starlark.List
		system       *starlark.Dict
		dumpRequest  starlark.Bool
	)
	if err := starlark.UnpackArgs(
		b.Name(), args, kwargs,
		"contents", &contentsList,
		"system?", &system,
		"dump_request?", &dumpRequest,
	); err != nil {
		return nil, err
	}

	var (
		contents   []*gemini.Content
		systemPart *gemini.Part
	)

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

	if system != nil {
		systemTextVal, ok, err := system.Get(starlark.String("text"))
		if err != nil {
			return starlark.None, err
		}
		if !ok {
			return starlark.None, fmt.Errorf("%s: system.text is not a string", b.Name())
		}
		systemText, ok := systemTextVal.(starlark.String)
		if !ok {
			return starlark.None, fmt.Errorf("%s: system.text is not a string", b.Name())
		}
		systemPart = &gemini.Part{
			Text: string(systemText),
		}
	}

	params := gemini.GenerateContentParams{
		Contents: contents,
		SystemInstruction: &gemini.Content{
			Parts: []*gemini.Part{systemPart},
		},
	}

	if bool(dumpRequest) && thread.Print != nil {
		j, err := json.MarshalIndent(params, "", "  ")
		if err != nil {
			return starlark.None, err
		}
		thread.Print(thread, fmt.Sprintf("Gemini request body: %s", j))
	}

	resp, err := m.c.GenerateContent(ctx, params)
	if err != nil {
		return starlark.None, fmt.Errorf("%s: failed to generate text: %w", b.Name(), err)
	}

	var candidates []starlark.Value
	for _, candidate := range resp.Candidates {
		var textParts []starlark.Value
		for _, part := range candidate.Content.Parts {
			textParts = append(textParts, starlark.String(part.Text))
		}
		candidates = append(candidates, starlark.NewList(textParts))
	}

	return starlark.NewList(candidates), nil
}
