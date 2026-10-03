# Telemetry and privacy

Delegator's optional telemetry sends aggregate product-usage reports to help
improve the tool. It is disabled until you explicitly submit Yes during
interactive `dg init` or set it to `true`. A new instance starts at `null`, which
means unanswered and sends nothing. Scripted setup keeps the saved value unless
you pass `dg init --telemetry=true|false`.

Check or change the choice with:

```sh
dg config get telemetry
dg config set telemetry true
dg config set telemetry false
```

After consent, Delegator immediately attempts an installation report. Later
reports cover complete UTC days through yesterday, including zero-count days,
and can catch up at most 90 days. The opt-in day, disabled or interrupted days,
and the current partial day are skipped. A failed request may be rebuilt and
retried after at least 15 minutes.

Reports contain aggregate ticket and run counts, allowlisted agent families,
current settings, release and operating-system information, a random per-database
instance UUID, and an optional application-specific machine hash. Current
settings are snapshots taken when reports are built, not a history of settings.
Ticket text and titles, prompts, source code, paths, project names, logs, errors,
commands, credentials, and individual ticket or run IDs are excluded.

Setting telemetry to `false` stops future reports. It does not contact the
collector or erase reports already received. There is no environment override,
public payload preview, telemetry command group, or self-service deletion command.
Read the [privacy notice](https://alcubi.ai/delegator/privacy/) for identifier derivation, the complete
field list, provider and IP handling, retention, and the privacy-request and
operator-deletion process.
