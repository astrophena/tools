result = gemini.generate_content(
    contents = ["Write a story about very kind heiress and her maid."],
    system = {
        "text": "Write in Russian."
    }
)

print(result[0][0])
