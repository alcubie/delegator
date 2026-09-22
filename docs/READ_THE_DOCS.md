# Publishing the CLI reference

The public site is built from `docs/public`. The other documents under `docs`
are repository design and maintenance material and are not published.

## Work locally

Create an isolated Python environment and install the pinned builder:

```sh
python3 -m venv .venv
.venv/bin/python -m pip install -r docs/requirements.txt
```

Regenerate the Cobra reference after changing command help or structure:

```sh
make docs
```

Build the site, preview it at `http://127.0.0.1:8000`, or run the same check
used by CI:

```sh
make docs-build MKDOCS=.venv/bin/mkdocs
make docs-serve MKDOCS=.venv/bin/mkdocs
make docs-check MKDOCS=.venv/bin/mkdocs
```

`make docs-check` fails if the committed reference differs from `cli.Root` or
if MkDocs reports a build warning or error. `make check` includes it.

## One-time Read the Docs setup

These settings live in Read the Docs and the DNS provider, not in this
repository:

1. In Read the Docs Community, import `github.com/alcubie/delegator` through
   its GitHub integration. Use **Alcubi Delegator CLI Reference** as the name,
   `alcubi-delegator` as the project slug, and `main` as the default branch.
2. Leave `.readthedocs.yaml` at the repository root as the configuration-file
   path. It selects MkDocs, the build image, Python, and the pinned requirements.
3. Under **Versions**, keep `latest` active, public, and not hidden. It follows
   `main`. Keep other branches and tags inactive until they are intentionally
   published, and set `latest` as the default version under **Admin > Settings**.
4. The initial host is `alcubi-delegator.readthedocs.io`. If a custom Alcubi
   documentation subdomain is approved later, add it under **Admin > Domains**,
   mark it canonical, add the exact CNAME value shown there at the DNS provider,
   and update `site_url` plus the README links in the same change.

The Alcubi Delegator name is provisional pending clearance; the site must not
describe it as a registered trademark.
