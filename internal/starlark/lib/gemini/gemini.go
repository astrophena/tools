// © 2024 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

// Package gemini contains a Starlark module that exposes Gemini API.
package gemini

import (
	"encoding/base64"
	"fmt"
	"net/http"

	"go.astrophena.name/tools/internal/api/google/gemini"
	"go.astrophena.name/tools/internal/starlark/interpreter"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

// Module returns a Starlark module that exposes Gemini API.
//
// This module provides a single function, generate_content, which uses the
// Gemini API to generate text, optionally with an image as context.
//
// It accepts the following keyword arguments:
//
//   - model (str): The name of the model to use for generation (e.g., "gemini-1.5-flash").
//   - contents (list of (str, str) tuples): A list of (role, text) tuples representing
//     the conversation history. Valid roles are typically "user" and "model".
//   - image (bytes, optional): The raw bytes of an image to include. The image is
//     inserted as a new part just before the last part of the 'contents'.
//     This is useful for multimodal prompts (e.g., asking a question about an image).
//   - system_instructions (str, optional): System instructions to guide Gemini's response.
//   - unsafe (bool, optional): If set to true, disables all safety settings for the
//     content generation, allowing potentially harmful content. Use with caution.
//
// For example, for a text-only prompt:
//
//	responses = gemini.generate_content(
//	    model="gemini-1.5-flash",
//	    contents=[
//	        ("user", "Once upon a time,"),
//	        ("model", "there was a brave knight."),
//	        ("user", "What happened next?")
//	    ],
//	    system_instructions="You are a creative story writer. Write a short story based on the provided prompt."
//	)
//
// To ask a question about an image:
//
//	image_data = ... # read image file content as bytes
//	responses = gemini.generate_content(
//	    model="gemini-1.5-flash",
//	    contents=[
//	        ("user", "Describe this image in detail.")
//	    ],
//	    image=image_data
//	)
//
// The responses variable will contain a list of generated responses, where each response
// is a list of strings representing the parts of the generated content.
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
