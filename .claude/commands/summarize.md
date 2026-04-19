---
description: Zoteroの論文を要約する。デフォルトは落合陽一式6項目要約。--saveでZoteroにノート保存。
user-invocable: true
---

# 論文要約スキル

論文のitemKeyと引数が `$ARGUMENTS` で渡される。

## 引数のパース

`$ARGUMENTS` を以下のルールでパースせよ：

- 最初のトークン（または `--save` / `--format` 以外の連続トークン）→ `<query>`（必須）
- `--save` → 要約後にZoteroへノート保存
- `--format <name>` → 要約フォーマット指定（省略時は `ochiai`）
  - `ochiai` — 落合陽一式6項目要約（デフォルト）
  - `brief` — 簡潔要約
  - `abstract` — 構造化アブストラクト
  - `custom "..."` — `--format custom` の次のクォート文字列をカスタムプロンプトとして使用
- `<query>` が見つからない場合はエラーメッセージを出して終了

### queryの判定

`<query>` がitemKey形式（英数字ちょうど8文字、例: `FQVL7ZHM`）かどうかを判定する：

- **itemKey形式に一致** → そのまま `<itemKey>` として使用（従来動作）
- **一致しない** → タイトル/キーワード検索として扱う（次のStep 1aで検索）

## 手順

### Step 1a: itemKeyの解決

**queryがitemKey形式の場合：**

そのまま `<itemKey>` として Step 1b へ進む。

**queryがキーワードの場合：**

Bashツールで以下を実行：

```
cd ~/zotero-cli && ./zotero-cli search <query>
```

検索結果に応じて分岐：

- **0件** → 「検索結果が見つかりませんでした: `<query>`」と表示して終了
- **1件** → その itemKey を自動的に採用し、Step 1b へ進む
- **複数件** → 候補一覧を番号付きで表示し、AskUserQuestion ツールでユーザーに番号を選択してもらう。選択された itemKey で Step 1b へ進む

候補一覧の表示形式：

```
検索結果: <N>件見つかりました。番号を入力して選択してください。

  1. [FQVL7ZHM] Investigating Environmental, Social, and Governance... (Angioni et al.)
  2. [99NU4NKK] An automated information extraction system... (Mohsin et al., 2024)
  ...
```

### Step 1b: 論文情報の取得

Bashツールで以下を実行：

```
cd ~/zotero-cli && ./zotero-cli context <itemKey> --json --with-notes
```

コマンドが失敗した場合はエラーを表示して終了。

### Step 2: 要約の生成

取得したJSONから論文情報を読み取り、指定フォーマットに従って**日本語で**要約を生成する。

#### フォーマット: `ochiai`（デフォルト）

以下の6項目で要約：

```
## 📄 論文要約: <タイトル>

**著者:** <著者リスト>
**出版年:** <年>
**ジャーナル/会議:** <出版先>

### 1. どんなもの？
（この研究が何をしたかを簡潔に説明）

### 2. 先行研究と比べてどこがすごい？
（新規性・差別化ポイント）

### 3. 技術や手法のキモはどこ？
（コアとなる技術的アプローチ）

### 4. どうやって有効だと検証した？
（実験設定・データセット・評価指標・主要結果）

### 5. 議論はある？
（限界・今後の課題・著者の考察）

### 6. 次に読むべき論文は？
（引用されている重要な関連論文を2-3本）
```

#### フォーマット: `brief`

```
## 📄 論文要約: <タイトル>

**著者:** <著者リスト> | **出版年:** <年>

- **概要:** （1-2文）
- **手法:** （1-2文）
- **結果:** （1-2文）
- **意義:** （1-2文）
```

#### フォーマット: `abstract`

```
## 📄 論文要約: <タイトル>

**著者:** <著者リスト> | **出版年:** <年>

### Background
（研究背景と目的）

### Method
（手法の説明）

### Results
（主要な結果）

### Conclusion
（結論と意義）
```

#### フォーマット: `custom`

ユーザー指定のカスタムプロンプトに従って自由形式で要約。

### Step 3: ノート保存（`--save` 指定時のみ）

`--save` が指定されている場合のみ実行：

1. 生成した要約をtempfileに書き出す
2. Bashツールで以下を実行：

```
cd ~/zotero-cli && ./zotero-cli add-note <itemKey> --body-file <tempfile> --tags "ai-summary"
```

3. tempfileを削除
4. 保存完了メッセージを表示

`--save` が指定されていない場合は、要約を表示するだけで終了。
