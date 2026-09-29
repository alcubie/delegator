"""Offline checks for the canonical v1 examples; not a production validator."""

import copy
import datetime as dt
import json
import math
from pathlib import Path
import re


ROOT = Path(__file__).resolve().parents[1]
FIXTURES = ROOT / "testdata/analytics/v1"
SCHEMAS = ROOT / "docs/analytics/v1"
COUNTERS = (
    "tickets_created", "tickets_first_ready", "tickets_accepted",
    "tickets_cancelled", "runs_started", "runs_ended", "runs_ended_unsuccessfully",
)
KEYWORDS = {
    "$schema", "$defs", "$ref", "title", "type", "properties", "required",
    "additionalProperties", "anyOf", "enum", "const", "minimum", "maximum",
    "pattern", "format", "minItems", "maxItems", "items",
}


def require(condition, message):
    if not condition:
        raise ValueError(message)


def unique_object(pairs):
    result = {}
    for key, value in pairs:
        require(key not in result, "duplicate JSON property")
        result[key] = value
    return result


def parse(raw):
    return json.loads(raw.decode("utf-8"), object_pairs_hook=unique_object,
                      parse_constant=lambda _: require(False, "non-JSON number"))


def read(path):
    return parse(path.read_bytes())


def schema_supported(schema):
    require(not (schema.keys() - KEYWORDS), "unsupported schema keyword")
    for key in ("$defs", "properties"):
        for child in schema.get(key, {}).values():
            schema_supported(child)
    for child in schema.get("anyOf", []):
        schema_supported(child)
    if "items" in schema:
        schema_supported(schema["items"])


def validate(value, schema, root):
    if "$ref" in schema:
        prefix = "#/$defs/"
        require(schema["$ref"].startswith(prefix), "external reference")
        validate(value, root["$defs"][schema["$ref"][len(prefix):]], root)
    if "anyOf" in schema:
        for choice in schema["anyOf"]:
            try:
                validate(value, choice, root)
                break
            except ValueError:
                pass
        else:
            raise ValueError("no schema alternative matches")
    types = {
        "object": isinstance(value, dict), "array": isinstance(value, list),
        "string": isinstance(value, str), "null": value is None,
        "boolean": type(value) is bool,
        "integer": type(value) in (int, float) and math.isfinite(value)
        and value == int(value),
    }
    if "type" in schema:
        require(types[schema["type"]], "wrong type")
    if "const" in schema:
        require(value == schema["const"], "wrong constant")
    if "enum" in schema:
        require(value in schema["enum"], "unknown enum value")
    if isinstance(value, dict):
        props = schema.get("properties", {})
        require(set(schema.get("required", [])) <= value.keys(), "missing field")
        if schema.get("additionalProperties") is False:
            require(value.keys() <= props.keys(), "unknown field")
        for key in value.keys() & props.keys():
            validate(value[key], props[key], root)
    if isinstance(value, list):
        require(len(value) >= schema.get("minItems", 0), "too few items")
        require(len(value) <= schema.get("maxItems", len(value)), "too many items")
        for item in value:
            validate(item, schema["items"], root)
    if "minimum" in schema:
        require(value >= schema["minimum"], "below minimum")
    if "maximum" in schema:
        require(value <= schema["maximum"], "above maximum")
    if "pattern" in schema:
        require(re.fullmatch(schema["pattern"], value) is not None, "bad pattern")
    if schema.get("format") == "date":
        dt.date.fromisoformat(value)
    if schema.get("format") == "date-time":
        # Validate the calendar without discarding nanoseconds in comparisons.
        dt.datetime.strptime(value[:19], "%Y-%m-%dT%H:%M:%S")


def midnight(date):
    return str(date) + "T00:00:00.000000000Z"


