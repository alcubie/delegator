"""Reject tags outside SemVer before running the release builder."""

import re
import sys

NUMBER = r"(?:0|[1-9][0-9]*)"
IDENTIFIER = rf"(?:{NUMBER}|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*)"
TAG = rf"v{NUMBER}\.{NUMBER}\.{NUMBER}(?:-{IDENTIFIER}(?:\.{IDENTIFIER})*)?(?:\+[0-9A-Za-z-]+(?:\.[0-9A-Za-z-]+)*)?"

if len(sys.argv) != 2 or re.fullmatch(TAG, sys.argv[1], flags=re.ASCII) is None:
    sys.exit("expected a semantic version tag, for example v1.4.0 or v1.4.0-rc.1")
