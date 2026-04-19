---
description: 論文の批判的読解。強み・弱み・方法論の妥当性・研究ギャップを体系的に分析する。--saveでZoteroにノート保存。
user-invocable: true
---

# 論文批判的読解スキル

論文のitemKeyと引数が `$ARGUMENTS` で渡される。

## 引数のパース

`$ARGUMENTS` を以下のルールでパースせよ：

- 最初のトークン（または `--save` / `--perspective` 以外の連続トークン）→ `<query>`（必須）
- `--save` → 分析後にZoteroへノート保存
- `--perspective <text>` → 特定の観点に焦点を当てた分析（省略時は全般的な批判的分析）
- `<query>` が見つからない場合はエラーメッセージを出して終了

### queryの判定

`<query>` がitemKey形式（英数字ちょうど8文字、例: `FQVL7ZHM`）かどうかを判定する：

- **itemKey形式に一致** → そのまま `<itemKey>` として使用
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

### Step 2: 批判的分析の生成

取得したJSONから論文情報を読み取り、以下の構造で**日本語で**批判的分析を生成する。

`--perspective` が指定されている場合は、その観点を重視して分析する。

```
## 🔍 批判的分析: <タイトル>

**著者:** <著者リスト>
**出版年:** <年>
**ジャーナル/会議:** <出版先>

### 研究の概要
（1-2文で研究の目的と主要な貢献を要約）

### 強み
1. **<強み1のタイトル>** — （説明）
2. **<強み2のタイトル>** — （説明）
3. **<強み3のタイトル>** — （説明）

### 弱み・限界
1. **<弱み1のタイトル>** — （説明）
2. **<弱み2のタイトル>** — （説明）
3. **<弱み3のタイトル>** — （説明）

### 方法論の妥当性
- **研究デザイン:** （適切性の評価）
- **データ:** （データセットの規模・質・代表性）
- **分析手法:** （手法選択の妥当性）
- **再現性:** （再現可能性の評価）

### 研究ギャップと今後の方向性
- （この研究が埋めきれていないギャップ）
- （今後の研究への示唆）

### 自身の研究への示唆
- （自分の研究にどう活かせるか、注意すべき点）
```

### Step 3: ノート保存（`--save` 指定時のみ）

`--save` が指定されている場合のみ実行：

1. 生成した分析をtempfileに書き出す
2. Bashツールで以下を実行：

```
cd ~/zotero-cli && ./zotero-cli add-note <itemKey> --body-file <tempfile> --tags "ai-critique"
```

3. tempfileを削除
4. 保存完了メッセージを表示

`--save` が指定されていない場合は、分析を表示するだけで終了。
