# テストスタイルガイド（zotero-cli）

生成・手書きを問わず、このリポジトリのテストは以下の形式に従う。
形式の一部（Contract コメント・gofmt）は hook（`scripts/hook-validate-test-contract.sh`）が自動検証し、違反はファイル書き込み時にブロックされる。

## Contract コメント（必須・hook 検証対象）

すべてのトップレベル `func TestXxx` の直前に、**何の契約を固定するか・なぜその挙動が必要か**を書く。

```go
// Contract: GetChildren follows pagination instead of trusting one page.
// The Zotero API caps a page at 100 items (and defaults to 25 without an
// explicit limit) — a paper with more than one page of annotations must not
// have the rest silently dropped.
func TestGetChildrenPaginates(t *testing.T) {
```

- 「何をするか」ではなく「**何が壊れたら誰がどう困るか**」まで書く
- 過去バグ由来のテストは [qa-perspectives.md](qa-perspectives.md) の該当行を参照する
- `TestMain` は対象外

## テスト名

- 挙動を文で表す: `TestSearchHandlerNoResults`、`TestCreateNoteFailedResponse`
- 禁止: `TestOK` / `TestWorks` / `TestBasic` のような中身を示さない名前（曖昧語禁止）

## 構造

- **AAA**（Arrange-Act-Assert）。各ブロックは空行で区切る
- バリエーションは **table-driven** + `t.Run(tt.name, ...)`。ケース名も挙動を表す文にする
- 1テスト1契約。複数の契約を1つのテストに詰め込まない

## アサーション

- **送信ペイロードは必ずデコードして検証する**。生文字列の `strings.Contains` は本文と偶然一致して無意味化する（実例: タグ検証が note 本文の "summary" にマッチして常に green だった）
- 期待値は具体的に書く（件数だけでなくキー・順序まで）
- エラー系は「エラーになること」に加え、**API 呼び出しが発生していないこと**（バリデーション系）や **status code がメッセージに含まれること**（HTTP 系）まで確認する

## ネットワーク・決定性

- **実 API を叩かない**。`Client.HTTPClient` にスタブ `http.RoundTripper` を注入する
- 既存スタブの使い分け（新規に作る前にこれを再利用する）:

| スタブ | 場所 | 用途 |
|---|---|---|
| `stubTransport` | `zotero/client_test.go` | パス→レスポンスのマップ。GET 専用（書き込みが混入したら 405 で落とす） |
| `queryRecordingTransport` | `zotero/read_test.go` | リクエスト URL の記録＋応答キュー（ページネーション検証用） |
| `recordingTransport` | `zotero/write_test.go` | メソッド・パス・ボディの記録（書き込み系検証用） |
| `pathStubTransport` / `recordingTransport` | `cmd/zotero-mcp/main_test.go` | 上記の MCP パッケージ版（Go の `_test.go` はパッケージ間で共有できないため別実装） |

- 時刻・乱数に依存するテストを書かない（必要になったら注入に変える）
- 実行は常に `go test -race ./...`。flaky なテストは即座に直すか削除する（skip で放置しない）

## カバレッジ

- 床は 80%（zotero パッケージ）。ただし**%稼ぎのトートロジーテストを書かない** — 観点（qa-perspectives.md）に紐付かないテストは書く意味を疑う
- 新機能・バグ修正の PR では、変更が触れる P1 観点がすべてテストに紐付いていることをセルフレビューで確認する
