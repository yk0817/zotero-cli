---
description: 関連研究セクション生成。論文群から関連研究のドラフトを日本語/英語で自動生成。--saveでZoteroにノート保存。
user-invocable: true
---

# 関連研究セクション生成スキル

引数が `$ARGUMENTS` で渡される。

## 引数のパース

`$ARGUMENTS` を以下のルールでパースせよ：

- `--tag <tag>` → 指定タグの論文を対象にする
- `--collection <collectionKey>` → 指定コレクションの論文を対象にする
- `--keys "<key1>,<key2>,..."` → カンマ区切りのitemKeyリストで対象を指定
- `--theme <text>` → 関連研究セクションのテーマ・文脈を指定（省略時は論文群から自動推定）
- `--lang <ja|en>` → 出力言語（デフォルト: `ja`）
- `--limit <N>` → 対象論文数の上限（デフォルト: 20）
- `--save` → 生成したセクションをZoteroにノート保存（最初の論文のノートとして保存）
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

### Step 2: BibTeXキーの取得

対象論文のBibTeXキーを取得するため、Bashツールで以下を実行：

```
cd ~/zotero-cli && ./zotero-cli bibtex --keys "<key1>,<key2>,..."
```

各論文のBibTeXキー（`@article{bibkey, ...}` の `bibkey` 部分）を抽出し、itemKeyとの対応表を作成する。

### Step 3: 関連研究セクションの生成

取得した論文情報とBibTeXキーを使い、指定言語で関連研究セクションのドラフトを生成する。

#### テーマ分類

論文群を内容に基づいて2〜4のサブテーマにグルーピングする。`--theme` が指定されている場合は、そのテーマの文脈で分類する。

#### 出力フォーマット

**日本語（`--lang ja`、デフォルト）：**

```
## 📝 関連研究

**テーマ:** <テーマ（自動推定または指定）>
**対象論文数:** <N>本

### <サブテーマ1のタイトル>

<サブテーマ1に属する論文群を引用しながら、研究の流れ・発展を記述する。>
<著者名 \cite{bibkey1} は...。一方、\cite{bibkey2} では...。>

### <サブテーマ2のタイトル>

<サブテーマ2に属する論文群を同様に記述。>

### <サブテーマ3のタイトル>（必要に応じて）

<サブテーマ3に属する論文群を同様に記述。>

### まとめと研究の位置づけ

<既存研究の全体像を要約し、残された課題や本研究の位置づけを述べる。>
```

**英語（`--lang en`）：**

同じ構造で英語の学術的文体で記述する。セクションタイトルは `Related Work`、サブセクションも英語にする。

#### 引用形式

- 本文中の引用は `\cite{bibkey}` 形式を使用する
- BibTeXキーが取得できなかった論文は `(著者名, 年)` 形式で引用する

### Step 4: ノート保存（`--save` 指定時のみ）

`--save` が指定されている場合のみ実行：

1. 生成した関連研究セクションをtempfileに書き出す
2. 対象論文の**最初のitemKey**に対して、Bashツールで以下を実行：

```
cd ~/zotero-cli && ./zotero-cli add-note <firstItemKey> --body-file <tempfile> --tags "ai-related-work"
```

3. tempfileを削除
4. 保存完了メッセージを表示

`--save` が指定されていない場合は、関連研究セクションを表示するだけで終了。
