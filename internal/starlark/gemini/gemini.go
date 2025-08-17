// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package gemini

import (
	"encoding/base64"
	"fmt"
	"net/http"

	"go.astrophena.name/tools/internal/api/gemini"
	"go.astrophena.name/tools/internal/starlark/interpreter"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

// Module returns a Starlark module that exposes Gemini API.
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
		image              starlark.Bytes
		systemInstructions string
		unsafe             bool
	)

	if err := starlark.UnpackArgs(
		b.Name(), args, kwargs,
		"model", &model,
		"contents", &contentsList,
		"image?", &image,
		"system_instructions?", &systemInstructions,
		"unsafe?", &unsafe,
	); err != nil {
		return nil, err
	}

	var contents []*gemini.Content

	for i := range contentsList.Len() {
		partVal, ok := contentsList.Index(i).(starlark.Tuple)
		if !ok {
			return nil, fmt.Errorf("%s: contents[%d] is not a tuple", b.Name(), i)
		}

		if len(partVal) != 2 {
			return nil, fmt.Errorf("%s: contents[%d] must have exactly two elements: role and model", b.Name(), i)
		}

		roleStr, ok := partVal.Index(0).(starlark.String)
		if !ok {
			return nil, fmt.Errorf("%s: in contents[%d] role must be a string", b.Name(), i)
		}
		textStr, ok := partVal.Index(1).(starlark.String)
		if !ok {
			return nil, fmt.Errorf("%s: in contents[%d] text must be a string", b.Name(), i)
		}

		content := &gemini.Content{
			Parts: []*gemini.Part{
				{Text: string(textStr)},
			},
			Role: string(roleStr),
		}
		contents = append(contents, content)
	}

	if image.Len() > 0 {
		imageData := &gemini.Content{
			Parts: []*gemini.Part{
				{
					InlineData: &gemini.InlineData{
						MimeType: http.DetectContentType([]byte(image)),
						Data:     base64.StdEncoding.EncodeToString([]byte(image)),
					},
				},
			},
			Role: "user",
		}
		if len(contents) > 0 {
			contents = append(contents[:len(contents)-1], append([]*gemini.Content{imageData}, contents[len(contents)-1:]...)...)
		} else {
			contents = append(contents, imageData)
		}
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
