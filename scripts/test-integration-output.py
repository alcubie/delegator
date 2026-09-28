"""Exercise the integration reporter without calling paid agents."""

import contextlib
import importlib.util
import io
import json
from pathlib import Path
import sys
import tempfile
import unittest

spec = importlib.util.spec_from_file_location("integration", Path(__file__).with_name("test-integration.py"))
integration = importlib.util.module_from_spec(spec)
spec.loader.exec_module(integration)


class OutputTests(unittest.TestCase):
    def test_progress_and_bounded_failure_summary_preserve_full_log(self):
        output, log = [], io.StringIO()
        reporter = integration.Reporter(log, output.append)

        def event(action, test="", **extra):
            reporter.line(json.dumps(dict(Action=action, Package="internal/handler", Test=test, **extra)))

        event("run", "TestIntegrationAgent")
        event("run", "TestIntegrationAgent/terminal")
        event("output", "TestIntegrationAgent/terminal", Output="noisy agent output\n" * 100)
        self.assertNotIn("noisy agent output", "\n".join(output))
        reporter.heartbeat()
        self.assertIn("WAIT internal/handler TestIntegrationAgent/terminal", output[-1])
        event("output", "TestIntegrationAgent/terminal", Output="resume failed: trust dialog\n")
        event("fail", "TestIntegrationAgent/terminal", Elapsed=5)
        event("fail", "TestIntegrationAgent", Elapsed=6)
        event("fail")
        reporter.finish(1)
        summary = "\n".join(output)
        self.assertIn("0 passed, 1 failed, 0 skipped", summary)
        self.assertIn("resume failed: trust dialog", summary)
        self.assertEqual(summary.count("noisy agent output"), 11)
        self.assertEqual(log.getvalue().count("noisy agent output"), 100)

    def test_build_failure_and_skip_reason(self):
        output = []
        reporter = integration.Reporter(io.StringIO(), output.append)
        reporter.line(json.dumps(dict(Action="output", Package="agent", Test="TestIntegrationMissing",
                                      Output="agent is not on PATH\n")))
        reporter.line(json.dumps(dict(Action="skip", Package="agent", Test="TestIntegrationMissing")))
        reporter.line("compiler error: undefined symbol\n")
        reporter.finish(1)
        self.assertIn("agent is not on PATH", "\n".join(output))
        self.assertIn("compiler error: undefined symbol", "\n".join(output))
        self.assertIn("0 passed, 0 failed, 1 skipped", "\n".join(output))

    def test_dependency_build_failure_retains_compiler_diagnostic(self):
        output = []
        reporter = integration.Reporter(io.StringIO(), output.append)
        reporter.line(json.dumps(dict(Action="build-output", ImportPath="dependency",
                                      Output="file.go:1: undefined symbol\n")))
        reporter.line(json.dumps(dict(Action="build-fail", ImportPath="dependency")))
        reporter.line(json.dumps(dict(Action="fail", Package="agent")))
        reporter.line("go: additional diagnostic\n")
        reporter.finish(1)
        summary = "\n".join(output)
        self.assertIn("file.go:1: undefined symbol", summary)
        self.assertIn("go: additional diagnostic", summary)

    def test_quiet_process_heartbeat_logs_and_exit_status(self):
        for code in (0, 7):
            with self.subTest(code=code), tempfile.TemporaryDirectory() as temporary:
                directory = Path(temporary)
                program = ("import time; print('compiler diagnostic', flush=True); "
                           f"time.sleep(0.15); raise SystemExit({code})")
                output = io.StringIO()
                with contextlib.redirect_stdout(output):
                    result = integration.run([sys.executable, "-c", program], directory, heartbeat=0.05)
                self.assertEqual(result, code)
                self.assertIn("WAIT building", output.getvalue())
                self.assertIn("Full logs:", output.getvalue())
                self.assertIn("compiler diagnostic", (directory / "output.log").read_text())
                self.assertIn("compiler diagnostic", (directory / "events.jsonl").read_text())


if __name__ == "__main__":
    unittest.main()
