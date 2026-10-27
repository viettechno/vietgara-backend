#!/usr/bin/env python3
"""Traceability between documented E2E scenarios and the blackbox suite.

Two checks, standard library only:

1. Every `E2E-XXX-NNN` ID named by a blackbox test (a `// Scenario:
   E2E-XXX-NNN.` comment, optionally `(partial)`) must exist in the
   documented set; every documented P1/P2 scenario should have a test.
2. Every API operation in the OpenAPI document should be called by a
   blackbox test. Path templates are resolved from the typed client
   (`client/*.go`: literals plus the `garagePath`/`repairOrderPath`
   helpers), and only methods invoked from `scenarios/` or `fixture/`
   count — `seed/` is setup, not coverage.

Documented IDs come from a checked-in snapshot (regenerate with --regen
from a vietgara-docs checkout) because that repository is internal and CI
has no credentials for it. The snapshot header records the source commit.

Exit status: 1 when a test names an unknown ID. Missing scenarios and
coverage below the gate only fail with --fail-on-missing /
--fail-on-coverage; until the Release 4.1 batches land they report as
warnings (the missing list is the 4.1 burn-down).
"""

import argparse
import datetime
import json
import pathlib
import re
import sys
import urllib.request

DOC_FILES = [
    "01_Identity_Access.md",
    "02_Garage_Staff.md",
    "03_License_Subscription.md",
    "04_Customer_Vehicle_Partner.md",
    "05_Quote_Pricing.md",
    "06_Repair_Order.md",
    "07_Inventory.md",
    "08_Settlement.md",
    "09_Catalogue.md",
    "10_Platform_Staff.md",
]
DOC_BASE = "docs/05-Testing/E2E_Scenarios"
RAW_BASE = "https://raw.githubusercontent.com/viettechno/vietgara-docs/{ref}/" + DOC_BASE

ID_RE = re.compile(r"E2E-[A-Z]+-[0-9]+")
HEADING_RE = re.compile(r"^###\s*(E2E-[A-Z]+-[0-9]+)\b")
PRIORITY_RE = re.compile(r"\*\*Priority:\*\*\s*(P1|P2|P3)")
TAG_RE = re.compile(r"Scenario:\s*(E2E-[A-Z]+-[0-9]+(?:\s*,\s*E2E-[A-Z]+-[0-9]+)*)(\s*\(partial\))?")
SOURCE_RE = re.compile(r"# Generated from (\S+)")
CALL_SITE_RE = re.compile(r"\.([A-Z][A-Za-z0-9_]*)\(")
CLIENT_METHOD_RE = re.compile(r"^func \(c \*Client\) (\w+)\(", re.MULTILINE)
HELPER_RE = re.compile(r"^func (\w+)\([^)]*\) string \{", re.MULTILINE)
HTTP_VERBS = {"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"}
# Documented scenarios that cannot run against the shared blackbox stack and
# stay manual, with the reason. They are reported but never count as missing.
MANUAL_ONLY = {
    "E2E-IAM-021": "edits PLATFORM_ADMIN_EMAILS and restarts the backend twice",
}
# Test-harness packages whose calls execute as part of a test run. seed/
# is deliberately excluded: seeding is setup, not coverage.
CALLER_DIRS = ("scenarios", "fixture")


def documented_from_dir(docs_dir):
    """{id: priority} from a vietgara-docs checkout."""
    found = {}
    for name in DOC_FILES:
        lines = (pathlib.Path(docs_dir) / DOC_BASE / name).read_text().splitlines()
        for i, line in enumerate(lines):
            m = HEADING_RE.match(line)
            if not m:
                continue
            prio = "?"
            for follow in lines[i + 1 : i + 8]:
                pm = PRIORITY_RE.search(follow)
                if pm:
                    prio = pm.group(1)
                    break
            found.setdefault(m.group(1), prio)
    return found


