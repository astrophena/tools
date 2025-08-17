# © 2024 Ilya Mateyko. All rights reserved.
# Use of this source code is governed by the ISC
# license that can be found in the LICENSE.md file.

responses = gemini.generate_content(
    model="gemini-1.5-flash",
    contents=[("user", "Write a story about very kind heiress and her maid.")],
    system_instructions="Write in Russian.",
)

print(responses[0][0])
