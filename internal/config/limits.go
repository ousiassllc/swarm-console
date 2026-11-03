package config

// MaxTextRunes は貼り付け本文（text・file の中身・組み立て後の全文）と dispatch の command・extra_rules の上限（字）。
// safety・pane/ops・tools が同じ値を使う。
const MaxTextRunes = 8000

// MaxTitleRunes は pane_start・dispatch の title の上限（字）。
const MaxTitleRunes = 80

// DefaultFileGetMaxBytes は file_get の 1 ファイルの上限の既定（20 MiB）。MaxFileGetMaxBytes は config で上げられる上限（100 MiB。base64 にして 1 つの応答に載せるため）。
const (
	DefaultFileGetMaxBytes int64 = 20 << 20
	MaxFileGetMaxBytes     int64 = 100 << 20
)
