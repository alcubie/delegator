# Third-party software

The [FSL-1.1-ALv2 license](LICENSE) covers Delegator's source code, tests,
documentation, and build files unless a file says otherwise. No such file
currently states a different license, and the repository contains no vendored
dependency source.

Dependencies are not relicensed under FSL-1.1-ALv2. The module names and exact
versions are recorded in `go.mod` and `go.sum`; each remains under its upstream
license. A Delegator binary contains code from the following modules on at least
one supported operating system:

| Module | License |
| --- | --- |
| `github.com/coder/acp-go-sdk` | Apache-2.0 |
| `github.com/creack/pty` | MIT |
| `github.com/dustin/go-humanize` | MIT |
| `github.com/google/uuid` | BSD-3-Clause |
| `github.com/gosimple/slug` | MPL-2.0 |
| `github.com/gosimple/unidecode` | Apache-2.0 |
| `github.com/inconshreveable/mousetrap` | Apache-2.0 |
| `github.com/mattn/go-isatty` | MIT |
| `github.com/ncruces/go-strftime` | MIT |
| `github.com/remyoudompheng/bigfft` | BSD-3-Clause |
| `github.com/spf13/cobra` | Apache-2.0 |
| `github.com/spf13/pflag` | BSD-3-Clause |
| `golang.org/x/sys` | BSD-3-Clause |
| `modernc.org/libc` | BSD-3-Clause, with bundled third-party notices |
| `modernc.org/mathutil` | BSD-3-Clause |
| `modernc.org/memory` | BSD-3-Clause, with bundled third-party notices |
| `modernc.org/sqlite` | BSD-3-Clause; bundled SQLite is public domain and sqlite-vec is MIT |

The authoritative copyright notices and license texts accompany each module's
source. Distributors must preserve every notice and satisfy every applicable
upstream license; this summary does not replace those terms.
