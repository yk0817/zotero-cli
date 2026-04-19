---
description: Auto-generate a survey table. Create a Markdown literature review table from collections, tags, or specified papers. Use --save to save as a Zotero note.
user-invocable: true
---

# Survey Table Generation Skill

Arguments are passed via `$ARGUMENTS`.

## Argument Parsing

Parse `$ARGUMENTS` with the following rules:

- `--tag <tag>` → Target papers with the specified tag
- `--collection <collectionKey>` → Target papers in the specified collection
- `--keys "<key1>,<key2>,..."` → Target papers by comma-separated itemKey list
- `--columns "<col1>,<col2>,..."` → Custom table columns (default columns if omitted)
- `--limit <N>` → Maximum number of target papers (default: 20)
- `--save` → Save the generated table as a Zotero note (saved to the first paper's notes)
- If none of the above are specified, treat the entire `$ARGUMENTS` as a keyword search

If no target specification is found, display an error message and exit.

## Procedure

### Step 1: Retrieve Paper List

Run the following with the Bash tool based on the specification method:

**Tag specified:**

```
cd ~/zotero-cli && ./zotero-cli export --tag <tag> --format json --limit <limit>
```

**Collection specified:**

```
cd ~/zotero-cli && ./zotero-cli export --collection <collectionKey> --format json --limit <limit>
```

**itemKeys specified:**

```
cd ~/zotero-cli && ./zotero-cli export --keys "<key1>,<key2>,..." --format json
```

**Keyword search:**

```
cd ~/zotero-cli && ./zotero-cli search <query>
```

Branch based on results:

- **0 results** → Display "No results found for: `<query>`" and exit
- **Multiple results** → Display candidate list and use AskUserQuestion to let the user select multiple target numbers

After selection, retrieve details for each itemKey using `context --json`.

If the command fails, display the error and exit. If 0 results, display "No target papers found" and exit.

### Step 2: Generate Survey Table

Read the paper information from the retrieved JSON and generate a Markdown table **in English**.

#### Default Columns

When `--columns` is not specified, use the following columns:

```
## Survey Table

**Target:** <tag name / collection name / search keyword>
**Papers:** <N>

| Authors & Year | Title | Research Objective | Method | Data | Key Results |
|----------------|-------|--------------------|--------|------|-------------|
| Author1 et al. (2024) | Title1 | ... | ... | ... | ... |
| Author2 et al. (2023) | Title2 | ... | ... | ... | ... |
| ... | ... | ... | ... | ... | ... |

### Trends & Observations
- (Trends visible across the paper collection)
- (Dominant methods and datasets)
- (Chronological changes if any)
```

#### Custom Columns

When `--columns` is specified, compose the table with the specified columns. "Authors & Year" and "Title" are always included at the beginning.

Example: `--columns "method,data,accuracy"` results in:

```
| Authors & Year | Title | Method | Data | Accuracy |
```

### Step 3: Save Note (`--save` only)

Execute only if `--save` is specified:

1. Write the generated survey table to a tempfile
2. For the **first itemKey** among the target papers, run the following with the Bash tool:

```
cd ~/zotero-cli && ./zotero-cli add-note <firstItemKey> --body-file <tempfile> --tags "ai-survey-table"
```

3. Delete the tempfile
4. Display a save confirmation message

If `--save` is not specified, simply display the survey table and exit.