def documented_from_ref(ref):
    """{id: priority} by fetching the scenario files at a docs ref."""
    found = {}
    for name in DOC_FILES:
        url = RAW_BASE.format(ref=ref) + "/" + name
        try:
            with urllib.request.urlopen(url, timeout=30) as resp:
                text = resp.read().decode("utf-8")
        except Exception as exc:
            sys.exit(f"traceability: cannot fetch {url}: {exc}")
        lines = text.splitlines()
        for i, line in enumerate(lines):
            m = HEADING_RE.match(line)
            if not m:
                continue
            prio = "?"
            for follow in lines[i + 1 : i + 8]:
                pm = PRIORITY_RE.search(follow)
                if pm:
                    prio = pm.group(1)
                    break
            found.setdefault(m.group(1), prio)
    return found


def regen(docs_dir, out_path, source_ref):
    found = documented_from_dir(docs_dir)
    with open(out_path, "w") as f:
        f.write(
            f"# Documented E2E scenario IDs and priorities.\n"
            f"# Generated from {source_ref} on {datetime.date.today().isoformat()}.\n"
            f"# DO NOT EDIT: regenerate with\n"
            f"#   python3 testing/blackbox/scripts/check_traceability.py"
            f" --regen --docs-dir <vietgara-docs> --out {out_path}\n"
        )
        for sid in sorted(found):
            f.write(f"{sid} {found[sid]}\n")
    print(f"traceability: wrote {len(found)} documented IDs to {out_path}")


def load_snapshot(path):
    """(source_comment, {id: priority}) from the snapshot file."""
    source, found = "", {}
    for line in pathlib.Path(path).read_text().splitlines():
        sm = SOURCE_RE.match(line)
        if sm:
            source = sm.group(1)
            continue
        if line.startswith("#"):
            continue
        parts = line.split()
        if not parts:
            continue
        found[parts[0]] = parts[1] if len(parts) > 1 else "?"
    return source, found


def test_tags(scenarios_dir):
    """{id: 'full'|'partial'} from // Scenario: comments in *_test.go."""
    tags = {}
    for path in sorted(pathlib.Path(scenarios_dir).rglob("*_test.go")):
        for m in TAG_RE.finditer(path.read_text()):
            partial = bool(m.group(2))
            for sid in ID_RE.findall(m.group(1)):
                if sid not in tags or not partial:
                    tags[sid] = "partial" if partial else "full"
    return tags


class Unresolvable(Exception):
    pass


def _strip_line_comments(source):
    """Remove // comments (outside string literals)."""
    out, i, s = [], 0, None
    while i < len(source):
        ch = source[i]
        if s:
            out.append(ch)
            if ch == "\\" and i + 1 < len(source):
                out.append(source[i + 1])
                i += 2
                continue
            if ch == s:
                s = None
            i += 1
            continue
        if ch in "\"`'":
            s = ch
            out.append(ch)
            i += 1
            continue
        if ch == "/" and source[i + 1 : i + 2] == "/" and not ch == "*":
            while i < len(source) and source[i] != "\n":
                i += 1
            continue
        out.append(ch)
        i += 1
    return "".join(out)


def split_top_level(text, sep):
    """Split on sep at nesting depth 0 (strings respected, no comments)."""
    parts, depth, cur, i, s = [], 0, [], 0, None
    while i < len(text):
        ch = text[i]
        if s:
            cur.append(ch)
            if ch == "\\" and i + 1 < len(text):
                cur.append(text[i + 1])
                i += 2
                continue
            if ch == s:
                s = None
            i += 1
            continue
        if ch in "\"`'":
            s = ch
            cur.append(ch)
            i += 1
            continue
        if ch in "([{":
            depth += 1
        elif ch in ")]}":
            depth -= 1
        if ch == sep and depth == 0:
            parts.append("".join(cur))
            cur = []
        else:
            cur.append(ch)
        i += 1
    parts.append("".join(cur))
    return parts


