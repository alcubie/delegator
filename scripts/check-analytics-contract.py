#!/usr/bin/env python3
"""Check shared JSON examples and INSERT-only replay, not a wire schema."""

import copy
import json
from pathlib import Path
import sqlite3
import uuid

FIXTURES = Path(__file__).resolve().parents[1] / "testdata/analytics/v1"


def read(name):
    return json.loads((FIXTURES / name).read_text())


def replay(db, request):
    # Mirrors only the documented unique-key storage rule. Production validation
    # and sanitization belong to the collector's tests, not a second validator.
    with db:
        for report in request["reports"]:
            key = ("installation" if report["kind"] == "installation"
                   else "daily:" + report["date"])
            expected = uuid.uuid5(uuid.UUID(request["instance_id"]), key)
            assert report["id"] == str(expected), (report["id"], expected)
            db.execute("""INSERT INTO reports VALUES (?, ?, ?, ?, ?)
                          ON CONFLICT(report_id) DO NOTHING""",
                       (report["id"], request["instance_id"], report["kind"],
                        report["date"], json.dumps(report["data"])))


def main():
    installation = read("installation.json")
    catch_up = read("catch-up.json")
    expanded = read("retry-expanded.json")
    assert [r["date"] for r in catch_up["reports"]] == ["2026-09-26", "2026-09-27"]
    assert [r["date"] for r in expanded["reports"]] == [
        "2026-09-26", "2026-09-27", "2026-09-28"]
    assert [r["id"] for r in expanded["reports"][:2]] == [
        r["id"] for r in catch_up["reports"]]
    with sqlite3.connect(":memory:") as db:
        db.execute("""CREATE TABLE reports (
            report_id TEXT PRIMARY KEY, instance_id TEXT, kind TEXT,
            date TEXT, data TEXT)""")
        for request in (installation, installation, catch_up, expanded, expanded):
            replay(db, request)
        assert db.execute("SELECT count(*) FROM reports").fetchone()[0] == 4
        assert db.execute("SELECT count(*) FROM reports WHERE kind='installation'").fetchone()[0] == 1
        rows = db.execute("SELECT data FROM reports WHERE kind='daily' ORDER BY date").fetchall()
        assert sum(json.loads(row[0])["tickets_created"] for row in rows) == 3
        before = db.execute("SELECT * FROM reports ORDER BY report_id").fetchall()
        changed = copy.deepcopy(expanded)
        changed["reports"][0]["data"]["tickets_created"] = 999
        changed["reports"][0]["data"]["settings"]["runs"] = 99
        replay(db, changed)
        assert db.execute("SELECT * FROM reports ORDER BY report_id").fetchall() == before
        # A different instance reporting the same date is not a duplicate.
        other = copy.deepcopy(catch_up)
        other["instance_id"] = "22222222-2222-4222-8222-222222222222"
        for report in other["reports"]:
            report["id"] = str(uuid.uuid5(uuid.UUID(other["instance_id"]), "daily:" + report["date"]))
        replay(db, other)
        assert db.execute("SELECT count(*) FROM reports").fetchone()[0] == 6
    print("Analytics examples: stable IDs, catch-up, duplicate suppression and first receipt verified")


if __name__ == "__main__":
    main()
