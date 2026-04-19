---
description: Generate a Related Work section. Auto-generate a related work draft from a set of papers with \cite{} references. Use --save to save as a Zotero note.
user-invocable: true
---

# Related Work Section Generation Skill

Arguments are passed via `$ARGUMENTS`.

## Argument Parsing

Parse `$ARGUMENTS` with the following rules:

- `--tag <tag>` → Target papers with the specified tag
- `--collection <collectionKey>` → Target papers in the specified collection
- `--keys "<key1>,<key2>,..."` → Target papers by comma-separated itemKey list
- `--theme <text>` → Theme/context for the related work section (auto-inferred from papers if omitted)
- `--lang <ja|en>` → Output language (default: `en`)
- `--limit <N>` → Maximum number of target papers (default: 20)
- `--save` → Save the generated section as a Zotero note (saved to the first paper's notes)
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

### Step 2: Retrieve BibTeX Keys

To obtain BibTeX keys for the target papers, run the following with the Bash tool:

```
cd ~/zotero-cli && ./zotero-cli bibtex --keys "<key1>,<key2>,..."
```

Extract each paper's BibTeX key (the `bibkey` part from `@article{bibkey, ...}`) and create a mapping between itemKeys and BibTeX keys.

### Step 3: Generate Related Work Section

Using the retrieved paper information and BibTeX keys, generate a related work section draft in the specified language.

#### Theme Classification

Group the papers into 2-4 sub-themes based on content. If `--theme` is specified, classify within that theme's context.

#### Output Format

**English (`--lang en`, default):**

```
## Related Work

**Theme:** <theme (auto-inferred or specified)>
**Papers:** <N>

### <Sub-theme 1 Title>

<Describe the papers belonging to sub-theme 1 with citations, covering the research flow and development.>
<Author1 \cite{bibkey1} proposed... Meanwhile, \cite{bibkey2} demonstrated...>

### <Sub-theme 2 Title>

<Describe the papers belonging to sub-theme 2 similarly.>

### <Sub-theme 3 Title> (if needed)

<Describe the papers belonging to sub-theme 3 similarly.>

### Summary and Research Positioning

<Summarize the overall landscape of existing research, remaining challenges, and positioning of the current study.>
```

**Japanese (`--lang ja`):**

Same structure written in Japanese academic style. Section titles use Japanese (e.g., `関連研究`, `まとめと研究の位置づけ`).

#### Citation Format

- Use `\cite{bibkey}` format for in-text citations
- For papers where BibTeX keys could not be retrieved, use `(Author, Year)` format

### Step 4: Save Note (`--save` only)

Execute only if `--save` is specified:

1. Write the generated related work section to a tempfile
2. For the **first itemKey** among the target papers, run the following with the Bash tool:

```
cd ~/zotero-cli && ./zotero-cli add-note <firstItemKey> --body-file <tempfile> --tags "ai-related-work"
```

3. Delete the tempfile
4. Display a save confirmation message

If `--save` is not specified, simply display the related work section and exit.
