---
description: Close-reading discussion of a paper. Discuss interactively around your own Zotero highlights and comments. Use --color to focus on marks of a specific color.
user-invocable: true
---

# Paper Discussion Skill

The paper's itemKey and arguments are passed via `$ARGUMENTS`.

## Argument Parsing

Parse `$ARGUMENTS` with the following rules:

- First token (or consecutive tokens other than `--color`) → `<query>` (required)
- `--color <hex>` → Focus the discussion on annotations of the given color (e.g., `--color "#ff0000"`)
- If no `<query>` is found, display an error message and exit

### Query Classification

Determine whether `<query>` matches itemKey format (exactly 8 alphanumeric characters, e.g., `FQVL7ZHM`):

- **Matches itemKey format** → Use directly as `<itemKey>`
- **Does not match** → Treat as title/keyword search (proceed to Step 1a search)

## Procedure

### Step 1a: Resolve itemKey

**If query is in itemKey format:**

Use it directly as `<itemKey>` and proceed to Step 1b.

**If query is a keyword:**

Run via the Bash tool:

```
cd ~/zotero-cli && ./zotero-cli search <query>
```

Branch on the search results:

- **0 results** → Display "No search results found: `<query>`" and exit
- **1 result** → Automatically adopt that itemKey and proceed to Step 1b
- **Multiple results** → Display a numbered candidate list and ask the user to select one via the AskUserQuestion tool. Proceed to Step 1b with the selected itemKey

### Step 1b: Fetch Context

Run via the Bash tool (returns metadata + full text + annotations + notes in one call):

```
cd ~/zotero-cli && ./zotero-cli context <itemKey> --output json
```

If `--color` was given, additionally run the following and use these annotations as the discussion axis:

```
cd ~/zotero-cli && ./zotero-cli annotations <itemKey> --color "<hex>" --output json
```

If the command fails, display the error and exit.

### Step 2: Check Annotations

Inspect the returned `annotations`:

- **0 annotations** → Inform the user: "This paper has no synced annotations. Highlight it in Zotero and sync, or we can start a discussion based on the full text instead" and ask how to proceed
- **1 or more** → Proceed to Step 3

Handling by annotation type:

- `highlight` / `underline` — `annotationText` is the selected passage; `annotationComment` is the user's comment if present
- `note` — `annotationComment` is the body (a note placed on the page)
- `ink` / `image` — No text. State explicitly "handwritten (text not readable) — p.N" and read the surrounding pages in the full text to infer and explain the context

### Step 3: Present the Discussion

This is **distinct** from the Ochiai-style summary (/summarize-en). Build a close-reading starting point **in English** around the user's marked passages:

```
## 💬 Close-Reading Discussion: <Title>

**Authors:** <author list> | **Year:** <year>
**Annotations:** <N> items (highlight: x, note: y, ink: z)

### Reading Your Marked Passages

#### 1. [p.<page>] "<highlighted text (quote, abridged if long)>"
- **Why this matters:** (the role of this passage in the paper's overall argument)
- **Response to your comment:** (only if annotationComment exists — respond, supplement, or push back)
- **Related passages:** (connect to related discussion, equations, or results from the full text)

#### 2. ... (cover all annotations in sortIndex order)

#### N. [p.<page>] Handwritten annotation (text not readable)
- This page discusses (summary of the page's content from the full text). Let me know what your handwritten note was about if you recall.

### Discussion Starters

(From the distribution of marks and the tone of comments, raise 2-3 themes the user
 seems interested in, each ending with a question back to the user)
```

### Step 4: Continue the Dialogue

After presenting, continue the conversation following the user's questions and reactions. Ground answers in the full text, distinguishing facts from inference. If something is not in the paper, say so explicitly.
