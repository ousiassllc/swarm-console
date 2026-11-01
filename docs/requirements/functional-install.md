# 機能要件：インストール

[機能要件](functional.md) の続き。§13 インストール（FR-INS）を定める。新しい版の確認と自己更新は [機能要件：新しい版の確認と自己更新](functional-update.md)（FR-UPD）、デプロイ構成は [アーキテクチャ設計 §7](../architecture/deployment.md#7-デプロイ構成) を参照する。

改訂履歴は [機能要件（本書の親）](functional.md#改訂履歴) にまとめてある。

## 13. インストール（FR-INS）

運用ホストへの最初の導入を、リポジトリの直下で打つ `make install` の 1 コマンドにする：最新リリースを取る → `SHA256SUMS` で照合する → `~/bin/swarm-console/swarm-console` に置く → systemd のユーザーサービスとして常駐させる。実体は `scripts/install.sh`。手元ビルドは使わない。入れた後の更新は、既定では `serve` の自己更新（FR-UPD）に任せる。

| ID | 要件 |
|----|------|
| FR-INS-1 | 入口は `make install`（リポジトリの直下。`bash scripts/install.sh`）だけ。ユーザー権限で動き、`sudo` は要らない。置き場・unit・設定の場所はすべて `$HOME` から決める（`HOME` を一時ディレクトリに差し替えれば、実際のホームに触れずに試せる） |
| FR-INS-2 | 対応環境は Linux（linux/amd64）だけ。ほかの OS・arch、必要なコマンド（`curl`・`sha256sum`・`systemctl`・`loginctl` など）が無いとき、ユーザーの systemd（`systemctl --user`）に接続できないときは、何も置かずに失敗する |
| FR-INS-3 | 入れるのは GitHub Releases の最新リリース。版は `https://github.com/ousiassllc/swarm-console/releases/latest` の転送先の末尾から取り（`api.github.com` は使わない）、FR-UPD-3 の形（`vX.Y.Z`）に合わなければ失敗する。ダウンロード先は FR-UPD-13 と同じ形で、検証済みの版から組み立てる：`…/releases/download/<tag>/swarm-console_<tag>_linux_amd64` と `…/SHA256SUMS`。`https` だけ。`SHA256SUMS` は 64 KiB、バイナリは 64 MiB まで |
| FR-INS-4 | 検証は FR-UPD-14 と同じ規則。`SHA256SUMS`（行末の CR とファイル名直前の `*` は許す）に、アセット名の行がちょうど 1 行あり、ハッシュが 64 桁の小文字 16 進で、ダウンロードしたバイナリの SHA-256 と一致すること。さらに、ダウンロードしたバイナリの `version` の出力がリリースの版と一致すること。どれかが合わなければ、**`~/bin`・`~/.config` のどちらにも、systemd にも触れずに**失敗する（終了コード 0 以外） |
| FR-INS-5 | バイナリは `~/bin/swarm-console/swarm-console` に、通常ファイル（権限 0755）で置く。シンボリックリンクにしない（自己更新は、この場所の通常ファイルだけを入れ替えるため。FR-UPD-12）。同じフォルダに一時ファイルを書いてから `rename` で入れ替え、本体の名前が消える瞬間を作らない（自己更新の `.new`・`.old`・マーカーは使わない）。配置先がすでにシンボリックリンクのときは、置き換えずに失敗する |
| FR-INS-6 | 何回流しても同じ結果になる。すでに入っているとき：(a) ハッシュが最新と同じ → 何もしない（`unchanged`）。(b) 入っている版が最新リリース以上 → 置き換えない（`kept`。ダウングレードしない。FR-UPD-12 と同じ）。(c) それ以外（入っている版が古い、版を確認できない）→ 最新版で上書きする（`updated`）。unit は内容が同じなら触らない。動いているサービスは**再起動しない**（動いているプロセスは古いバイナリのまま。新しい版にするには `systemctl --user restart swarm-console`。`serve` の自己更新でも入れ替わる）。入れ替えたのに動いていたときは、そのことを表示する |
| FR-INS-7 | systemd の unit は、リポジトリの `deploy/swarm-console.service` を `~/.config/systemd/user/swarm-console.service` に**実ファイルのコピー**として置く（リンクにしない。リポジトリを消しても残る）。内容が違う（または無い）ときだけ書き、書いたときだけ `systemctl --user daemon-reload` する。unit の内容は [アーキテクチャ設計 §7](../architecture/deployment.md#7-デプロイ構成) のとおりで、自己更新のために変更を要しない |
| FR-INS-8 | 設定ファイル（`~/.config/swarm-console/config.toml`）は**作らない**（雛形も置かない）。`listen_addr` に運用ホストの tailnet の IP を書く必要があり、マシンごとの値で、トークンもマシンごとに発行するため。ある場合：`systemctl --user enable --now swarm-console` で起動し、`active` になるまで最大 10 秒待つ（ならなければ、`journalctl` を見るよう案内して失敗）。無い場合：`systemctl --user enable swarm-console` で有効にするだけで起動せず（起動しても `Restart=always` で失敗を繰り返すため）、`setup` → `token add` → `make install` のやり直しの手順を表示して、終了コード 0 で終わる |
| FR-INS-9 | リンガリング（ログアウトしてもユーザーサービスを止めない）：`loginctl show-user $USER --property=Linger --value` が `yes` なら何もしない。そうでなければ `loginctl enable-linger $USER` を実行し、失敗したら失敗にする |
| FR-INS-10 | 終わるとき、版と、バイナリ（`installed`・`updated`・`unchanged`・`kept`）・unit（`written`・`unchanged`）・リンガリング（`enabled`・`unchanged`）の結果を 1 行ずつ表示する |
| FR-INS-11 | 途中で落ちても、再度 `make install` を打てば続きから完了する（FR-INS-6）。FR-INS-4 の検証に落ちたときは何も置かない。置いたあとに落ちたとき（unit・有効化・起動・リンガリング）は、置いたものは残る（バイナリの名前は常にあり、壊れた中間状態は作らない） |
