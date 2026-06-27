---
description: Map a paper's citation network (references it cites + papers citing it) via Semantic Scholar, with a one-line summary per work in an overview table. Use --save to save as a Zotero note.
user-invocable: true
---

# Citation Network Skill

The paper's itemKey and arguments are passed via `$ARGUMENTS`. This skill lists the works the paper **cites (backward)** and the works that **cite it (forward)**, attaching a one-line summary to each and arranging them into overview tables.

The external data source is the **Semantic Scholar Graph API**. The CLI calls it, so no API key is required (if `SEMANTIC_SCHOLAR_API_KEY` is set, the rate limit is raised).

## Argument Parsing

Parse `$ARGUMENTS` with the following rules:

- First token (or consecutive tokens not starting with `--`) → `<query>` (required)
- `--direction <dir>` → `backward` (references only) / `forward` (cited-by only) / `both` (both, default)
- `--limit <N>` → max papers per direction (default 25)
- `--save` → save the result as a Zotero note (tag `ai-citations`)
- If no `<query>` is found, display an error message and exit

### Query Classification

Determine whether `<query>` matches itemKey format (exactly 8 alphanumeric characters, e.g., `FQVL7ZHM`):

- **Matches itemKey format** → use directly as `<itemKey>`
- **Does not match** → treat as a title/keyword search (Step 1a)

## Procedure

### Step 1a: Resolve itemKey

**If query is in itemKey format:** use directly as `<itemKey>` and proceed to Step 2.

**If query is a keyword:** run the following with the Bash tool:

```
cd ~/zotero-cli && ./zotero-cli search <query>
```

Branch based on results:

- **0 results** → display "No results found for: `<query>`" and exit
- **1 result** → automatically adopt that itemKey and proceed to Step 2
- **Multiple results** → display a numbered list of candidates and use AskUserQuestion to let the user select. Proceed to Step 2 with the selected itemKey

Candidate list format:

```
Search results: <N> items found. Enter a number to select.

  1. [FQVL7ZHM] Investigating Environmental, Social, and Governance... (Angioni et al.)
  2. [99NU4NKK] An automated information extraction system... (Mohsin et al., 2024)
  ...
```

### Step 2: Fetch the Citation Network

Run the following with the Bash tool (`<dir>` `<N>` resolved from the arguments):

```
cd ~/zotero-cli && ./zotero-cli citations <itemKey> --direction <dir> --limit <N> --output json
```

The response uses the `{"ok":true,"data":{...}}` envelope. `data` has this shape:

- `title` — the target paper's title
- `paperId` — the paper identified on Semantic Scholar
- `backward` — array of works this paper cites (empty when `--direction forward`)
- `forward` — array of works that cite this paper (sorted by citation count desc; empty when `--direction backward`)

Each entry has `title` / `year` / `authors` (array of `name`) / `citationCount` / `abstract` / `externalIds`.

**Error handling:**

- `{"ok":false,"error":{"code":"NOT_FOUND",...}}` → the paper could not be identified on Semantic Scholar (no DOI, arXiv ID, or title match). Report this and exit, noting it is not a bug but a metadata gap.
- `{"ok":false,"error":{"code":"API_ERROR",...}}` → API failure or rate limit. Display the message and exit.
- An empty `backward` / `forward` array → "none". This is not an error (it can legitimately happen for papers not in Semantic Scholar).

### Step 3: Build the Overview Tables

Read the JSON and attach a **one-line summary** (the gist in one sentence, from title/abstract) to each work, then arrange everything **in English** into the tables below. For works without an abstract, infer the gist from the title (keep it concise; no need to flag it as inferred).

```
## Citation Network: <Title>

Semantic Scholar ID: `<paperId>`

### Key works this paper cites (backward, N)

| Year | Title | Authors | One-line summary | Citations |
|------|-------|---------|------------------|-----------|
| 2017 | Attention Is All You Need | Vaswani et al. | Proposes the Transformer, removing recurrence from seq2seq | 120000 |
| ... | ... | ... | ... | ... |

### Papers citing this paper (forward, M, by citation count)

| Year | Title | Authors | One-line summary | Citations |
|------|-------|---------|------------------|-----------|
| ... | ... | ... | ... | ... |
```

- With `--direction backward`, omit the forward table; with `--direction forward`, omit the backward table.
- Abbreviate authors as "First et al." beyond three names.
- For a direction with zero results, state "(none)" explicitly (do not leave an empty table).

### Step 4: Save Note (`--save` only)

Execute only if `--save` is specified:

1. Write the generated overview (the full Step 3 Markdown) to a tempfile
2. Run the following with the Bash tool:

```
cd ~/zotero-cli && ./zotero-cli add-note <itemKey> --body-file <tempfile> --tags "ai-citations"
```

3. Delete the tempfile
4. Display a save confirmation message

If `--save` is not specified, simply display the overview and exit.
