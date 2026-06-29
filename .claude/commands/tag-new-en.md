---
description: Deliberately create a NEW tag and apply it to a paper. The only entry point for extending the vocabulary. Checks for duplicates among existing tags and warns on near-duplicates before applying. If you only want to tag within the closed vocabulary, use tag.
user-invocable: true
---

# New-Tag Creation Skill

The paper's itemKey and the new tag(s) to apply are passed via `$ARGUMENTS`.

This skill is **the single entry point for intentionally extending Zotero's tag
vocabulary.** Ordinary tagging (the `tag` skill) is closed to existing tags
only (a closed vocabulary); this one's **explicit purpose is to create new
tags.** Because of that, to avoid polluting the vocabulary with careless new
tags, it always runs a duplicate / near-duplicate check against existing tags
before applying.

## Argument Parsing

Parse `$ARGUMENTS` with the following rules:

- First token (or consecutive tokens other than `--tag` / `--dry-run`) → `<query>` (required; the target paper)
- `--tag "<new tag>"` → A new tag to create and apply (**required; may be repeated**)
- `--dry-run` → Do not apply; only preview the tags to be added and the resulting tag set
- If no `<query>` or no `--tag` is found, display an error message and exit

### Query Classification

Determine whether `<query>` matches itemKey format (exactly 8 alphanumeric characters, e.g., `FQVL7ZHM`):

- **Matches itemKey format** → Use directly as `<itemKey>`
- **Does not match** → Treat as title/keyword search (proceed to Step 1a search)

## Procedure

### Step 1a: Resolve itemKey

**If query is in itemKey format:** Use directly as `<itemKey>` and proceed to Step 2.

**If query is a keyword:**

```
cd ~/zotero-cli && ./zotero-cli search <query>
```

- **0 results** → Display "No results found for: `<query>`" and exit
- **1 result** → Automatically adopt that itemKey and proceed to Step 2
- **Multiple results** → Display a numbered list of candidates and use AskUserQuestion to let the user select a number

### Step 2: Duplicate / Near-Duplicate Check (Most Important)

Fetch the existing tag list with the Bash tool:

```
cd ~/zotero-cli && ./zotero-cli tags --output json
```

For each new tag given via `--tag`, cross-reference it against the existing tag list:

- **Exact match exists** (including case-only differences) → it is not new.
  Abort creation and advise "use the existing tag `<existing tag>` (the `tag`
  skill)," then exit.
- **A similar tag exists** (spelling variants, singular/plural, EN/JA, abbreviations;
  e.g., `LLM` vs `large-language-model`, `知識グラフ` vs `knowledge-graph`) →
  **warn and prompt for reconsideration.** Present the similar candidates and use
  AskUserQuestion to choose "create the new tag / use the existing tag / abort."
- **No duplicate and no near-duplicate** → the new tag is reasonable. Proceed to Step 3.

### Step 3: Apply the New Tag

Apply the confirmed new tag(s).

**With `--dry-run`:**

```
cd ~/zotero-cli && ./zotero-cli tag <itemKey> --add "<new tag>" --dry-run
```

Preview the resulting tag set and exit (no write).

**Otherwise:**

```
cd ~/zotero-cli && ./zotero-cli tag <itemKey> --add "<new tag>"
```

For multiple new tags, repeat `--add`.

### Step 4: Report Completion

Display the new tag(s) applied and the resulting tag set. If you created at
least one new tag, state explicitly that "the library vocabulary has been
extended (new tag: …)," and note that from now on that tag is also part of the
closed vocabulary the `tag` skill chooses from.
