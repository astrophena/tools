# © 2024 Ilya Mateyko. All rights reserved.
# Use of this source code is governed by the ISC
# license that can be found in the LICENSE.md file.

result = gemini.generate_content(
    model = "gemini-1.5-flash",
    contents = ["How are you?", "I'm fine, thank you!", "Tell me about yourself."],
)

print(result[0][0])
