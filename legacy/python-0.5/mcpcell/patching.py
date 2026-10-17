"""Edit one MCP entry, preserving unrelated content; never rewrite whole JSON."""
from __future__ import annotations
import copy
import json
from .discovery import ConfigError, jsonc, parse
from .storage import Conflict


def get_at(obj, keys):
    for k in keys: obj = obj[k]
    return obj


def span(text: str, keys: list[str]) -> tuple[int, int]:
    clean = jsonc(text)
    decoder = json.JSONDecoder()
    def skip(i):
        while i < len(clean) and clean[i].isspace(): i += 1
        return i
    def walk(start, remaining):
        start = skip(start)
        if not remaining:
            _, end = decoder.raw_decode(clean, start)
            return start, end
        if clean[start] != '{': raise Conflict('目标路径不是配置对象')
        i = skip(start + 1)
        while i < len(clean) and clean[i] != '}':
            key, i = decoder.raw_decode(clean, i)
            i = skip(i)
            if clean[i] != ':': raise Conflict('配置语法不支持')
            value_start = skip(i + 1)
            if key == remaining[0]: return walk(value_start, remaining[1:])
            _, i = decoder.raw_decode(clean, value_start)
            i = skip(i)
            if clean[i] == ',': i = skip(i + 1)
        raise Conflict('目标配置已不存在')
    return walk(0, keys)


def replace_entry(raw: bytes, fmt: str, keys: list[str], expected: dict, replacement: dict,
                  restore_fragment: str | None = None) -> bytes:
    obj = parse(raw, fmt)
    try: current = get_at(obj, keys)
    except (KeyError, TypeError): raise Conflict('配置项已被删除或移动') from None
    if current != expected: raise Conflict('这条 MCP 已被外部修改，未覆盖；请先核对')
    text = raw.decode('utf-8-sig')
    if fmt == 'toml':
        try: import tomlkit
        except ImportError: raise Conflict('Codex 接入需要 tomlkit：请先安装 requirements.txt') from None
        doc = tomlkit.parse(text)
        target = get_at(doc, keys)
        # Change only changed fields; other tables/comments/order stay untouched.
        for k in set(expected) - set(replacement): del target[k]
        for k, v in replacement.items():
            if k not in expected or expected[k] != v: target[k] = v
        output = tomlkit.dumps(doc)
    else:
        start, end = span(text, keys)
        if restore_fragment is not None:
            output = text[:start] + restore_fragment + text[end:]
        else:
            # Keep comments and original whitespace around unchanged properties.
            edits = []
            absent = []
            if set(expected) - set(replacement):
                raise Conflict('迁移不会自动删除客户端字段')
            for k, v in replacement.items():
                if k in expected:
                    if expected[k] != v:
                        a, b = span(text, keys + [k]); edits.append((a,b,json.dumps(v,ensure_ascii=False)))
                else: absent.append((k,v))
            if absent:
                # Insert before existing members: trailing commas and comments at
                # the end of a JSONC object need not be guessed or rewritten.
                ins = '\n' + ',\n'.join('    '+json.dumps(k)+': '+json.dumps(v,ensure_ascii=False) for k,v in absent)
                ins += (',' if expected else '') + '\n'
                edits.append((start+1,start+1,ins))
            output = text
            for a,b,value in sorted(edits,reverse=True): output = output[:a]+value+output[b:]
    result = (b'\xef\xbb\xbf' if raw.startswith(b'\xef\xbb\xbf') else b'') + output.encode()
    verify = parse(result, fmt)
    intended = copy.deepcopy(obj)
    parent = get_at(intended, keys[:-1]); parent[keys[-1]] = replacement
    if verify != intended: raise Conflict('变更后校验不一致，未写入')
    return result
