---
description: Compare multiple papers. Organize methods, results, and contributions in a comparison table. Use --save to save as Zotero notes.
user-invocable: true
---

# Paper Comparison Analysis Skill

Multiple paper itemKeys or keywords and arguments are passed via `$ARGUMENTS`.

## Argument Parsing

Parse `$ARGUMENTS` with the following rules:

- Multiple tokens matching itemKey format (exactly 8 alphanumeric characters) → list of `<itemKeys>`
- Or, consecutive tokens other than `--save` / `--focus` / `--limit` → `<query>` (keyword search)
- `--save` → Save comparison results as Zotero notes (saved to each paper)
- `--focus <aspect>` → Comparison focus (e.g., `method`, `data`, `evaluation`). Omit for general comparison
- `--limit <N>` → Number of papers to compare from keyword search (default: select from results)
- If neither itemKeys nor query are found, display an error message and exit
- If only one itemKey is specified, display "At least 2 papers are required for comparison" and exit

## Procedure

### Step 1a: Resolve itemKeys

**If multiple itemKeys are directly specified:**

Use them as the `<itemKeys>` list and proceed to Step 1b.

**If query is a keyword:**

Run the following with the Bash tool:

```
cd ~/zotero-cli && ./zotero-cli search <query>
```

Branch based on results:

- **0 results** → Display "No results found for: `<query>`" and exit
- **1 result** → Display "At least 2 papers are required for comparison" and exit
- **Multiple results** →
  - If `--limit <N>` is specified → Automatically adopt the top N results
  - If `--limit` is not specified → Display a numbered list and use AskUserQuestion to let the user select multiple numbers (e.g., `1,3,5` or space-separated)

Candidate list format:

```
Search results: <N> items found. Enter paper numbers separated by commas to compare (e.g., 1,3,5).

  1. [FQVL7ZHM] Investigating Environmental, Social, and Governance... (Angioni et al.)
  2. [99NU4NKK] An automated information extraction system... (Mohsin et al., 2024)
  ...
```

### Step 1b: Retrieve Paper Information

For each selected itemKey, run the following with the Bash tool (in parallel when possible):

```
cd ~/zotero-cli && ./zotero-cli context <itemKey> --json
```

If any command fails, report the failed itemKey and continue comparison with successful papers only. If fewer than 2 papers succeed, display an error and exit.

### Step 2: Generate Comparison Analysis

Read the paper information from the retrieved JSON and generate a comparison analysis **in English** with the following structure.

If `--focus` is specified, emphasize that aspect in the comparison.

```
## Paper Comparison Analysis

### Papers Compared

| # | Paper | Authors | Year |
|---|-------|---------|------|
| 1 | <Title 1> | <Authors 1> | <Year 1> |
| 2 | <Title 2> | <Authors 2> | <Year 2> |
| ... | ... | ... | ... |

### Comparison Table

| Aspect | Paper 1 | Paper 2 | ... |
|--------|---------|---------|-----|
| **Research Objective** | ... | ... | ... |
| **Method** | ... | ... | ... |
| **Dataset** | ... | ... | ... |
| **Evaluation Metrics** | ... | ... | ... |
| **Key Results** | ... | ... | ... |
| **Limitations** | ... | ... | ... |

### Overall Analysis

#### Commonalities
- (Shared approaches and findings across papers)

#### Differences
- (Key differences and contrasting approaches)

#### Research Gaps
- (Areas not fully covered by these papers)

#### Implications
- (Insights from the comparison, implications for own research)
```

### Step 3: Save Notes (`--save` only)

Execute only if `--save` is specified:

1. Write the generated comparison analysis to a tempfile
2. For **each itemKey** in the comparison, run the following with the Bash tool:

```
cd ~/zotero-cli && ./zotero-cli add-note <itemKey> --body-file <tempfile> --tags "ai-compare"
```

3. Delete the tempfile
4. Display a save confirmation message (listing each itemKey saved to)

If `--save` is not specified, simply display the comparison analysis and exit.
