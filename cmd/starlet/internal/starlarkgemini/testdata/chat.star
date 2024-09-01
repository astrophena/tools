result = gemini.generate_content(
    contents = ["How are you?", "I'm fine, thank you!", "Tell me about yourself."],
)

print(result[0][0])
