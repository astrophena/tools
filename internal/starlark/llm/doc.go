// © 2026 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

/*
Package llm contains a Starlark module for generating text with the configured
LLM provider.

The module sends chat-style contents, optional image bytes, and optional
instructions to the configured OpenAI Responses-compatible /responses endpoint.
It returns the response's text output as a single string and records token usage
under a caller-provided usage key.

This is useful with the OpenAI API and with other AI providers that expose a
compatible endpoint, such as OpenRouter.

This module provides two functions: generate and usage.

usage(key, date?) returns cumulative token usage for the key.
If date is provided (YYYY-MM-DD), it returns usage only for that exact UTC day.

generate accepts the following keyword arguments:

  - model (str): Model name to generate with.
  - contents (list of (str, str) tuples): A list of (role, content) messages.
  - usage_key (str): Arbitrary key used to accumulate persistent token usage stats.
  - image (bytes, optional): Optional raw image bytes to upload as input_image.
  - instructions (str, optional): Optional high-level instructions for the model.

For example:

	text = llm.generate(
	    model="gpt-4.1-mini",
	    contents=[
	        ("user", "Describe this image briefly.")
	    ],
	    image=files.read("cat.jpg"),
	    usage_key="chat:123",
	    instructions="Be concise."
	)

The return value is a single string from the response output message text.
*/
package llm

import (
	_ "embed"
	"sync"

	"go.astrophena.name/tools/internal/starlark/internal"
)

//go:embed doc.go
var doc []byte

var Documentation = sync.OnceValue(func() string {
	return internal.ParseDocComment(doc)
})
