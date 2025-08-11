// © 2025 Ilya Mateyko. All rights reserved.
// Use of this source code is governed by the ISC
// license that can be found in the LICENSE.md file.

/*
Package gemini contains a Starlark module that exposes Gemini API.

This module provides a single function, generate_content, which uses the
Gemini API to generate text, optionally with an image as context.

It accepts the following keyword arguments:

  - model (str): The name of the model to use for generation (e.g., "gemini-1.5-flash").
  - contents (list of (str, str) tuples): A list of (role, text) tuples representing
    the conversation history. Valid roles are typically "user" and "model".
  - image (bytes, optional): The raw bytes of an image to include. The image is
    inserted as a new part just before the last part of the 'contents'.
    This is useful for multimodal prompts (e.g., asking a question about an image).
  - system_instructions (str, optional): System instructions to guide Gemini's response.
  - unsafe (bool, optional): If set to true, disables all safety settings for the
    content generation, allowing potentially harmful content. Use with caution.

For example, for a text-only prompt:

	responses = gemini.generate_content(
	    model="gemini-1.5-flash",
	    contents=[
	        ("user", "Once upon a time,"),
	        ("model", "there was a brave knight."),
	        ("user", "What happened next?")
	    ],
	    system_instructions="You are a creative story writer. Write a short story based on the provided prompt."
	)

To ask a question about an image:

	image_data = ... # read image file content as bytes
	responses = gemini.generate_content(
	    model="gemini-1.5-flash",
	    contents=[
	        ("user", "Describe this image in detail.")
	    ],
	    image=image_data
	)

The responses variable will contain a list of generated responses, where each response
is a list of strings representing the parts of the generated content.
*/
package gemini

import (
	_ "embed"
	"sync"

	"go.astrophena.name/tools/internal/starlark/lib/internal"
)

//go:embed doc.go
var doc []byte

var Documentation = sync.OnceValue(func() string {
	return internal.ParseDocComment(doc)
})
