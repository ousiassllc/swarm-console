package safety

// CheckDirList は dir_list の path を検証し、シンボリックリンク解決後の絶対パスを返す。
// 判定は file_get（CheckFileGet）と同じで、根だけを allowed_roots にする。reason の語彙も同じ。
//   - 空・制御文字 → invalid_args
//   - 相対パス（not_absolute）・".." を含む（dotdot）・allowed_roots の外（outside_roots。存在しなくても同じ）・
//     denied_paths の一致・配下（denied）・秘密のファイル名（secret）→ path_forbidden
//   - allowed_roots の下だが存在しない → not_found
//   - ディレクトリでない → path_forbidden（not_directory。pane_start の cwd と同じ語）
func (p *Policy) CheckDirList(path string) (string, error) {
	resolved, err := p.CheckFileGet(path, p.allowedRoots)
	if err != nil {
		return "", err
	}
	if !isDir(resolved) {
		return "", pathForbidden("not_directory")
	}
	return resolved, nil
}

// IsListableDir は path が dir_list に載せてよいディレクトリか。
// pane_start の cwd として通り（CheckCWD）、かつ dir_list の path として通る（CheckDirList）ものだけ true。
// dir_list で見えたものを pane_start に渡して forbidden_path にならず、その下をさらに dir_list で開けることを保証する。
func (p *Policy) IsListableDir(path string) bool {
	if _, err := p.CheckCWD(path); err != nil {
		return false
	}
	_, err := p.CheckDirList(path)
	return err == nil
}