def _balanced(text, start, openers="([{", closers=")]}"):
    """End index (exclusive) of the balanced run starting at start."""
    depth, i, s = 0, start, None
    while i < len(text):
        ch = text[i]
        if s:
            if ch == "\\" and i + 1 < len(text):
                i += 2
                continue
            if ch == s:
                s = None
            i += 1
            continue
        if ch in "\"`'":
            s = ch
            i += 1
            continue
        if ch in openers:
            depth += 1
        elif ch in closers:
            depth -= 1
            if depth == 0:
                return i + 1
        i += 1
    raise Unresolvable(text[start : start + 60])


def top_level_funcs(source):
    """[(name, body)] for top-level `func` declarations."""
    clean = _strip_line_comments(source)
    found = []
    for m in re.finditer(r"^func (?:\(c \*Client\) )?(\w+)\(", clean, re.MULTILINE):
        brace = clean.index("{", m.end())
        found.append((m.group(1), clean[brace + 1 : _balanced(clean, brace) - 1]))
    return found


def resolve_expr(expr, helpers, env=None, seen=()):
    """Resolve a path expression to a template; {..} marks parameters.

    Besides literals, helper calls and url.PathEscape, bare identifiers
    resolve through env (local `path :=` / `path +=` assignments collected
    by collect_calls).
    """
    env = env or {}
    parts = split_top_level(expr, "+")
    if len(parts) > 1:
        return "".join(resolve_expr(p, helpers, env, seen) for p in parts)
    atom = expr.strip()
    if len(atom) >= 2 and atom[0] == '"' and atom[-1] == '"':
        return atom[1:-1].replace('\\"', '"').replace("\\\\", "\\")
    if len(atom) >= 2 and atom[0] == "`" and atom[-1] == "`":
        return atom[1:-1]
    m = re.fullmatch(r"url\.PathEscape\(.*\)", atom, re.DOTALL)
    if m:
        return "{id}"
    m = re.fullmatch(r"([A-Za-z_]\w*)\(.*\)", atom, re.DOTALL)
    if m:
        name = m.group(1)
        if name in helpers and name not in seen:
            return resolve_expr(helpers[name], helpers, env, seen + (name,))
        raise Unresolvable(atom)
    m = re.fullmatch(r"([A-Za-z_]\w*)", atom)
    if m and m.group(1) in env:
        if m.group(1) in seen:
            raise Unresolvable(atom)
        return env[m.group(1)]
    raise Unresolvable(atom)


def _statements(body):
    """Go statements: split on ; and newlines at depth 0 (strings kept)."""
    out = []
    for part in split_top_level(body, ";"):
        out.extend(s.strip() for s in split_top_level(part, "\n"))
    return [s for s in out if s]


def collect_calls(body, helpers):
    """[(http_method|None, template)] from call[/DownloadRaw sites.

    Tracks local `name :=` / `name +=` string assembly (the client's
    `path := ...` + `?updateMask=`/`?query=` pattern); query strings are
    stripped since they never appear in the OpenAPI paths.
    """
    env, pairs, ok = {}, [], True
    for stmt in _statements(body):
        sites = []
        for keyword in ("call[", "DownloadRaw("):
            start = 0
            while True:
                at = stmt.find(keyword, start)
                if at < 0:
                    break
                try:
                    if keyword == "call[":
                        br = stmt.index("[", at + 4)
                        paren = stmt.index("(", _balanced(stmt, br))
                    else:
                        paren = stmt.index("(", at + len(keyword) - 1)
                    end = _balanced(stmt, paren)
                    args = split_top_level(stmt[paren + 1 : end - 1], ",")
                    if keyword == "call[":
                        method_arg, path_arg = args[2], args[3]
                    else:
                        method_arg, path_arg = args[1], args[2]
                    mm = re.search(r"http\.Method(\w+)", method_arg)
                    template = resolve_expr(path_arg, helpers, env).split("?")[0]
                    sites.append((
                        mm.group(1).upper() if mm else None,
                        template,
                    ))
                    start = end
                except (Unresolvable, ValueError, IndexError):
                    ok = False
                    start = at + len(keyword)
        if sites:
            pairs.extend(sites)
            continue
        m = re.match(r"(\w+)\s*:=\s*(.+)$", stmt, re.S)
        if m:
            try:
                env[m.group(1)] = resolve_expr(m.group(2), helpers, env)
            except Unresolvable:
                env.pop(m.group(1), None)
            continue
        m = re.match(r"(\w+)\s*\+=\s*(.+)$", stmt, re.S)
        if m and m.group(1) in env:
            try:
                env[m.group(1)] = env[m.group(1)] + resolve_expr(m.group(2), helpers, env)
            except Unresolvable:
                env.pop(m.group(1), None)
    return pairs, ok


