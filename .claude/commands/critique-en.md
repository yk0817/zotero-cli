---
description: Critical reading of a paper. Systematically analyze strengths, weaknesses, methodological validity, and research gaps. Use --save to save as a Zotero note.
user-invocable: true
---

# Critical Paper Analysis Skill

The paper's itemKey and arguments are passed via `$ARGUMENTS`.

## Argument Parsing

Parse `$ARGUMENTS` with the following rules:

- First token (or consecutive tokens other than `--save` / `--perspective`) → `<query>` (required)
- `--save` → Save the analysis as a Zotero note
- `--perspective <text>` → Focus on a specific aspect (omit for general critical analysis)
- If no `<query>` is found, display an error message and exit

### Query Classification

Determine whether `<query>` matches itemKey format (exactly 8 alphanumeric characters, e.g., `FQVL7ZHM`):

- **Matches itemKey format** → Use directly as `<itemKey>`
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

### Step 2: Generate Critical Analysis

Read the paper information from the retrieved JSON and generate a critical analysis **in English** with the following structure.

If `--perspective` is specified, emphasize that aspect in the analysis.

```
## Critical Analysis: <Title>

**Authors:** <Author list>
**Year:** <Year>
**Journal/Conference:** <Venue>

### Research Overview
(Summarize the research objective and main contributions in 1-2 sentences)

### Strengths
1. **<Strength 1 title>** — (explanation)
2. **<Strength 2 title>** — (explanation)
3. **<Strength 3 title>** — (explanation)

### Weaknesses & Limitations
1. **<Weakness 1 title>** — (explanation)
2. **<Weakness 2 title>** — (explanation)
3. **<Weakness 3 title>** — (explanation)

### Methodological Validity
- **Research Design:** (assessment of appropriateness)
- **Data:** (dataset size, quality, representativeness)
- **Analysis Methods:** (validity of method choices)
- **Reproducibility:** (assessment of reproducibility)

### Research Gaps & Future Directions
- (Gaps not fully addressed by this research)
- (Implications for future research)

### Implications for My Research
- (How to leverage for own research, points to be aware of)
```

### Step 3: Save Note (`--save` only)

Execute only if `--save` is specified:

1. Write the generated analysis to a tempfile
2. Run the following with the Bash tool:

```
cd ~/zotero-cli && ./zotero-cli add-note <itemKey> --body-file <tempfile> --tags "ai-critique"
```

3. Delete the tempfile
4. Display a save confirmation message

If `--save` is not specified, simply display the analysis and exit.
