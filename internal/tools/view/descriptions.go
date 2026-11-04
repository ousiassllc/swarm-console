package view

// ツールの説明。日本語の固定文で、クライアントによらず同じ（API 仕様 §1.1・§5）。
const (
	descReport = "全ペインの状況をサーバーが組み立てた報告にして返す。" +
		"markdown（既定）の場合、返った markdown に手を加えずそのまま表示する（要約・言い換え・省略をしない）。" +
		"pane を指定すると 1 ペインの詳しい版。format=json は panes_status と同じ構造に、要対応の一覧と、" +
		"使用状況・メモリ・CPU・スワップ・ディスク・更新確認・ログイン期限を足したもの。"
	descSessionsList = "tmux セッションの一覧を返す。既定は呼び出し元に許可されたセッションだけで、" +
		"セッションごとに layout・ペインの位置・操作できるか（operable）を持つ。all=true なら許可外のセッションも id だけで含める。"
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
	descNoteHistory = "閉じたペインのメモを新しい順に返す。repo・from・origin・started_by・items（指定したものをすべて含む）で絞り込める。"
)