def client_table(client_dir):
    """({method: [(http_method, template)]}, [unresolved methods], {helpers})."""
    helpers = {}
    for path in sorted(pathlib.Path(client_dir).glob("*.go")):
        for name, body in top_level_funcs(path.read_text()):
            if not name[0].islower():
                continue  # client methods are MethodNames; helpers are camelCase
            ret = body.split("return", 1)
            if len(ret) == 2 and "\n" not in ret[1].strip().rstrip(";"):
                helpers[name] = ret[1].strip().rstrip(";")
    table, unresolved = {}, []
    for path in sorted(pathlib.Path(client_dir).glob("*.go")):
        for name, body in top_level_funcs(path.read_text()):
            if not (name and name[0].isupper()):
                continue
            pairs, ok = collect_calls(body, helpers)
            if pairs:
                table.setdefault(name, []).extend(pairs)
            if not ok:
                unresolved.append(name)
    return table, sorted(set(unresolved)), helpers


def called_methods(blackbox_dir, defined):
    """Client methods invoked from scenarios/ or fixture/ test code."""
    used = set()
    for d in CALLER_DIRS:
        for path in pathlib.Path(blackbox_dir, d).rglob("*.go"):
            used.update(CALL_SITE_RE.findall(_strip_line_comments(path.read_text())))
    return sorted(used & defined)


def _norm_seg(seg):
    return re.sub(r"\{[^{}]*\}", "{}", seg)


def template_equal(resolved, swagger_path):
    a, b = resolved.split("/"), swagger_path.split("/")
    if len(a) != len(b):
        return False
    return all(_norm_seg(x) == _norm_seg(y) for x, y in zip(a, b))


def api_coverage(swagger_path, blackbox_dir, client_dir):
    """(total, covered, uncalled, stats) over the OpenAPI operations."""
    with open(swagger_path) as f:
        spec = json.load(f)
    ops = [
        (method.upper(), path)
        for path, methods in spec["paths"].items()
        for method in methods
        if method.upper() in HTTP_VERBS
    ]
    table, unresolved, _ = client_table(client_dir)
    called = called_methods(blackbox_dir, set(table))
    templates = [
        (m, t) for name in called for m, t in table.get(name, []) if m
    ]
    uncalled = [
        (method, path)
        for method, path in ops
        if not any(m == method and template_equal(t, path) for m, t in templates)
    ]
    stats = {
        "defined": len(table),
        "called": len(called),
        "never_called": sorted(
            n for n in table if n not in called and table[n]
        ),
        "unresolved": [n for n in unresolved if n in called],
    }
    return len(ops), len(ops) - len(uncalled), sorted(uncalled), stats


