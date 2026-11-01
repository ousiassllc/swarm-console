package updatecheck

// Describe は version --check の 2 行目（機能要件 §12.2）。URL は最新版が vX.Y.Z の形に合うときだけ、その版から組み立てる。
func Describe(r Result) string {
	switch r.Status {
	case UpToDate:
		return "最新です（最新版 " + r.Latest + "）"
	case UpdateAvailable:
		return "新しい版があります: " + r.Latest + "（" + ReleaseURL(r.Latest) + "）"
	case Unknown:
		return describeUnknown(r.Reason)
	case Pending, Disabled:
	}
	return ""
}

func describeUnknown(reason Reason) string {
	switch reason {
	case ReasonNetwork:
		return "確認できませんでした: GitHub に接続できません"
	case ReasonRateLimit:
		return "確認できませんでした: GitHub の取得回数の上限に達しました。しばらくしてからやり直してください"
	case ReasonAccess:
		return "確認できませんでした: GitHub が取得を拒否しました"
	case ReasonNoRelease:
		return "確認できませんでした: 公開されたリリースがありません"
	case ReasonReleaseInvalid:
		return "確認できませんでした: 最新リリースの版の形が不正です"
	case ReasonDevBuild:
		return "確認しません: この実行ファイルにはリリースの版が入っていません（dev）"
	}
	return "確認できませんでした"
}

// ExitCode は version --check の終了コード（up_to_date 0・update_available 10・unknown 1。FR-UPD-6）。
func ExitCode(r Result) int {
	switch r.Status {
	case UpToDate:
		return 0
	case UpdateAvailable:
		return 10
	case Unknown, Pending, Disabled:
	}
	return 1
}
