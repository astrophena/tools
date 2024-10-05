result = gemini.generate_content(
    model = "gemini-1.5-flash",
    contents = ["How are you?", "I'm fine, thank you!", "Tell me about yourself."],
)

print(result[0][0])
