---
description: サーベイ表自動生成。コレクション・タグ・指定論文から文献整理テーブルをMarkdownで一括生成。--saveでZoteroにノート保存。
user-invocable: true
---

# サーベイ表自動生成スキル

引数が `$ARGUMENTS` で渡される。

## 引数のパース

`$ARGUMENTS` を以下のルールでパースせよ：

- `--tag <tag>` → 指定タグの論文を対象にする
- `--collection <collectionKey>` → 指定コレクションの論文を対象にする
- `--keys "<key1>,<key2>,..."` → カンマ区切りのitemKeyリストで対象を指定
- `--columns "<col1>,<col2>,..."` → テーブルのカラムをカスタム指定（省略時はデフォルトカラム）
- `--limit <N>` → 対象論文数の上限（デフォルト: 20）
- `--save` → 生成した表をZoteroにノート保存（最初の論文のノートとして保存）
- 上記のいずれの指定もない場合は、`$ARGUMENTS` 全体をキーワード検索として扱う

対象指定が見つからない場合はエラーメッセージを出して終了。

## 手順

### Step 1: 論文リストの取得

指定方法に応じてBashツールで以下を実行：

**タグ指定の場合：**

```
cd ~/zotero-cli && ./zotero-cli export --tag <tag> --format json --limit <limit>
```

**コレクション指定の場合：**

```
cd ~/zotero-cli && ./zotero-cli export --collection <collectionKey> --format json --limit <limit>
```

**itemKey指定の場合：**

```
cd ~/zotero-cli && ./zotero-cli export --keys "<key1>,<key2>,..." --format json
```

**キーワード検索の場合：**

```
cd ~/zotero-cli && ./zotero-cli search <query>
```

検索結果に応じて分岐：

- **0件** → 「検索結果が見つかりませんでした: `<query>`」と表示して終了
- **複数件** → 候補一覧を表示し、AskUserQuestion ツールでユーザーに対象の番号を複数選択してもらう

選択後、各itemKeyについて `context --json` で詳細を取得する。

コマンドが失敗した場合はエラーを表示して終了。結果が0件の場合は「対象の論文が見つかりませんでした」と表示して終了。

### Step 2: サーベイ表の生成

取得したJSONから各論文の情報を読み取り、**日本語で**Markdownテーブルを生成する。

#### デフォルトカラム

`--columns` 未指定時は以下のカラムを使用：

```
## 📊 サーベイ表

**対象:** <タグ名/コレクション名/検索キーワード>
**論文数:** <N>本

| 著者・年 | タイトル | 研究目的 | 手法 | データ | 主要結果 |
|----------|---------|---------|------|--------|---------|
| Author1 et al. (2024) | Title1 | ... | ... | ... | ... |
| Author2 et al. (2023) | Title2 | ... | ... | ... | ... |
| ... | ... | ... | ... | ... | ... |

### 傾向と気づき
- （論文群全体から見える傾向）
- （主流の手法・データセット）
- （時系列での変化があれば）
```

#### カスタムカラム

`--columns` 指定時は、指定されたカラムでテーブルを構成する。「著者・年」と「タイトル」は常に先頭に含める。

例: `--columns "手法,データ,精度"` の場合：

```
| 著者・年 | タイトル | 手法 | データ | 精度 |
```

### Step 3: ノート保存（`--save` 指定時のみ）

`--save` が指定されている場合のみ実行：

1. 生成したサーベイ表をtempfileに書き出す
2. 対象論文の**最初のitemKey**に対して、Bashツールで以下を実行：

```
cd ~/zotero-cli && ./zotero-cli add-note <firstItemKey> --body-file <tempfile> --tags "ai-survey-table"
```

3. tempfileを削除
4. 保存完了メッセージを表示

`--save` が指定されていない場合は、サーベイ表を表示するだけで終了。
