# © 2024 Ilya Mateyko. All rights reserved.
# Use of this source code is governed by the ISC
# license that can be found in the LICENSE.md file.

responses = gemini.generate_content(
    model="gemini-1.5-flash",
    contents=[
        ("user", "How are you?"),
        ("model", "I'm fine, thank you!"),
        ("user", "Tell me about yourself."),
    ],
)

print(responses[0][0])
