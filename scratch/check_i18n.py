import json

def stats(filepath):
    with open(filepath, 'r', encoding='utf-8') as f:
        data = json.load(f)
    print(f"{filepath}: {len(data)} keys")

stats("web/i18n/en.json")
stats("web/i18n/en.fixed.json")
