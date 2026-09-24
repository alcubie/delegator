#!/usr/bin/env python3
"""Run an installer test with or without a controlling terminal."""

import errno
import os
import pty
import select
import signal
import subprocess
import sys
import time


TIMEOUT_SECONDS = 30


def die(message):
    print(f"test-install-pty: {message}", file=sys.stderr)
    return 2


def terminal_run(input_path, output_path, command):
    with open(input_path, "rb") as source:
        answers = source.read()

    child, master = pty.fork()
    if child == 0:
        os.execvpe(command[0], command, os.environ)

    output = bytearray()
    deadline = time.monotonic() + TIMEOUT_SECONDS
    try:
        while answers:
            written = os.write(master, answers)
            answers = answers[written:]

        while True:
            remaining = deadline - time.monotonic()
            if remaining <= 0:
                os.killpg(child, signal.SIGTERM)
                os.waitpid(child, 0)
                return die("terminal command timed out")
            ready, _, _ = select.select([master], [], [], remaining)
            if not ready:
                continue
            try:
                chunk = os.read(master, 65536)
            except OSError as error:
                if error.errno == errno.EIO:
                    break
                raise
            if not chunk:
                break
            output.extend(chunk)
    finally:
        os.close(master)
        with open(output_path, "wb") as destination:
            destination.write(output)

    _, status = os.waitpid(child, 0)
    return os.waitstatus_to_exitcode(status)


def no_terminal_run(output_path, command):
    with open(output_path, "wb") as destination:
        try:
            result = subprocess.run(
                command,
                stdin=subprocess.DEVNULL,
                stdout=destination,
                stderr=subprocess.STDOUT,
                start_new_session=True,
                timeout=TIMEOUT_SECONDS,
                check=False,
            )
        except subprocess.TimeoutExpired:
            return die("non-terminal command timed out")
    return result.returncode


def main():
    if "--" not in sys.argv:
        return die("usage: test-install-pty.py MODE INPUT OUTPUT -- COMMAND...")
    separator = sys.argv.index("--")
    options = sys.argv[1:separator]
    command = sys.argv[separator + 1 :]
    if len(options) != 3 or not command:
        return die("usage: test-install-pty.py MODE INPUT OUTPUT -- COMMAND...")
    mode, input_path, output_path = options
    if mode == "terminal":
        return terminal_run(input_path, output_path, command)
    if mode == "no-terminal":
        return no_terminal_run(output_path, command)
    return die(f"unknown mode: {mode}")


if __name__ == "__main__":
    sys.exit(main())
