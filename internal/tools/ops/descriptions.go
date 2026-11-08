package ops

// ツールの説明。日本語の固定文で、クライアントによらず同じ（API 仕様 §1.1・§5）。
const (
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
	descFileGet = "サーバー上の画像・動画（png・jpg・jpeg・webp・gif・mp4）を 1 つ、base64 で返す（path・mime・size・sha256・data_base64）。" +
		"取れるのは、いま見えているペインの cwd の下・レポート置き場・設定で足した根の下だけ。" +
		"秘密に見えるファイル・中身が画像／動画でないもの・上限（既定 20 MiB）を超えるものは断る。"
	descDirList = "pane_start の cwd に選べるディレクトリを 1 階層ずつ返す（path・entries（name・path・git）・truncated）。" +
		"path を省略すると許可されたフォルダ（allowed_roots）そのもの。返すのは直下のサブディレクトリだけで、" +
		"ファイル・隠しディレクトリ・denied_paths の配下・許可の外へ出るリンクは載せない。limit は既定 200、1〜500。"
)
