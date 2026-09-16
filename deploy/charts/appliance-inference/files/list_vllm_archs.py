import ast
import json
from importlib.util import find_spec
from pathlib import Path


def string_keys(node, tables):
    if isinstance(node, ast.Name) and node.id in tables:
        return list(tables[node.id])
    if not isinstance(node, ast.Dict):
        return []
    keys = []
    for key, value in zip(node.keys, node.values):
        if key is None:
            keys.extend(string_keys(value, tables))
        elif isinstance(key, ast.Constant) and isinstance(key.value, str):
            keys.append(key.value)
    return keys


spec = find_spec("vllm")
if spec is None or not spec.submodule_search_locations:
    raise SystemExit("installed vLLM package was not found")
registry = Path(spec.submodule_search_locations[0]) / "model_executor" / "models" / "registry.py"
if not registry.is_file():
    raise SystemExit("installed vLLM ModelRegistry source was not found")
module = ast.parse(registry.read_text(encoding="utf-8"), filename=str(registry))
tables = {}
for node in module.body:
    if isinstance(node, ast.Assign) and len(node.targets) == 1 and isinstance(node.targets[0], ast.Name):
        name = node.targets[0].id
        if name.endswith("_MODELS"):
            tables[name] = string_keys(node.value, tables)
architectures = tables.get("_VLLM_MODELS") or tables.get("_TEXT_GENERATION_MODELS") or []
if not architectures:
    raise SystemExit("installed vLLM model architecture table was empty or unrecognized")
print(json.dumps(sorted(set(architectures))))
