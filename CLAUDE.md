# tfreview

Go 1.24 / cobra CLI。`terraform plan` の結果だけを入力に、YAML で定義した基準で
レビューし PR にコメントとラベルを 1 つずつ出す。ステータスは alpha（v1 まで破壊的変更可）。

## コマンド

`extract`（plan JSON の縮約）/ `review`（判定）/ `comment`（PR 反映）/ `fetch`（PR の plan 取得）

## 構成

- `cmd/tfreview/` - CLI 配線のみ。ロジックは置かない
- `internal/` - `plan` `match` `judge` `llm` `render` `state` `github` `config` に分割
- `internal/config/default.yaml` - `.tfreview.yaml` 無しのときの組み込みチェック

## 不変条件

- plan の `before` は runner の外に出さない。`extract` は `after` と `changed_keys` だけを残す
- LLM は hit / miss を返すだけ。深刻度(level)は config が決める
- 判定は plan + config のハッシュで target ごとにキャッシュされる

## 検証

push 前に CI と同じものを通す:
`go build ./... && go vet ./... && go test ./... && golangci-lint run`

## 公開リポジトリ

例やテストには `acme/widgets` のような中立な名前を使う。実在の会社名・社内リポジトリ名・
認証情報はコード / docs / テスト / コミットメッセージのどこにも書かない。