def main():
    ap = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    ap.add_argument("--scenarios-dir", default="testing/blackbox/scenarios")
    ap.add_argument("--client-dir", default="testing/blackbox/client")
    ap.add_argument("--ids-file", default="testing/blackbox/scenarios/documented-ids.txt")
    ap.add_argument("--proto-dir", default="",
                    help="vietgara-proto checkout (for openapi/vietgara.swagger.json); "
                         "resolved with `go list -m` when empty")
    ap.add_argument("--docs-ref", default="master",
                    help="vietgara-docs ref for --regen-from-ref")
    ap.add_argument("--coverage-gate", type=float, default=80.0)
    ap.add_argument("--fail-on-missing", action="store_true")
    ap.add_argument("--fail-on-coverage", action="store_true")
    ap.add_argument("--regen", action="store_true")
    ap.add_argument("--regen-from-ref", action="store_true")
    ap.add_argument("--docs-dir", default="")
    ap.add_argument("--out", default="testing/blackbox/scenarios/documented-ids.txt")
    ap.add_argument("--source-ref", default="")
    args = ap.parse_args()

    if args.regen:
        if not args.docs_dir or not args.source_ref:
            sys.exit("traceability: --regen needs --docs-dir and --source-ref")
        regen(args.docs_dir, args.out, args.source_ref)
        return 0
    if args.regen_from_ref:
        found = documented_from_ref(args.docs_ref)
        print(f"traceability: fetched {len(found)} documented IDs from docs@{args.docs_ref}")
        for sid in sorted(found):
            print(f"{sid} {found[sid]}")
        return 0

    source, documented = load_snapshot(args.ids_file)
    print(f"traceability: documented={len(documented)} ({source})")
    tags = test_tags(args.scenarios_dir)

    unknown = sorted(s for s in tags if s not in documented)
    if unknown:
        print("traceability: ERROR: tests name unknown scenario IDs:")
        for sid in unknown:
            print(f"  {sid}")
        return 1

    full = sorted(s for s, k in tags.items() if k == "full")
    partial = sorted(s for s, k in tags.items() if k == "partial")
    missing = sorted(s for s in documented if s not in tags and s not in MANUAL_ONLY)
    manual = sorted(s for s in MANUAL_ONLY if s in documented and s not in tags)
    by_prio = {}
    for sid in missing:
        by_prio.setdefault(documented[sid], []).append(sid)
    print(f"traceability: covered={len(full)} full, {len(partial)} partial, "
          f"missing={len(missing)} (" + ", ".join(
              f"{p}={len(by_prio.get(p, []))}" for p in ("P1", "P2", "P3", "?")) + ")")
    for sid in partial:
        print(f"  partial: {sid}")
    for prio in ("P1", "P2", "P3", "?"):
        for sid in sorted(by_prio.get(prio, [])):
            print(f"  missing {prio}: {sid}")

    for sid in manual:
        print(f"  manual only: {sid} ({MANUAL_ONLY[sid]})")

    proto_dir = args.proto_dir
    if not proto_dir:
        import subprocess
        proto_dir = subprocess.run(
            ["go", "list", "-m", "-f", "{{.Dir}}",
             "github.com/viettechno/vietgara-proto"],
            capture_output=True, text=True, cwd="testing/blackbox",
            check=True).stdout.strip()
    swagger = pathlib.Path(proto_dir) / "openapi" / "vietgara.swagger.json"
    blackbox_dir = str(pathlib.Path(args.scenarios_dir).parent)
    total, covered, uncalled, stats = api_coverage(str(swagger), blackbox_dir, args.client_dir)
    pct = 100.0 * covered / total if total else 0.0
    status = "OK" if pct >= args.coverage_gate else f"BELOW GATE {args.coverage_gate:g}% (warn)"
    print(f"api-coverage: {covered}/{total} operations ({pct:.1f}%) — {status}")
    print(f"api-coverage: client methods defined={stats['defined']}, "
          f"called from tests={stats['called']}")
    if stats["never_called"]:
        print("api-coverage: client methods never called from tests: "
              + ", ".join(stats["never_called"]))
    for name in stats["unresolved"]:
        print(f"api-coverage: WARNING: could not resolve path for {name}")
    for method, path in uncalled:
        print(f"  uncalled: {method} {path}")

    if args.fail_on_missing and missing:
        print("traceability: ERROR: documented scenarios without a test")
        return 1
    if args.fail_on_coverage and pct < args.coverage_gate:
        print("traceability: ERROR: API coverage below gate")
        return 1
    return 0


if __name__ == "__main__":
    sys.exit(main())
