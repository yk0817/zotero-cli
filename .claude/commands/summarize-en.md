---
description: Summarize a paper. Default is the Ochiai-style 6-point summary. Use --save to save as a Zotero note.
user-invocable: true
---

# Paper Summary Skill

The paper's itemKey and arguments are passed via `$ARGUMENTS`.

## Argument Parsing

Parse `$ARGUMENTS` with the following rules:

- First token (or consecutive tokens other than `--save` / `--format`) → `<query>` (required)
- `--save` → Save the summary as a Zotero note
- `--format <name>` → Summary format (defaults to `ochiai` if omitted)
  - `ochiai` — Ochiai-style 6-point summary (default)
  - `brief` — Concise summary
  - `abstract` — Structured abstract
  - `custom "..."` — Use the quoted string following `--format custom` as a custom prompt
- If no `<query>` is found, display an error message and exit

### Query Classification

Determine whether `<query>` matches itemKey format (exactly 8 alphanumeric characters, e.g., `FQVL7ZHM`):

- **Matches itemKey format** → Use directly as `<itemKey>` (legacy behavior)
- **Does not match** → Treat as title/keyword search (proceed to Step 1a search)

## Procedure

### Step 1a: Resolve itemKey

**If query is in itemKey format:**

Use directly as `<itemKey>` and proceed to Step 1b.

**If query is a keyword:**

Run the following with the Bash tool:

```
cd ~/zotero-cli && ./zotero-cli search <query>
```

Branch based on results:

- **0 results** → Display "No results found for: `<query>`" and exit
- **1 result** → Automatically adopt that itemKey and proceed to Step 1b
- **Multiple results** → Display a numbered list of candidates and use AskUserQuestion to let the user select a number. Proceed to Step 1b with the selected itemKey

Candidate list format:

```
Search results: <N> items found. Enter a number to select.

  1. [FQVL7ZHM] Investigating Environmental, Social, and Governance... (Angioni et al.)
  2. [99NU4NKK] An automated information extraction system... (Mohsin et al., 2024)
  ...
```

### Step 1b: Retrieve Paper Information

Run the following with the Bash tool:

```
cd ~/zotero-cli && ./zotero-cli context <itemKey> --json --with-notes
```

If the command fails, display the error and exit.

### Step 2: Generate Summary

Read the paper information from the retrieved JSON and generate a summary **in English** following the specified format.

#### Format: `ochiai` (default)

Summarize with the following 6 points:

```
## 📄 Paper Summary: <Title>

**Authors:** <Author list>
**Year:** <Year>
**Journal/Conference:** <Venue>

### 1. What is it?
(Briefly explain what this research did)

### 2. What makes it better than prior work?
(Novelty and differentiating points)

### 3. What is the core of its technique/method?
(The core technical approach)

### 4. How was it validated?
(Experimental setup, datasets, evaluation metrics, key results)

### 5. Any discussion?
(Limitations, future work, the authors' discussion)

### 6. What papers should be read next?
(2-3 important related papers cited in the work)
```

#### Format: `brief`

```
## 📄 Paper Summary: <Title>

**Authors:** <Author list> | **Year:** <Year>

- **Overview:** (1-2 sentences)
- **Method:** (1-2 sentences)
- **Results:** (1-2 sentences)
- **Significance:** (1-2 sentences)
```

#### Format: `abstract`

```
## 📄 Paper Summary: <Title>

**Authors:** <Author list> | **Year:** <Year>

### Background
(Research background and objective)

### Method
(Description of the method)

### Results
(Key results)

### Conclusion
(Conclusion and significance)
```

#### Format: `custom`

Summarize freely following the user-specified custom prompt.

### Step 3: Save Note (`--save` only)

Execute only if `--save` is specified:

1. Write the generated summary to a tempfile
2. Run the following with the Bash tool:

```
cd ~/zotero-cli && ./zotero-cli add-note <itemKey> --body-file <tempfile> --tags "ai-summary"
```

3. Delete the tempfile
4. Display a save confirmation message

If `--save` is not specified, simply display the summary and exit.
