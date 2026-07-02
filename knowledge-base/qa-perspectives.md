# テスト観点チェックリスト（zotero-cli）

新機能・修正のテストを書く前にこの表を読み、**該当する P1 観点には必ず最低1つのテストを紐付ける**。
テスト設計の自動実行は `/test-design` を使う（観点→テストの対応表を出力する）。

## 優先度の定義

| 優先度 | 基準 | 扱い |
|---|---|---|
| P1 | データ欠損・誤った成功報告・ライブラリ汚染につながる | テスト必須。欠けていたら PR をマージしない |
| P2 | 利用者（人間/LLM）の誤読・使い勝手に影響 | 原則テストを書く。見送る場合は PR に理由を書く |
| P3 | 表示の細部・スタイル | 任意 |

## P1: API 境界

- [ ] **リクエスト形状**: エンドポイントパス・クエリパラメータ・ヘッダが Zotero API の期待と一致するか（スタブで URL を記録して検証）
- [ ] **ページネーション**: 1ページ（100件）を超える結果が**黙って切り捨てられない**か。`limit` 未指定時の API 既定値（25件）に依存していないか
- [ ] **エラー伝播**: 非2xx・不正JSONが必ずエラーになるか。**「空結果」と「失敗」が区別される**か
- [ ] **入力バリデーション**: 外部入力（item key・ファイルパス・LLM からの引数）が API 呼び出し**前**に検証されるか。URL パスに生の値が埋め込まれる箇所は特に必須

## P1: 書き込み系（ライブラリを汚染しうる操作）

- [ ] **失敗の握りつぶし禁止**: API が `failed` を返したとき・HTTP エラーのとき、成功として報告されないか
- [ ] **ペイロード形状**: 送信 JSON をデコードして検証する（生文字列マッチ禁止 → [test-style-guide.md](test-style-guide.md)）
- [ ] **タグポリシー**: AI 作成ノートに `ai-generated` タグが**ちょうど1回**付くか（重複・欠落とも downstream のフィルタを壊す）
- [ ] **エスケープ**: ユーザー本文が HTML/JSON として解釈されて壊れないか（`i<j`、`AT&T` 等を含むケースを必ず入れる）
- [ ] **dry-run**: dry-run 指定時に**書き込み API 呼び出しが発生しない**か。注意: `add --dry-run` は payload を見せるためにメタデータ解決（Crossref/arXiv/OpenLibrary/ページ取得）は行う。「呼び出しなし」が保証するのは Zotero への**書き込み**だけ（`tag --dry-run` と同じ扱い）
- [ ] **識別子による冪等性**: 同一 DOI/arXiv/ISBN/URL の既存 item を検出したら重複作成しないか。検出は quick-search（ゆるい）＋**厳密フィールド一致**で、near-match を誤って重複扱いしない（`add` の `findDuplicate`）。検索が既存を返さない場合は重複を作りうる既知の限界で、`--if-exists skip|update|duplicate` で制御する
- [ ] **update の非破壊**: `--if-exists update` は書誌フィールドのみ PATCH し、既存の tags/collections を消さない（`updatePayload` が両者を除外）。version 経由の楽観ロックで lost-update を防ぐ
- [ ] **型別フィールド妥当性**: itemType に無効なフィールドを送らないか（例: `conferencePaper` に `publicationTitle`）。コンテナ誌名は型別フィールド（publicationTitle/proceedingsTitle/bookTitle）へ振り分ける。空フィールドは payload から省く（`BuildItemPayload`）

## P1: 出力契約（AI エージェントが解析する）

- [ ] **JSON エンベロープ**: `--output json` は `{"ok":true,"data":...}` / `{"ok":false,"error":{...}}` を守るか。空結果は `data: []`（null 禁止）
- [ ] **空文字列禁止**: ツール/コマンドの結果が**空文字列にならない**か。0件なら「No results found」等の明示メッセージ（LLM は空応答を故障と区別できない）
- [ ] **誤読防止ヒント**: アノテーション0件には同期ヒントを出すか（「マークなし」と誤読させない。実例: [MEMORY] annotations 0件をバグと誤認）

## P2: フィルタ・整形

- [ ] **case-insensitivity**: LLM/CLI 入力由来のフィルタ値（色・種別等）は大文字小文字を区別しない
- [ ] **rune 安全**: 文字列切り詰めは byte でなく rune 単位（日本語タイトルで文字化けしない）
- [ ] **空の表示**: 表示列が空文字列にならない（著者・タグ不明は `-`）
- [ ] **順序保証**: 「読書順」等の順序を約束する出力は、入力順に依存せずソートされているか

## P2: CLI 契約

- [ ] **エラーコード**: 失敗は CLAUDE.md のエラーコード表（`VALIDATION_ERROR` 等）に対応する CLIError で返るか
- [ ] **フラグとスキーマ**: 新フラグは `schema` コマンドの出力に反映されているか

## 過去バグ由来の観点（バグを直したらここに追記する）

バグ修正の手順（正本）: ①先に失敗する回帰テストを書く（RED）→ ②修正（GREEN）→ ③この表に観点・由来・テスト名を追記する。

| 観点 | 由来 | 再発防止テスト |
|---|---|---|
| limit 未指定のページ切り捨て | GetChildren が API 既定25件で切り捨て、26個目以降のハイライトが消えた（PR #3） | `TestGetChildrenPaginates` |
| 全件フィルタ後の空文字列応答 | zotero_search が attachment のみの結果で `""` を返した（PR #3） | `TestSearchHandlerNoResults` |
| LLM 入力の未検証パス埋め込み | MCP 読み取りツールが item_key 未検証で URL を組んだ（PR #3） | `TestHandlersRejectInvalidItemKeyWithoutAPICall` |
| HTML エスケープ漏れ | CreateNote が `i<j` を含む本文を壊した（PR #3） | `TestPlainTextToNoteHTMLEscapes` |
| 本文と偶然一致する無意味アサーション | タグ検証が note 本文の同語にマッチして常に green だった（PR #3） | ペイロード解析ベースに修正済み |
| read-modify-write でのフィールド欠落 | `Tag` が `type` を持たず、`tag --add` が item の automatic tag(type=1)を manual(0)に降格させた（PR #12） | `TestApplyTagDeltaPreservesType` / `TestUpdateItemTagsPreservesAutomaticTagTypeInBody` |
| 未指定スライスが JSON `null` 化 | `tag --dry-run` で未指定の add/remove が `null` になり「空は []」規約に反した（PR #12） | `TestEmptyIfNil` / `TestTagDryRunPayloadShape` |
| 同種データの JSON 形不一致 | `tag` の出力 tags が `[]string` で、`get`/`context` の `[]Tag` と形が違った（PR #12） | `TestTagResultPayloadShape` |
| 単一行語彙への空白系制御文字混入 | `validateTags` が `\n`/`\t` を許し、タブ入りタグが `tags` 表示を崩した（PR #12） | `TestValidateTags` |
| タイトル検索の誤同定（無関係論文を found=true で採用） | `searchPaperID` が `limit=1` のトップヒットをタイトル一致を確認せず返し、汎用的な短いタイトル（例 "Introduction"）で無関係論文の引用ネットワークを黙って構築していた（issue #16） | `TestResolvePaperIDRejectsUnrelatedTitleHit` / `TestResolvePaperIDScansCandidatesForMatch` / `TestResolvePaperIDMatchesDespiteCaseAndPunctuation` |
