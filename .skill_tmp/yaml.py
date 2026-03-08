"""Minimal PyYAML-compatible shim for skill scaffolding scripts in offline env."""

class YAMLError(Exception):
    """Compatibility exception matching PyYAML API."""


def _parse_scalar(value):
    if value in {"true", "True"}:
        return True
    if value in {"false", "False"}:
        return False
    if value in {"null", "Null", "~", ""}:
        return None
    if (value.startswith('"') and value.endswith('"')) or (
        value.startswith("'") and value.endswith("'")
    ):
        return value[1:-1]
    return value


def safe_load(text):
    if text is None:
        return None
    if not isinstance(text, str):
        raise YAMLError("expected string input")

    root = {}
    stack = [(0, root)]

    for raw in text.splitlines():
        if not raw.strip() or raw.lstrip().startswith("#"):
            continue

        indent = len(raw) - len(raw.lstrip(" "))
        line = raw.strip()

        if ":" not in line:
            raise YAMLError(f"invalid line: {raw}")

        key, raw_value = line.split(":", 1)
        key = key.strip()
        raw_value = raw_value.strip()

        if not key:
            raise YAMLError(f"invalid key in line: {raw}")

        while stack and indent < stack[-1][0]:
            stack.pop()
        if not stack:
            raise YAMLError("invalid indentation")

        current = stack[-1][1]

        if raw_value == "":
            new_obj = {}
            current[key] = new_obj
            stack.append((indent + 2, new_obj))
        else:
            current[key] = _parse_scalar(raw_value)

    return root
