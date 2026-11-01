package tools

// ツールの説明。日本語の固定文で、クライアントによらず同じ（API 仕様 §1.1・§5）。
const (
	descReport = "全ペインの状況をサーバーが組み立てた報告にして返す。" +
		"markdown（既定）の場合、返った markdown に手を加えずそのまま表示する（要約・言い換え・省略をしない）。" +
		"pane を指定すると 1 ペインの詳しい版。format=json は panes_status と同じ構造に、要対応の一覧と、" +
		"使用状況・CPU・スワップ・ディスク・更新確認・ログイン期限を足したもの。"
	descPanesStatus = "対象セッションの全ペインを 1 回で返す（位置・ID・エージェント・状態・タイトル・cwd・経過・活動・" +
		"画面変化からの時間・メモ要約）。ホスト負荷・並びの警告・layout も返す。"
	descPaneTail = "1 ペインの最後の n 行を、枠線・TODO 表示・ステータス行を取り除いた本文だけで返す。" +
		"raw=true なら飾りも残す。unknown のペインは飾りの除去をしない。"
	descAskCard    = "ペインの質問カード（質問・選択肢・カーソル・自由入力の可否）を返す。質問中でなければ {\"asking\":false}。"
	descReportRead = "ペインの最終報告ファイル（<位置>.md）を読む。ファイルがなければ {\"exists\":false}。"
	descHostStatus = "サーバーの負荷・CPU 数・CPU 使用率・メモリ・スワップ・ディスク（使用率つき）・tmux セッションの有無・" +
		"エージェント種別ごとのペイン数・稼働時間と、使用状況（usage）・omp の新しい版の確認（omp）・" +
		"swarm-console の新しい版の確認の結果と自己更新の状況（update）を返す。"
	descAgentAuthStatus = "エージェント（omp・claude・codex）ごとのログイン状態（valid・expired・expiring_soon・unknown）と期限日時を返す。" +
		"鍵・トークンの中身は返さない。"
	descNoteGet     = "1 ペインのメモを全項目返す。メモがなければ note は null。"
	descNoteHistory = "閉じたペインのメモを新しい順に返す。repo・from・items（指定したものをすべて含む）で絞り込める。"

	descAskPick = "質問中のペインのカードに答える。n で選択肢の番号を選ぶか、text で「その他」に文字を入れる（どちらか一方）。" +
		"カーソルが合わなければ Enter を送らずに中止する。"
	descSendText = "ペインのエージェントへ text（またはサーバー上の file）を貼り付けて Enter を送る。" +
		"許可する制御文字は改行とタブだけ。8000 字まで。Ctrl-C などの特別なキーは送れない。"
	descSendEscape = "作業中のエージェントへ Esc を送って中断する。オーナーの OK があるときだけ使う。" +
		"confirm: true が必須。実行したことはメモのログに自動で残る。"
	descPaneStart = "shell のペインで cwd へ移動してエージェント（omp・claude・codex）を起動し、タイトルを設定する。" +
		"cwd は許可されたフォルダの下の既存ディレクトリだけ。"
	descPaneClose = "idle または shell のペインのエージェントを終了し、画面を片づけてタイトルを消す。" +
		"終了コマンドのあとは shell に戻るまで待ってから片づける（上限までに戻らなければ exit_timeout で、cd ~ 以降は送らない）。" +
		"片づけを確かめられたら、メモを履歴へ移し最終報告ファイルをアーカイブする。"
	descDispatch = "空きペイン（shell または idle）へ、決まった形の指示文（command・やること・条件）を組み立てて送る。" +
		"shell なら先にエージェントを起動する。note があれば一緒に保存する。送った指示文の全文を sent に返す。"
	descNoteSet = "ペインのメモを、渡した項目だけ書き換える。値が null ならその項目を消す。秘密に見える文字列は拒否する。"
	descNoteLog = "ペインのメモのログに、時刻つきで 1 行（500 字まで）足す。直近 50 行を残す。"
)
