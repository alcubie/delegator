"""Stream live Go test progress, keeping verbose diagnostics in a private log."""

import argparse
from collections import Counter, defaultdict, deque
import json
import os
from pathlib import Path
import queue
import signal
import subprocess
import tempfile
import threading
import time


class Reporter:
    def __init__(self, log, emit=print):
        self.log = log
        self.emit = emit
        self.active = {}
        self.results = {}
        self.tails = defaultdict(lambda: deque(maxlen=12))
        self.started = time.monotonic()

    @staticmethod
    def label(key):
        package, test = key
        return f"{package.removeprefix('github.com/alcubie/delegator/')} {test}".strip()

    def line(self, line):
        try:
            event = json.loads(line)
        except ValueError:
            event = {"Action": "output", "Output": line}
        package = event.get("Package", event.get("ImportPath", "go test"))
        test = event.get("Test", "")
        key = (package, test)
        action = event.get("Action")
        output = event.get("Output", "")
        if output:
            self.log.write(f"[{self.label(key)}] {output}")
            self.log.flush()
            for text in output.splitlines():
                if text.strip() and not text.startswith(("===", "---", "PASS", "FAIL")):
                    self.tails[key].append(text)
        if test and action == "run":
            self.active[key] = time.monotonic()
            self.emit(f"RUN  {self.label(key)}")
        elif action in ("pass", "fail", "skip", "build-fail"):
            if action == "build-fail":
                action = "fail"
            self.active.pop(key, None)
            if test or action == "fail":
                self.results[key] = action
                self.emit(f"{action.upper():4} {self.label(key)} ({event.get('Elapsed', 0):.1f}s)")
                if action == "skip" and self.tails[key]:
                    self.emit(f"     {self.tails[key][-1].strip()[:400]}")

    def heartbeat(self):
        now = time.monotonic()
        # Report the current subtest instead of repeating its parent.
        leaves = [key for key in self.active if not any(
            other[0] == key[0] and other[1].startswith(key[1] + "/")
            for other in self.active)]
        if leaves:
            for key in leaves:
                self.emit(f"WAIT {self.label(key)} ({now - self.active[key]:.0f}s elapsed)")
        else:
            self.emit(f"WAIT building or finishing test packages ({now - self.started:.0f}s elapsed)")

    def finish(self, code):
        counts = Counter(result for (_, test), result in self.results.items()
                         if test and "/" not in test)
        self.emit(f"\nIntegration tests: {counts['pass']} passed, {counts['fail']} failed, "
                  f"{counts['skip']} skipped ({time.monotonic() - self.started:.1f}s)")
        failures = [key for key, result in self.results.items() if result == "fail"]
        if code:
            self.emit(f"go test exited with status {code}.")
            self.emit("\nFailure details (last 12 diagnostic lines per test):")
            for key in failures:
                # Package/parent failures duplicate their failing children, unless
                # they have diagnostics of their own (for example a panic).
                children = any(other != key and other[0] == key[0] and
                               (not key[1] or other[1].startswith(key[1] + "/"))
                               for other in failures)
                if children and not self.tails[key]:
                    continue
                self.emit(f"\nFAIL {self.label(key)}")
                for text in self.tails[key]:
                    self.emit("  " + text[:400] + (" … [see full log]" if len(text) > 400 else ""))
            for key, tail in self.tails.items():
                if not failures or key == ("go test", ""):
                    for text in tail:
                        self.emit(f"  {self.label(key)}: {text[:400]}")


def run(command, directory, heartbeat=15):
    def emit(text):
        print(text, flush=True)

    emit("Integration tests: building and starting live agents…")
    emit(f"Full logs: {directory / 'output.log'}")
    lines = queue.Queue()
    with (directory / "events.jsonl").open("w") as events, (directory / "output.log").open("w") as log:
        reporter = Reporter(log, emit)
        process = subprocess.Popen(command, stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
                                   text=True, errors="replace", start_new_session=True)

        def read():
            for line in process.stdout:
                lines.put(line)
            lines.put(None)

        reader = threading.Thread(target=read, daemon=True)
        reader.start()
        next_heartbeat = time.monotonic() + heartbeat
        try:
            while True:
                try:
                    line = lines.get(timeout=max(0, next_heartbeat - time.monotonic()))
                except queue.Empty:
                    line = ""
                if line is None:
                    break
                if line:
                    events.write(line)
                    events.flush()
                    reporter.line(line)
                if time.monotonic() >= next_heartbeat:
                    reporter.heartbeat()
                    next_heartbeat = time.monotonic() + heartbeat
            code = process.wait()
        except KeyboardInterrupt:
            os.killpg(process.pid, signal.SIGTERM)
            try:
                process.wait(timeout=5)
            except subprocess.TimeoutExpired:
                os.killpg(process.pid, signal.SIGKILL)
                process.wait()
            code = 130
        finally:
            process.stdout.close()
        reporter.finish(code)
    emit(f"\nFull logs: {directory / 'output.log'}")
    return code


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--run", default="^TestIntegration", help="Go test name expression")
    parser.add_argument("packages", nargs="*", default=["./..."])
    args = parser.parse_args()
    directory = Path(tempfile.mkdtemp(prefix="delegator-integration-"))
    return run(["go", "test", "-json", "-count=1", "-tags", "integration", "-timeout", "15m",
                "-run", args.run, *args.packages], directory)


if __name__ == "__main__":
    raise SystemExit(main())
