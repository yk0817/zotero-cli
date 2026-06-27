---
description: 論文の引用ネットワーク（引用文献・被引用論文）を Semantic Scholar から取得し、各文献に一言サマリを付けて俯瞰表にまとめる。--saveでZoteroにノート保存。
user-invocable: true
---

# 引用ネットワーク スキル

論文のitemKeyと引数が `$ARGUMENTS` で渡される。指定論文が **引用している文献（backward）** と、その論文を **引用している論文（forward）** を一覧化し、各文献に一言サマリを付けて俯瞰表にまとめる。

外部 API は **Semantic Scholar Graph API** を使う。CLI 側で叩くので API キーは不要（あれば `SEMANTIC_SCHOLAR_API_KEY` 環境変数でレート上限が上がる）。

## 引数のパース

`$ARGUMENTS` を以下のルールでパースせよ：

- 最初のトークン（または `--` 始まりでない連続トークン）→ `<query>`（必須）
- `--direction <dir>` → `backward`（引用文献のみ） / `forward`（被引用のみ） / `both`（両方、デフォルト）
- `--limit <N>` → 各方向の最大取得件数（省略時は 25）
- `--save` → 結果を Zotero にノート保存（タグ `ai-citations`）
- `<query>` が見つからない場合はエラーメッセージを出して終了

### queryの判定

`<query>` がitemKey形式（英数字ちょうど8文字、例: `FQVL7ZHM`）かどうかを判定する：

- **itemKey形式に一致** → そのまま `<itemKey>` として使用
- **一致しない** → タイトル/キーワード検索として扱う（Step 1a）

## 手順

### Step 1a: itemKeyの解決

**queryがitemKey形式の場合：** そのまま `<itemKey>` として Step 2 へ進む。

**queryがキーワードの場合：** Bashツールで以下を実行：

```
cd ~/zotero-cli && ./zotero-cli search <query>
```

検索結果に応じて分岐：

- **0件** → 「検索結果が見つかりませんでした: `<query>`」と表示して終了
- **1件** → その itemKey を自動採用し Step 2 へ
- **複数件** → 候補一覧を番号付きで表示し、AskUserQuestion ツールでユーザーに選択してもらう。選択された itemKey で Step 2 へ

候補一覧の表示形式：

```
検索結果: <N>件見つかりました。番号を入力して選択してください。

  1. [FQVL7ZHM] Investigating Environmental, Social, and Governance... (Angioni et al.)
  2. [99NU4NKK] An automated information extraction system... (Mohsin et al., 2024)
  ...
```

### Step 2: 引用ネットワークの取得

Bashツールで以下を実行（`<dir>` `<N>` は引数で解決した値）：

```
cd ~/zotero-cli && ./zotero-cli citations <itemKey> --direction <dir> --limit <N> --output json
```

レスポンスは `{"ok":true,"data":{...}}` 形式。`data` は次の構造：

- `title` — 対象論文のタイトル
- `paperId` — Semantic Scholar 上で同定された論文ID
- `backward` — この論文が引用している文献の配列（`--direction forward` の場合は空）
- `forward` — この論文を引用している論文の配列（被引用数降順。`--direction backward` の場合は空）

各文献は `title` / `year` / `authors`（`name` の配列）/ `citationCount` / `abstract` / `externalIds` を持つ。

**エラー時の挙動：**

- `{"ok":false,"error":{"code":"NOT_FOUND",...}}` → Semantic Scholar で論文を同定できなかった（DOI・arXiv ID・タイトルのいずれも一致せず）。その旨を表示して終了（バグではなく、メタデータ不足が原因の旨を添える）。
- `{"ok":false,"error":{"code":"API_ERROR",...}}` → API 障害・レート制限。メッセージを表示して終了。
- `backward` / `forward` が空配列 → 「該当なし」。エラーではない（Semantic Scholar に登録がない論文では正常に起こりうる）。

### Step 3: 俯瞰表の生成

取得した JSON を読み、各文献に **一言サマリ**（タイトル・アブストラクトから1文で要点を）を付けて、**日本語で**以下の俯瞰表にまとめる。アブストラクトが無い文献はタイトルから推測した要点を書く（推測である旨は不要、簡潔に）。

```
## 引用ネットワーク: <Title>

Semantic Scholar 同定ID: `<paperId>`

### この論文が引用している主要文献（backward, N件）

| 年 | タイトル | 著者 | 一言サマリ | 被引用数 |
|----|---------|------|-----------|---------|
| 2017 | Attention Is All You Need | Vaswani et al. | Transformer を提案し系列変換から再帰を排除 | 120000 |
| ... | ... | ... | ... | ... |

### この論文を引用している論文（forward, M件・被引用数降順）

| 年 | タイトル | 著者 | 一言サマリ | 被引用数 |
|----|---------|------|-----------|---------|
| ... | ... | ... | ... | ... |
```

- `--direction backward` 指定時は forward 表を省略、`--direction forward` 指定時は backward 表を省略する。
- 著者は3名超なら「First et al.」と省略してよい。
- 件数が0の方向は「（該当なし）」と明記する（空表にしない）。

### Step 4: ノート保存（`--save` 指定時のみ）

`--save` が指定されている場合のみ実行：

1. 生成した俯瞰表（Step 3 の Markdown 全体）をtempfileに書き出す
2. Bashツールで以下を実行：

```
cd ~/zotero-cli && ./zotero-cli add-note <itemKey> --body-file <tempfile> --tags "ai-citations"
```

3. tempfileを削除
4. 保存完了メッセージを表示

`--save` が指定されていない場合は、俯瞰表を表示するだけで終了。
