---
description: Extract reproducibility-focused method details (experimental setup, datasets, metrics, hyperparameters, baselines) from a paper into a structured table. Use --save to save as a Zotero note.
user-invocable: true
---

# Reproducibility Method Extraction Skill

The paper's itemKey and arguments are passed via `$ARGUMENTS`.

This skill is a different axis from a "narrative summary" (summarize): it extracts **only the facts needed to reproduce the experiments** from the paper's full text. Exclude impressions, evaluations, and storytelling; collect facts that fit in a table (dataset names, sizes, splits, hyperparameters, evaluation metrics, representative numbers, compute resources, code release URLs, etc.).

## Argument Parsing

Parse `$ARGUMENTS` with the following rules:

- First token (or consecutive tokens other than `--save`) → `<query>` (required)
- `--save` → Save the extraction as a Zotero note
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

### Step 1b: Retrieve Paper Information (Metadata)

Run the following with the Bash tool:

```
cd ~/zotero-cli && ./zotero-cli context <itemKey> --json --with-notes
```

If the command fails, display the error and exit. This retrieves metadata such as title, authors, year, venue, and abstract.

### Step 1c: Retrieve Full Text

Reproducibility method extraction requires the **full text**. Run the following with the Bash tool:

```
cd ~/zotero-cli && ./zotero-cli fulltext <itemKey>
```

- **If full text is retrieved** → Use it as the primary source for the Step 2 extraction.
- **If full text cannot be retrieved** (no attached PDF / not synced to Zotero / empty full-text index) → Extract only what is available from the context abstract obtained in Step 1b, and prepend the following note to the output:

  ```
  > ⚠️ Full text could not be retrieved (likely no attached PDF / not synced), so the following is extracted only from the abstract and available metadata. Many fields are estimated or unknown.
  ```

If the full text is long, do not truncate with `--max-chars`; retrieve the whole text first and prioritize reading the methods, experiments, and evaluation sections.

### Step 2: Generate the Reproducibility Method Table

From the retrieved full text (or metadata if unavailable), extract **in English** with the following structure. **Do not fabricate** — for any field not stated in the text, explicitly write "Unknown / N/A". Write only facts grounded in the text wherever possible.

```
## 🔬 Reproducibility Method Extraction: <Title>

**Authors:** <Author list>
**Year:** <Year>
**Venue:** <Journal/Conference>

| Field | Content |
|-------|---------|
| **Task / Problem Setup** | (What is the input and output of the problem? Classification/generation/prediction/retrieval, etc.) |
| **Datasets** | (Name / size (item count, token count, etc.) / split (train/val/test) / preprocessing. Use a bullet list if multiple) |
| **Method / Model** | (Architecture / key components / base existing model) |
| **Hyperparameters** | (Learning rate / batch size / epochs / optimizer / scheduler / regularization, etc., where known. State explicitly when a value is unknown) |
| **Baselines / Comparison Methods** | (Methods/models used for comparison) |
| **Evaluation Metrics** | (Accuracy / F1 / BLEU / nDCG / RMSE, etc., metrics used) |
| **Main Results** | (Representative numbers. Proposed method vs. baseline key scores with concrete values) |
| **Compute Resources** | (GPU type/count / training time / memory, etc., where known) |
| **Reproducibility Notes** | (Code release URL / data availability / license / unknown points where settings are withheld, obstacles to reproduction) |
```

Notes:

- Details that do not fit in the table (e.g., per-dataset splits, or multiple experiments with different settings) may be added as bulleted sections below the table.
- Quote numbers exactly as stated in the text. If the paper has multiple settings, pick a representative one and note which setting it is.
- Distinguish "Unknown" from "Not applicable (N/A)" (the former = should be in the paper but cannot be read off; the latter = does not apply at all).

### Step 3: Save Note (`--save` only)

Execute only if `--save` is specified:

1. Write the generated extraction to a tempfile
2. Run the following with the Bash tool:

```
cd ~/zotero-cli && ./zotero-cli add-note <itemKey> --body-file <tempfile> --tags "ai-methods"
```

3. Delete the tempfile
4. Display a save confirmation message

If `--save` is not specified, simply display the extraction and exit.