def request_rules(value, received_at):
    installation, daily = value["installation"], value["daily"]
    require(installation is not None or daily is not None, "empty request")
    consent, generated = value["consent_started_at"], value["generated_at"]
    require(consent <= generated, "future consent")
    # Preserve the fractional nanoseconds when adding the skew allowance.
    latest = dt.datetime.strptime(received_at[:19], "%Y-%m-%dT%H:%M:%S")
    latest += dt.timedelta(minutes=5)
    latest = latest.strftime("%Y-%m-%dT%H:%M:%S") + received_at[19:]
    require(generated <= latest, "future generation")
    if installation is not None:
        require(installation["first_consent_at"] <= consent, "first consent order")
    if daily is None:
        return
    start, through = daily["from"], daily["through"]
    require(through == midnight(generated[:10]), "wrong cutoff")
    floor = dt.date.fromisoformat(through[:10]) - dt.timedelta(days=90)
    require(max(consent, midnight(floor)) <= start < through, "invalid coverage")
    receipt_floor = dt.date.fromisoformat(received_at[:10]) - dt.timedelta(days=90)
    previous = ""
    for day in daily["days"]:
        date = day["date"]
        require(previous < date, "unsorted or duplicate date")
        require(start[:10] <= date < through[:10], "date outside coverage")
        require(str(receipt_floor) <= date, "expired date")
        require(any(day[key] for key in COUNTERS), "zero-use day")
        require(day["runs_ended_unsuccessfully"] <= day["runs_ended"], "bad ends")
        require(sum(day["runs_started_by_agent"].values()) == day["runs_started"],
                "agent sum differs from starts")
        previous = date


def acknowledges(response, request, status):
    if status != 200 or response.get("status") != "accepted":
        return False
    return (
        all(response[key] == request[key] for key in
            ("instance_id", "consent_started_at", "generated_at"))
        and response["installation_acknowledged"] == (request["installation"] is not None)
        and response["reported_through"] ==
        (request["daily"]["through"] if request["daily"] else None)
    )


def check_sequence():
    sequence = read(FIXTURES / "sequence.json")
    installations, rows, settings = {}, {}, {}

    def apply(request, revoked=False):
        if revoked:
            return
        instance, generated = request["instance_id"], request["generated_at"]
        if request["installation"] is not None:
            first = request["installation"]["first_consent_at"]
            installations[instance] = min(first, installations.get(instance, first))
        daily = request["daily"]
        if daily is None:
            return
        if generated > settings.get(instance, ("", None))[0]:
            settings[instance] = (generated, daily["settings"])
        for day in daily["days"]:
            key = (instance, request["consent_started_at"], day["date"])
            if generated > rows.get(key, ("", None))[0]:
                rows[key] = (generated, day)

    # A full second replay must not double-count anything.
    for _ in range(2):
        for filename in sequence["requests"]:
            apply(read(FIXTURES / filename))
        expected = sequence["expected"]
        require(len(installations) == expected["installations"], "installation dedup")
        require(len(rows) == expected["daily_rows"], "daily upsert keys")
        totals = {key: sum(day[key] for _, day in rows.values()) for key in COUNTERS}
        require(totals == expected["totals"], "sequence totals")
        require(list(settings.values()) == [(expected["settings_generated_at"],
                                            expected["settings"])], "stale settings")
    installations.clear()
    rows.clear()
    settings.clear()
    for filename in sequence["requests"]:
        apply(read(FIXTURES / filename), revoked=True)
    require(not (installations or rows or settings), "revoked identity restored")


def check_counting(request_schema):
    source = read(FIXTURES / "counting.json")
    start, through = source["from"], source["through"]
    families = request_schema["$defs"]["family"]["enum"]
    days, first_ready = {}, set()

    def add(at, counter, family=None):
        if not start <= at < through:
            return
        date = at[:10]
        day = days.setdefault(date, {"date": date, **dict.fromkeys(COUNTERS, 0),
                                    "runs_started_by_agent": dict.fromkeys(families, 0)})
        day[counter] += 1
        if family is not None:
            day["runs_started_by_agent"][family] += 1

    for transition in source["transitions"]:
        at, ticket, target = (transition[key] for key in ("at", "ticket", "to"))
        if transition["from"] is None:
            add(at, "tickets_created")
        if target == "ready" and ticket not in first_ready:
            first_ready.add(ticket)
            add(at, "tickets_first_ready")
        if target in ("done", "cancelled"):
            add(at, "tickets_accepted" if target == "done" else "tickets_cancelled")
    for run in source["runs"]:
        add(run["started_at"], "runs_started", run["family"])
        if run["ended_at"] is not None:
            add(run["ended_at"], "runs_ended")
            if run["exit_code"] != 0:
                add(run["ended_at"], "runs_ended_unsuccessfully")
    expected = source["expected_days"]
    require([days[key] for key in sorted(days)] == expected, "counting oracle")
    require(expected == read(FIXTURES / "retry-expanded.json")["daily"]["days"],
            "counting and wire fixtures disagree")


