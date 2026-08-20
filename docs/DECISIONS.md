# Decisions

**Language:** ASD-STE100 Simplified Technical English, Issue 9. The technical names are
in §0 of [`TECHNICAL_DESIGN.md`](TECHNICAL_DESIGN.md).

Each row gives one decision and a short reason. The commit message gives the full
reason, and the conditions that make us examine the decision again.

| Date | Decision | The short reason | Commit |
|---|---|---|---|
| 2026-08-18 | A ticket is one directory with two files: `ticket.yaml` for the fields, and `ticket.md` for the prose. | The person writes the prose, and delegator writes the fields. Two files make the two owners clear, and no program must find the end of a header. | [611de08](https://github.com/alcubie/delegator/commit/611de08) |
| 2026-08-18 | Read and write YAML with `github.com/goccy/go-yaml`. | `gopkg.in/yaml.v3` was archived in April 2025. The v4 line of `go.yaml.in/yaml` is new, and its v1 to v3 lines are frozen. goccy has years of releases behind its API. | [5765188](https://github.com/alcubie/delegator/commit/5765188) |
| 2026-08-18 | Only `dg ticket` and `dg revise` write `ticket.md`. A command adds to the end of the file, and never writes it again from memory. | An append cannot lose text, because it does not hold the old text. A write from memory can put text that is minutes old over the text of the person. | [9d48c13](https://github.com/alcubie/delegator/commit/9d48c13) |
| 2026-08-18 | Write the limits on `result` and `flags` by hand. Do not use `github.com/go-playground/validator`. | Half of the rules of a ticket cannot be tags. A tag saves work only when a message for a developer is enough, and each message goes to an agent. The library adds 17 modules. | [91a1754](https://github.com/alcubie/delegator/commit/91a1754) |
| 2026-08-20 | Use hidden files for delegator-only files | Prefix with dot (.) for files that are only read by the program to further distinguish the files |  |