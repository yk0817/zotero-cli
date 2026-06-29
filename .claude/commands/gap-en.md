---
description: Identify open research gaps and candidate research questions across a set of papers (collection, tag, or multiple item keys). Use --save to save as a Zotero note.
user-invocable: true
---

# Research Gap Extraction Skill

Arguments are passed via `$ARGUMENTS`.

Identify **open research gaps** and **candidate research questions** across a set of papers (collection / tag / multiple item keys). This is a different axis from `survey-table` (organizing literature into a table) and `related-work` (writing a related work prose draft): the goal here is to surface "what should be asked next."

## Argument Parsing

Parse `$ARGUMENTS` with the following rules:

- `--tag <tag>` → Target papers with the specified tag
- `--collection <collectionKey>` → Target papers in the specified collection
- `--keys "<key1>,<key2>,..."` → Target papers by comma-separated itemKey list
- `--limit <N>` → Maximum number of target papers (default: 20)
- `--save` → Save the generated gap analysis as a Zotero note
- If none of the above are specified, treat the entire `$ARGUMENTS` as a keyword search

If no target specification is found, display an error message and exit.

## Procedure

### Step 1: Retrieve Target Papers

Run the following with the Bash tool based on the specification method (same input-collection method as `survey-table`):

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

If the command fails, display the error and exit. If 0 results, display "No target papers found" and exit. A gap analysis ideally needs **at least 2 papers**. If only 1 is found, you may proceed but note that "cross-paper analysis benefits from multiple papers."

### Step 2: Internally Understand Each Paper

From the retrieved JSON (and `context --json` as needed), internally capture the following for each paper (do not output at this stage):

- Research objective / problem addressed
- Method and data
- Main contributions and explicitly stated limitations (Limitations / Future Work)
- What was solved, and what remains open

Keep track of where you inferred beyond the text so you can mark it as "speculation" in the output. Do not fabricate.

### Step 3: Generate Gap Analysis Output

Using the retrieved paper information, output the following structure **in English**. Tie each claim to the target papers; mark anything not readable from the papers as "(speculation)". Do not fabricate.

```
## Research Gap Analysis

**Target:** <tag name / collection name / search keyword>
**Papers:** <N>

### Target Papers

1. Author et al. (Year) — Title
2. Author et al. (Year) — Title
... (N papers)

### Common Theme / Current State of the Art

<Summarize in 2-4 sentences what is known and how far the field has progressed in the area these papers cover.>

### Research Gaps

- **<Gap 1 heading>** — Why it is unresolved; which paper went how far (e.g., "Author (Year) showed X but did not verify Y").
- **<Gap 2 heading>** — Same.
- **<Gap 3 heading>** — Same.
(Add or remove as needed. Each gap must include "why unresolved / which paper went how far".)

### Candidate Research Questions

3-5 questions to ask next, each corresponding to a gap, with a brief aim.

1. **RQ1:** <question> — Aim: <one line>
2. **RQ2:** <question> — Aim: <one line>
3. **RQ3:** <question> — Aim: <one line>
(Up to 5.)

### Approach Hints

A one-line direction of a possible method for each RQ.

- **RQ1:** <method direction (e.g., apply X on dataset Y)>
- **RQ2:** <method direction>
- **RQ3:** <method direction>
```

Notes:

- Always tie evidence to the target papers. Mark any reasoning not stated in the papers as "(speculation)".
- Do not fabricate non-existent papers, numbers, or results.
- Map gaps to RQs so the reader can trace which gap each RQ addresses.

### Step 4: Save Note (`--save` only)

Execute only if `--save` is specified:

1. Write the generated gap analysis to a tempfile.
2. **Determine the target itemKey** (note: multiple papers are targeted):
   - If the itemKey is unambiguous (single `--keys` entry, or a single search selection), save to that itemKey.
   - If multiple papers are targeted, the save destination is not unique. Use AskUserQuestion to ask the user which paper's note to save it under (present the target paper list as options). If the user does not respond or defers, save to the **first itemKey** as the representative of the set.
3. For the determined itemKey, run the following with the Bash tool:

```
cd ~/zotero-cli && ./zotero-cli add-note <targetItemKey> --body-file <tempfile> --tags "ai-gap"
```

4. Delete the tempfile.
5. Display a save confirmation message (include the saved paper's title and itemKey).

If `--save` is not specified, simply display the gap analysis and exit.
