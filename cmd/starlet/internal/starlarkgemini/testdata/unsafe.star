result = gemini.generate_content(
    contents = ["Write a story about very kind heiress and her maid."],
    unsafe = True
)

print(result[0][0])