def check_normalization():
    source = read(FIXTURES / "normalization.json")
    builtins = {
        "claude": ["claude-agent-acp"], "codex": ["codex-acp"],
        "gemini": ["gemini", "--experimental-acp"], "opencode": ["opencode", "acp"],
        "goose": ["goose", "acp"], "github-copilot": ["copilot", "--acp"],
        "cursor": ["agent", "acp"], "pi": ["pi-acp"],
    }
    for case in source["agents"]:
        family = case["name"] if (case["name"] in builtins and
                                  builtins[case["name"]] == case["argv"]) else "custom"
        require(family == case["expected"], "agent normalization")
    number = r"(?:0|[1-9][0-9]{0,8})"
    for case in source["versions"]:
        version = (case["input"][1:] if re.fullmatch(rf"v{number}\.{number}\.{number}",
                                                   case["input"]) else "development")
        require(version == case["expected"], "version normalization")
    for case in source["os"]:
        os = case["input"] if case["input"] in ("linux", "darwin", "windows") else "other"
        require(os == case["expected"], "OS normalization")


def check_limits(schema, received_at):
    # Generate repetitive limit cases without committing ninety near-identical rows.
    value = read(FIXTURES / "retention-boundary.json")
    row = value["daily"]["days"][0]
    value["daily"]["days"] = [dict(row, date=str(dt.date(2026, 7, 1) +
                                               dt.timedelta(days=i))) for i in range(90)]
    validate(value, schema, schema)
    request_rules(value, received_at)
    require(len(json.dumps(value).encode()) < 131072, "90-day request exceeds limit")
    excessive = copy.deepcopy(value)
    excessive["daily"]["days"].append(dict(row, date="2026-09-29"))
    try:
        validate(excessive, schema, schema)
    except ValueError:
        pass
    else:
        raise ValueError("91-day request accepted")


def main():
    schemas = {kind: read(SCHEMAS / (kind + ".schema.json"))
               for kind in ("request", "response")}
    for schema in schemas.values():
        schema_supported(schema)
    manifest = read(FIXTURES / "cases.json")
    listed = {case["file"] for case in manifest["cases"]}
    auxiliary = {"cases.json", "sequence.json", "normalization.json", "counting.json"}
    require(listed | auxiliary == {p.name for p in FIXTURES.glob("*.json")},
            "fixture missing from manifest")
    for case in manifest["cases"]:
        valid = True
        acknowledged = False
        try:
            raw = (FIXTURES / case["file"]).read_bytes()
            require(len(raw) <= (131072 if case["kind"] == "request" else 4096),
                    "body too large")
            value = parse(raw)
            schema = schemas[case["kind"]]
            validate(value, schema, schema)
            if case["kind"] == "request":
                request_rules(value, case.get("received_at", manifest["received_at"]))
            else:
                acknowledged = acknowledges(value, read(FIXTURES / case["request"]),
                                            case["http_status"])
        except ValueError:
            valid = False
        require(valid == case["valid"], "unexpected validation: " + case["file"])
        if "acknowledges" in case:
            require(acknowledged == case["acknowledges"], "bad ack: " + case["file"])
    for malformed in (b'{"a":1,"a":2}', b'{"a":NaN}', b'{}{}', b'"\xff"'):
        try:
            parse(malformed)
        except ValueError:
            continue
        raise ValueError("malformed transport accepted")
    # Check every object boundary, including each fixed agent count object.
    request = read(FIXTURES / "retry-expanded.json")

    def unknown_fields(value):
        if isinstance(value, dict):
            value["unexpected_private_field"] = "synthetic"
            try:
                validate(request, schemas["request"], schemas["request"])
            except ValueError:
                pass
            else:
                raise ValueError("unknown field accepted")
            del value["unexpected_private_field"]
            for child in value.values():
                unknown_fields(child)
        elif isinstance(value, list):
            for child in value:
                unknown_fields(child)

    unknown_fields(request)
    check_sequence()
    check_counting(schemas["request"])
    check_normalization()
    check_limits(schemas["request"], manifest["received_at"])
    print(f"Analytics v1: {len(manifest['cases'])} fixtures, counting, retries, "
          "deletion, normalization, limits and strict-field checks passed")


if __name__ == "__main__":
    main()
