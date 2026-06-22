# Description
The `seed` folder holds database seeding and migrations.

- `migrate.go` runs the automatic, idempotent data migrations on every startup as part of `InitialInstallAndSeed`. These run unattended and must stay safe to repeat.
- The `*_dev.go` files seed development data.
- `recipient_lowercase.go` is an opt in, manual one off migration, described below.

# Opt in migrations

Some migrations are not safe to run unattended, so they are not part of startup. They are run explicitly from the CLI and exit once finished:

```
./phishingclub --migrate <name>
```

Available names:

- `lowercase-recipient-emails`

## lowercase-recipient-emails

### Background

Before 1.39.0 recipient emails were stored exactly as entered and were not normalized to lowercase. A recipient email is the recipient identity, so this allowed two recipients that differ only by casing, for example `Foo@example.com` and `foo@example.com`, to exist as separate rows. They are the same person stored twice, a duplicate recipient.

From 1.39.0 new recipients are stored lowercased and all lookups are case insensitive, so this duplicate can no longer be created. That fix applies to new data only and does not touch rows already in the database.

This migration repairs data created before 1.39.0 by normalizing existing recipients to lowercase and merging any case variant duplicates into one. It is only needed if such duplicates exist. On an install that has no duplicate recipients it makes no changes, and running it is harmless.

### What it does

For every set of recipients that share the same lowercased email it:

1. Picks the survivor. The row already stored lowercased is kept. If none is, the oldest row is kept and its email is lowercased.
2. Merges the others into the survivor. Every reference is repointed to the survivor across campaign membership, group membership, events and device codes. A membership that would become a duplicate, for example the same person already in a campaign or group, is collapsed to one.
3. Deletes the merged recipients.

A recipient that is simply stored with mixed casing and has no duplicate is lowercased in place.

The whole run is a single transaction, so it either completes fully or changes nothing. It is idempotent, so running it again does nothing once the data is clean.

### Dry run first

Add `--dry-run` to see what would change without writing anything:

```
./phishingclub --migrate lowercase-recipient-emails --dry-run
```

The dry run performs the full merge inside a transaction and then rolls it back, so the logged summary is exactly what a real run would do. Use it to confirm whether you have duplicates and how many recipients would be merged before running it for real.

### Run only when no campaigns are active

Run this only when no campaign is sending or collecting results. The merge is a real merge: when two case variants of the same person are in the same campaign, only one membership survives, and the removed membership row id is what appears in already sent tracking links and pixels, so those links stop resolving. Running it against a quiet system avoids losing in flight tracking.

### What it logs

On completion it logs a summary:

- `groupsProcessed` lowercased emails that needed work
- `duplicateGroups` of those, how many had more than one recipient
- `recipientsMerged` recipients removed by merging
- `emailsLowercased` survivors whose stored email was changed to lowercase

### Notes

- The application database is SQLite. Matching uses SQL `LOWER()`, which folds ASCII `A` to `Z`. A non ASCII letter in the local part is the lone case it does not fold, consistent with the rest of the recipient code.
- It lowercases but does not trim whitespace.
