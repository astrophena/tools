// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

package llm

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"time"

	"go.astrophena.name/tools/internal/api/llm"
	"go.astrophena.name/tools/internal/starlark/interpreter"

	"go.starlark.net/starlark"
	"go.starlark.net/starlarkstruct"
)

// Module returns a Starlark module that exposes a minimal Responses API call.
func Module(client *llm.Client, usagePath string) *starlarkstruct.Module {
	usage, err := newUsageStore(usagePath)
	if err != nil {
		usage = nil
	}
	m := &module{client: client, usage: usage}
	return &starlarkstruct.Module{
		Name: "llm",
		Members: starlark.StringDict{
			"generate": starlark.NewBuiltin("llm.generate", m.generate),
			"usage":    starlark.NewBuiltin("llm.usage", m.getUsage),
		},
	}
}

type module struct {
	client *llm.Client
	usage  *usageStore
}

func (m *module) generate(thread *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	ctx := interpreter.Context(thread)

	if m.client == nil {
		return starlark.None, fmt.Errorf("%s: LLM API is not available", b.Name())
	}

	var (
		model        string
		contentsList *starlark.List
		instructions string
		usageKey     string
		image        starlark.Bytes
	)

	if err := starlark.UnpackArgs(
		b.Name(), args, kwargs,
		"model", &model,
		"contents", &contentsList,
		"usage_key", &usageKey,
		"image?", &image,
		"instructions?", &instructions,
	); err != nil {
		return nil, err
	}

	messages := make([]llm.Message, 0, contentsList.Len()+1)
	for i := range contentsList.Len() {
		item, ok := contentsList.Index(i).(starlark.Tuple)
		if !ok {
			return nil, fmt.Errorf("%s: contents[%d] is not a tuple", b.Name(), i)
		}
		if len(item) != 2 {
			return nil, fmt.Errorf("%s: contents[%d] must have exactly two elements: role and content", b.Name(), i)
		}
		role, ok := item.Index(0).(starlark.String)
		if !ok {
			return nil, fmt.Errorf("%s: in contents[%d] role must be a string", b.Name(), i)
		}
		content, ok := item.Index(1).(starlark.String)
		if !ok {
			return nil, fmt.Errorf("%s: in contents[%d] content must be a string", b.Name(), i)
		}
		messages = append(messages, llm.Message{Role: string(role), Content: []llm.ContentPart{{Type: "input_text", Text: string(content)}}})
	}
	if image.Len() > 0 {
		mime := http.DetectContentType([]byte(image))
		imageURL := "data:" + mime + ";base64," + base64.StdEncoding.EncodeToString([]byte(image))
		messages = append(messages, llm.Message{Role: "user", Content: []llm.ContentPart{{Type: "input_image", ImageURL: imageURL}}})
	}

	resp, err := m.client.CreateResponse(ctx, llm.ResponseParams{Model: model, Input: messages, Instructions: instructions})
	if err != nil {
		return starlark.None, fmt.Errorf("%s: failed to generate text: %w", b.Name(), err)
	}
	if m.usage != nil {
		if err := m.usage.add(usageKey, time.Now(), resp.Usage.InputTokens, resp.Usage.OutputTokens); err != nil {
			return starlark.None, fmt.Errorf("%s: failed to persist usage: %w", b.Name(), err)
		}
	}

	return starlark.String(resp.OutputText), nil
}

func (m *module) getUsage(_ *starlark.Thread, b *starlark.Builtin, args starlark.Tuple, kwargs []starlark.Tuple) (starlark.Value, error) {
	var (
		key  string
		date string
	)
	if err := starlark.UnpackArgs(b.Name(), args, kwargs, "key", &key, "date?", &date); err != nil {
		return nil, err
	}
	if m.usage == nil {
		return starlarkstruct.FromStringDict(starlark.String("usage"), starlark.StringDict{}), nil
	}
	u := m.usage.get(key, date)
	return starlarkstruct.FromStringDict(starlark.String("usage"), starlark.StringDict{
		"input_tokens":  starlark.MakeInt64(u.InputTokens),
		"output_tokens": starlark.MakeInt64(u.OutputTokens),
		"total_tokens":  starlark.MakeInt64(u.InputTokens + u.OutputTokens),
	}), nil
}
