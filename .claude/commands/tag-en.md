---
description: Closed-vocabulary tagging — tag a paper using ONLY tags that already exist in the library. Reads the paper, then picks tags solely from the existing tag set; never invents new tags (use tag-new for that). --dry-run to preview, --max to cap how many tags are added.
user-invocable: true
---

# Closed-Vocabulary Tagging Skill

The paper's itemKey and arguments are passed via `$ARGUMENTS`.

The core principle of this skill is **"never let new tags proliferate in Zotero."**
Tags applied here are limited to **tags that already exist in the library**
(a closed vocabulary). If you want to apply a concept that has no existing tag,
that is the job of the separate `tag-new` skill — this skill **never creates a
new tag**.

## Argument Parsing

Parse `$ARGUMENTS` with the following rules:

- First token (or consecutive tokens other than `--dry-run` / `--max`) → `<query>` (required)
- `--dry-run` → Do not apply; only preview the tags to be added and the resulting tag set
- `--max <N>` → Upper bound on how many tags this skill newly applies (defaults to `5`)
- If no `<query>` is found, display an error message and exit

### Query Classification

Determine whether `<query>` matches itemKey format (exactly 8 alphanumeric characters, e.g., `FQVL7ZHM`):

- **Matches itemKey format** → Use directly as `<itemKey>`
- **Does not match** → Treat as title/keyword search (proceed to Step 1a search)

## Procedure

### Step 1a: Resolve itemKey

**If query is in itemKey format:** Use directly as `<itemKey>` and proceed to Step 2.

**If query is a keyword:**

Run the following with the Bash tool:

```
cd ~/zotero-cli && ./zotero-cli search <query>
```

Branch based on results:

- **0 results** → Display "No results found for: `<query>`" and exit
- **1 result** → Automatically adopt that itemKey and proceed to Step 2
- **Multiple results** → Display a numbered list of candidates and use AskUserQuestion to let the user select a number. Proceed to Step 2 with the selected itemKey

Candidate list format:

```
Search results: <N> items found. Enter a number to select.

  1. [FQVL7ZHM] Investigating Environmental, Social, and Governance... (Angioni et al.)
  2. [99NU4NKK] An automated information extraction system... (Mohsin et al., 2024)
  ...
```

### Step 2: Understand the Paper

Run the following with the Bash tool to retrieve the paper's metadata and notes (if any):

```
cd ~/zotero-cli && ./zotero-cli context <itemKey> --json --with-notes
```

If the title and abstract are not enough to classify it, also fetch the body text:

```
cd ~/zotero-cli && ./zotero-cli fulltext <itemKey> --max-chars 8000
```

If the command fails, display the error and exit.

### Step 3: Fetch the Existing Tag List (the Closed Vocabulary)

Run the following with the Bash tool to retrieve the library's **existing tag set**:

```
cd ~/zotero-cli && ./zotero-cli tags --output json
```

Only the tags returned here form the **entire set** of tags you may apply in
this skill. `numItems` (usage count) is included, so use it to judge whether a
tag is well-established or a one-off.

### Step 4: Choose Tags ONLY From the Existing Set

Cross-reference the paper content (Step 2) with the existing tag list (Step 3)
and choose tags that fit the content **only from within the existing tag set**.

- **Never choose or create a tag outside the set.** Do not apply a "would-be
  nice" new tag here.
- Do not exceed `--max <N>` (default 5). Pick the top N by strongest fit.
- Do not count tags already on the item (only newly-added tags count toward the limit).

### Step 5: Surface Unregistered-but-Desired Concepts (Without Applying)

If there are concepts you genuinely want to apply but that have no matching tag
in the existing list, only **list them as "candidates (unregistered — not
applied by this skill)."**

```
## Applied tags (chosen from the existing vocabulary)
- <existing tag 1>
- <existing tag 2>

## Candidates (unregistered — not applied)
To register these as new tags, use the `tag-new` skill.
- <unregistered concept 1>
- <unregistered concept 2>
```

### Step 6: Apply the Tags

Apply the chosen existing tags.

**With `--dry-run`:**

```
cd ~/zotero-cli && ./zotero-cli tag <itemKey> --add "<tag1>" --add "<tag2>" --dry-run
```

Preview the resulting tag set and exit (no write is performed).

**Otherwise:**

```
cd ~/zotero-cli && ./zotero-cli tag <itemKey> --add "<tag1>" --add "<tag2>"
```

After applying, display the applied tags, the resulting tag set, and the
Step 5 candidates (if any), then exit.
