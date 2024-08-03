// Package gemini contains a Starlark module that exposes Gemini API.
package gemini

import (
	"context"
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
// It accepts two keyword arguments:
//
//   - contents (list of strings): The text to be provided to Gemini for generation.
//   - system (dict, optional): System instructions to guide Gemini's response.
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
		contents *starlark.List
		system   *starlark.Dict
	)
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "contents", &contents, "system", &system); err != nil {
		return nil, err
	}

	var (
		parts      []*gemini.Part
		systemPart *gemini.Part
	)

	for i := range contents.Len() {
		partVal, ok := contents.Index(i).(starlark.String)
		if !ok {
			return starlark.None, fmt.Errorf("%s: contents[%d] is not a string", b.Name(), i)
		}
		parts = append(parts, &gemini.Part{
			Text: string(partVal),
		})
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

	resp, err := m.c.GenerateContent(ctx, gemini.GenerateContentParams{
		Contents: []*gemini.Content{
			{
				Parts: parts,
			},
		},
		SystemInstruction: &gemini.Content{
			Parts: []*gemini.Part{systemPart},
		},
	})
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
